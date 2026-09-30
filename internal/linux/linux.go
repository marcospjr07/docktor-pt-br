// Package linux contains read-only diagnostics backed by Linux system data.
package linux

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

type readFileFunc func(string) ([]byte, error)
type statFSFunc func(string) (filesystemStats, error)

type filesystemStats struct {
	blockSize uint64
	blocks    uint64
	free      uint64
	available uint64
}

// Checks returns the initial diagnostics in display order.
func Checks() []check.Check {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	dockerSocket := resolveDockerSocket(os.Getenv("DOCKER_HOST"), runtimeDir)
	return []check.Check{
		osCheck{readFile: os.ReadFile},
		uptimeCheck{readFile: os.ReadFile},
		memoryCheck{readFile: os.ReadFile},
		diskCheck{statFS: readFilesystem},
		systemdCheck{runSystemctl: runSystemctl, timeout: systemdQueryTimeout},
		sshCheck{configPath: defaultSSHConfigPath, includeBase: sshIncludeBase, readFile: readSSHFile},
		firewallCheck{lookPath: exec.LookPath, runCommand: runFirewallCommand, runSystemctl: runSystemctl},
		packagesCheck{lookPath: exec.LookPath, runAPT: runAPTCommand, stat: os.Stat, now: time.Now},
		dockerCheck{
			socketPath: dockerSocket,
			scope:      dockerSocketSystemdScope(dockerSocket, runtimeDir),
			unitState:  systemctlDockerUnitState,
			dialSocket: dialLocalDockerSocket,
			lookPath:   exec.LookPath,
		},
	}
}

func readFilesystem(path string) (filesystemStats, error) {
	var raw syscall.Statfs_t
	if err := syscall.Statfs(path, &raw); err != nil {
		return filesystemStats{}, err
	}

	blockSize := raw.Frsize
	if blockSize <= 0 {
		blockSize = raw.Bsize
	}
	if blockSize <= 0 {
		return filesystemStats{}, errInvalidFilesystem
	}
	return filesystemStats{
		blockSize: uint64(blockSize),
		blocks:    raw.Blocks,
		free:      raw.Bfree,
		available: raw.Bavail,
	}, nil
}

func contextWarning(ctx context.Context) (check.Result, bool) {
	if err := ctx.Err(); err != nil {
		message := err.Error()
		switch err {
		case context.Canceled:
			message = "contexto cancelado"
		case context.DeadlineExceeded:
			message = "tempo limite do contexto excedido"
		}
		return check.Result{Status: check.StatusWarn, Message: "varredura interrompida: " + message}, true
	}
	return check.Result{}, false
}
