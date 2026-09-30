package linux

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const firewallQueryTimeout = 3 * time.Second

type firewallCommandResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	err      error
}

type firewallCommandRunner func(context.Context, string, ...string) firewallCommandResult
type firewallLookup func(string) (string, error)

type firewallState uint8

const (
	firewallAbsent firewallState = iota
	firewallActive
	firewallInactive
	firewallFailed
	firewallUnknown
)

type firewallCheck struct {
	lookPath     firewallLookup
	runCommand   firewallCommandRunner
	runSystemctl systemctlRunner
	timeout      time.Duration
}

func (firewallCheck) Name() string { return "Firewall" }

func (c firewallCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}

	ufw := c.probeUFW(ctx)
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	firewalld := c.probeFirewalld(ctx)
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}

	status := check.StatusWarn
	if ufw == firewallActive || firewalld == firewallActive {
		status = check.StatusPass
	}
	if ufw == firewallAbsent && firewalld == firewallAbsent {
		return check.Result{Status: status, Message: "nenhuma ferramenta de firewall compatível encontrada"}
	}
	if ufw == firewallActive && firewalld == firewallActive {
		return check.Result{Status: status, Message: "UFW e firewalld ativos"}
	}

	var messages []string
	if ufw != firewallAbsent {
		messages = append(messages, firewallStateMessage("UFW", ufw))
	}
	if firewalld != firewallAbsent {
		messages = append(messages, firewallStateMessage("firewalld", firewalld))
	}
	return check.Result{Status: status, Message: strings.Join(messages, "; ")}
}

func (c firewallCheck) probeUFW(ctx context.Context) firewallState {
	path, err := c.lookPath("ufw")
	if err != nil {
		return firewallLookupState(err)
	}
	if path == "" {
		return firewallUnknown
	}

	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout())
	defer cancel()
	result := c.runCommand(queryCtx, path, "status")
	if queryCtx.Err() != nil || result.err != nil || result.exitCode != 0 || len(result.stderr) != 0 {
		return firewallUnknown
	}
	return parseUFWStatus(result.stdout)
}

func (c firewallCheck) probeFirewalld(ctx context.Context) firewallState {
	path, err := c.lookPath("firewall-cmd")
	if err != nil {
		return firewallLookupState(err)
	}
	if path == "" {
		return firewallUnknown
	}

	// D-Bus access can activate firewalld. Skip the client when the system
	// manager does not confirm the service is already running.
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout())
	defer cancel()
	stdout, err := c.runSystemctl(queryCtx, "--system", "show", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--no-pager", "--", "firewalld.service")
	if queryCtx.Err() != nil || err != nil {
		return firewallUnknown
	}
	serviceState := parseFirewalldUnitState(stdout)
	if serviceState != firewallActive {
		return serviceState
	}

	// The daemon may be running but internally failed. Only its own read-only
	// state query can establish a positive result.
	result := c.runCommand(queryCtx, path, "--state")
	if queryCtx.Err() != nil || len(result.stderr) != 0 {
		return firewallUnknown
	}
	if result.err == nil && result.exitCode == 0 && strings.TrimSpace(string(result.stdout)) == "running" {
		return firewallActive
	}
	// firewalld documents NOT_RUNNING as exit 252. A plain text match alone
	// cannot establish this state because it could be an error message.
	if result.exitCode == 252 && strings.TrimSpace(string(result.stdout)) == "not running" {
		return firewallInactive
	}
	if result.exitCode == 251 && strings.TrimSpace(string(result.stdout)) == "failed" {
		return firewallFailed
	}
	return firewallUnknown
}

func (c firewallCheck) queryTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return firewallQueryTimeout
}

func firewallLookupState(err error) firewallState {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return firewallAbsent
	}
	return firewallUnknown
}

func firewallStateMessage(name string, state firewallState) string {
	switch state {
	case firewallActive:
		return name + " ativo"
	case firewallInactive:
		return name + " instalado, mas inativo"
	case firewallFailed:
		return name + " relatou uma falha na inicialização"
	default:
		return "estado do " + name + " indisponível"
	}
}

func parseUFWStatus(output []byte) firewallState {
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		switch strings.TrimSpace(string(line)) {
		case "":
			continue
		case "Status: active":
			return firewallActive
		case "Status: inactive":
			return firewallInactive
		default:
			return firewallUnknown
		}
	}
	return firewallUnknown
}

func parseFirewalldUnitState(output []byte) firewallState {
	properties := make(map[string]string, 3)
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		key, value, ok := strings.Cut(string(line), "=")
		if !ok || value == "" || (key != "LoadState" && key != "ActiveState" && key != "SubState") {
			return firewallUnknown
		}
		if _, exists := properties[key]; exists {
			return firewallUnknown
		}
		properties[key] = value
	}
	if len(properties) != 3 || properties["LoadState"] != "loaded" {
		return firewallUnknown
	}
	if properties["ActiveState"] == "active" && properties["SubState"] == "running" {
		return firewallActive
	}
	if properties["ActiveState"] == "inactive" && properties["SubState"] == "dead" {
		return firewallInactive
	}
	if properties["ActiveState"] == "failed" && properties["SubState"] == "failed" {
		return firewallFailed
	}
	return firewallUnknown
}

func runFirewallCommand(ctx context.Context, path string, args ...string) firewallCommandResult {
	cmd := exec.CommandContext(ctx, path, args...)
	// Do not let inherited bus addresses redirect the firewalld query.
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DBUS_SYSTEM_BUS_ADDRESS=") || strings.HasPrefix(entry, "DBUS_SESSION_BUS_ADDRESS=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	cmd.WaitDelay = 200 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := firewallCommandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: -1, err: err}
	if err == nil {
		result.exitCode = 0
	} else {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.exitCode = exitError.ExitCode()
		}
	}
	return result
}
