package linux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const dockerSystemdTimeout = 2 * time.Second

type dockerSystemdScope uint8

const (
	dockerNoSystemd dockerSystemdScope = iota
	dockerSystemSystemd
	dockerUserSystemd
	dockerUnknownSystemd
)

type dockerUnitStateFunc func(context.Context, dockerSystemdScope, string) (string, error)

func dockerSocketSystemdScope(path, runtimeDir string) dockerSystemdScope {
	rawPath := filepath.Clean(path)
	path = canonicalDockerSocket(path)
	if isSystemDockerSocket(rawPath) || isSystemDockerSocket(path) {
		return dockerSystemSystemd
	}
	if filepath.IsAbs(runtimeDir) && path == canonicalDockerSocket(filepath.Join(runtimeDir, "docker.sock")) {
		// A user manager is authoritative only for its own runtime directory.
		info, err := os.Stat(runtimeDir)
		if err != nil || !info.IsDir() || os.Getuid() != os.Geteuid() {
			return dockerUnknownSystemd
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Getuid()) {
			return dockerUnknownSystemd
		}
		return dockerUserSystemd
	}
	return dockerNoSystemd
}

func isSystemDockerSocket(path string) bool {
	return path == "/run/docker.sock" || path == "/var/run/docker.sock"
}

func canonicalDockerSocket(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func (c dockerCheck) socketActivationWarning(ctx context.Context) (check.Result, bool) {
	if c.scope == dockerNoSystemd {
		return check.Result{}, false
	}
	// An unknown unit state cannot rule out activation, so skip the connection.
	unknown := dockerWarning("estado do serviço local do Docker indisponível; teste ignorado para evitar a ativação do socket")
	if c.scope == dockerUnknownSystemd || c.unitState == nil {
		return unknown, true
	}

	stateCtx, cancel := context.WithTimeout(ctx, dockerSystemdTimeout)
	defer cancel()
	socketState, err := c.unitState(stateCtx, c.scope, "docker.socket")
	if err != nil || stateCtx.Err() != nil {
		return unknown, true
	}
	switch socketState {
	case "inactive", "failed":
		return check.Result{}, false
	case "active":
		// An active socket can start docker.service on the first connection.
	default:
		return unknown, true
	}

	serviceState, err := c.unitState(stateCtx, c.scope, "docker.service")
	if err != nil || stateCtx.Err() != nil {
		return unknown, true
	}
	switch serviceState {
	case "active":
		return check.Result{}, false
	case "inactive", "failed":
		return dockerWarning("o daemon local do Docker não está em execução; teste ignorado para evitar a ativação do socket"), true
	case "activating", "deactivating", "reloading":
		return dockerWarning("o serviço local do Docker está mudando de estado; teste ignorado para evitar a ativação do socket"), true
	default:
		return unknown, true
	}
}

func systemctlDockerUnitState(ctx context.Context, scope dockerSystemdScope, unit string) (string, error) {
	return queryDockerUnitState(ctx, scope, unit, runSystemctl)
}

func queryDockerUnitState(ctx context.Context, scope dockerSystemdScope, unit string, run systemctlRunner) (string, error) {
	var flag string
	switch scope {
	case dockerSystemSystemd:
		flag = "--system"
	case dockerUserSystemd:
		flag = "--user"
	default:
		return "", fmt.Errorf("escopo do systemd para Docker inválido: %d", scope)
	}
	output, err := run(ctx, flag, "show", "--property=ActiveState", "--value", "--no-pager", "--", unit)
	if err != nil {
		return "", fmt.Errorf("systemctl show %s: %w", unit, err)
	}
	state := strings.TrimSpace(string(output))
	if state == "" || strings.ContainsAny(state, "\r\n") {
		return "", fmt.Errorf("systemctl show %s retornou um estado inválido", unit)
	}
	return state, nil
}
