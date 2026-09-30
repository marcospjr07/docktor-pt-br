package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const (
	firewalldRunning = "LoadState=loaded\nActiveState=active\nSubState=running\n"
	firewalldStopped = "LoadState=loaded\nActiveState=inactive\nSubState=dead\n"
)

var firewalldStateQuery = []string{
	"--system", "show", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--no-pager", "--", "firewalld.service",
}

func TestFirewallCheckBackendStates(t *testing.T) {
	tests := []struct {
		name               string
		ufwInstalled       bool
		ufwOutput          string
		ufwStderr          string
		ufwErr             error
		firewalldInstalled bool
		serviceOutput      string
		serviceErr         error
		firewalldOutput    string
		firewalldStderr    string
		firewalldExit      int
		firewalldErr       error
		wantFirewalldCmd   bool
		wantStatus         check.Status
		wantMessage        string
	}{
		{name: "neither installed", wantStatus: check.StatusWarn, wantMessage: "nenhuma ferramenta de firewall compatível encontrada"},
		{name: "UFW ativo", ufwInstalled: true, ufwOutput: "Status: active\nTo Action From\n", wantStatus: check.StatusPass, wantMessage: "UFW ativo"},
		{name: "UFW inactive", ufwInstalled: true, ufwOutput: "Status: inactive\n", wantStatus: check.StatusWarn, wantMessage: "UFW instalado, mas inativo"},
		{name: "UFW unknown", ufwInstalled: true, ufwOutput: "Status: uncertain\n", wantStatus: check.StatusWarn, wantMessage: "estado do UFW indisponível"},
		{name: "UFW query error", ufwInstalled: true, ufwOutput: "Status: active\n", ufwErr: os.ErrPermission, wantStatus: check.StatusWarn, wantMessage: "estado do UFW indisponível"},
		{name: "UFW stderr is not state", ufwInstalled: true, ufwOutput: "Status: active\n", ufwStderr: "unexpected warning\n", wantStatus: check.StatusWarn, wantMessage: "estado do UFW indisponível"},
		{name: "firewalld running", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", wantFirewalldCmd: true, wantStatus: check.StatusPass, wantMessage: "firewalld ativo"},
		{name: "firewalld stopped", firewalldInstalled: true, serviceOutput: firewalldStopped, wantStatus: check.StatusWarn, wantMessage: "firewalld instalado, mas inativo"},
		{name: "firewalld failed", firewalldInstalled: true, serviceOutput: "LoadState=loaded\nActiveState=failed\nSubState=failed\n", wantStatus: check.StatusWarn, wantMessage: "firewalld relatou uma falha na inicialização"},
		{name: "firewalld unknown", firewalldInstalled: true, serviceOutput: "LoadState=loaded\nActiveState=active\nSubState=exited\n", wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld unit missing", firewalldInstalled: true, serviceOutput: "LoadState=not-found\nActiveState=inactive\nSubState=dead\n", wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld query error", firewalldInstalled: true, serviceOutput: firewalldRunning, serviceErr: errors.New("manager unavailable"), wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld not running after preflight", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "not running\n", firewalldExit: 252, firewalldErr: errors.New("not running"), wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "firewalld instalado, mas inativo"},
		{name: "firewalld internal failure", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "failed\n", firewalldExit: 251, firewalldErr: errors.New("startup failed"), wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "firewalld relatou uma falha na inicialização"},
		{name: "firewalld exit does not match output", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", firewalldExit: 251, firewalldErr: errors.New("startup failed"), wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld unknown state", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "unexpected\n", wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld stderr is not state", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", firewalldStderr: "unexpected warning\n", wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "firewalld command error", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", firewalldErr: os.ErrPermission, wantFirewalldCmd: true, wantStatus: check.StatusWarn, wantMessage: "estado do firewalld indisponível"},
		{name: "both active", ufwInstalled: true, ufwOutput: "Status: active\n", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", wantFirewalldCmd: true, wantStatus: check.StatusPass, wantMessage: "UFW e firewalld ativos"},
		{name: "UFW active and firewalld inactive", ufwInstalled: true, ufwOutput: "Status: active\n", firewalldInstalled: true, serviceOutput: firewalldStopped, wantStatus: check.StatusPass, wantMessage: "UFW ativo; firewalld instalado, mas inativo"},
		{name: "UFW inactive and firewalld active", ufwInstalled: true, ufwOutput: "Status: inactive\n", firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", wantFirewalldCmd: true, wantStatus: check.StatusPass, wantMessage: "UFW instalado, mas inativo; firewalld ativo"},
		{name: "UFW unavailable and firewalld active", ufwInstalled: true, ufwErr: os.ErrPermission, firewalldInstalled: true, serviceOutput: firewalldRunning, firewalldOutput: "running\n", wantFirewalldCmd: true, wantStatus: check.StatusPass, wantMessage: "estado do UFW indisponível; firewalld ativo"},
		{name: "both inactive", ufwInstalled: true, ufwOutput: "Status: inactive\n", firewalldInstalled: true, serviceOutput: firewalldStopped, wantStatus: check.StatusWarn, wantMessage: "UFW instalado, mas inativo; firewalld instalado, mas inativo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ufwCalls, serviceCalls, firewalldCalls int
			c := firewallCheck{
				lookPath: func(name string) (string, error) {
					switch name {
					case "ufw":
						if tt.ufwInstalled {
							return "/fixture/ufw", nil
						}
					case "firewall-cmd":
						if tt.firewalldInstalled {
							return "/fixture/firewall-cmd", nil
						}
					default:
						t.Fatalf("unexpected binary lookup %q", name)
					}
					return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
				},
				runCommand: func(_ context.Context, path string, args ...string) firewallCommandResult {
					switch path {
					case "/fixture/ufw":
						ufwCalls++
						if !slices.Equal(args, []string{"status"}) {
							t.Fatalf("unexpected UFW arguments: %v", args)
						}
						return firewallCommandResult{stdout: []byte(tt.ufwOutput), stderr: []byte(tt.ufwStderr), err: tt.ufwErr}
					case "/fixture/firewall-cmd":
						firewalldCalls++
						if !slices.Equal(args, []string{"--state"}) {
							t.Fatalf("unexpected firewalld arguments: %v", args)
						}
						return firewallCommandResult{stdout: []byte(tt.firewalldOutput), stderr: []byte(tt.firewalldStderr), exitCode: tt.firewalldExit, err: tt.firewalldErr}
					default:
						t.Fatalf("unexpected firewall command: %q %v", path, args)
						return firewallCommandResult{}
					}
				},
				runSystemctl: func(_ context.Context, args ...string) ([]byte, error) {
					serviceCalls++
					if !slices.Equal(args, firewalldStateQuery) {
						t.Fatalf("unexpected systemctl arguments: %v", args)
					}
					return []byte(tt.serviceOutput), tt.serviceErr
				},
			}
			got := c.Run(context.Background())
			if got.Status != tt.wantStatus || got.Message != tt.wantMessage {
				t.Fatalf("got %#v; want %s %q", got, tt.wantStatus, tt.wantMessage)
			}
			if ufwCalls != boolCount(tt.ufwInstalled) || serviceCalls != boolCount(tt.firewalldInstalled) || firewalldCalls != boolCount(tt.wantFirewalldCmd) {
				t.Fatalf("probe calls = UFW %d, systemctl %d, firewall-cmd %d; want %t, %t, %t", ufwCalls, serviceCalls, firewalldCalls, tt.ufwInstalled, tt.firewalldInstalled, tt.wantFirewalldCmd)
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestFirewallParsersRejectAmbiguousOutput(t *testing.T) {
	for _, output := range []string{"", "Status: active-ish\n", "Warning\nStatus: active\n", "Status: ACTIVE\n"} {
		if state := parseUFWStatus([]byte(output)); state != firewallUnknown {
			t.Errorf("UFW output %q parsed as %d; want unknown", output, state)
		}
	}
	if state := parseUFWStatus([]byte("\n  Status: active  \nTo Action From\n")); state != firewallActive {
		t.Errorf("UFW status with whitespace parsed as %d; want active", state)
	}
	for _, output := range []string{
		"", "running\n", "ActiveState=active\nSubState=running\n",
		"LoadState=loaded\nActiveState=active\nActiveState=inactive\nSubState=running\n",
		"LoadState=loaded\nActiveState=active\nSubState=running\nOther=value\n",
		"LoadState=loaded\nActiveState=active\nSubState=dead\n",
	} {
		if state := parseFirewalldUnitState([]byte(output)); state != firewallUnknown {
			t.Errorf("firewalld unit output %q parsed as %d; want unknown", output, state)
		}
	}
	if state := parseFirewalldUnitState([]byte("SubState=running\nLoadState=loaded\nActiveState=active\n")); state != firewallActive {
		t.Errorf("firewalld reordered properties parsed as %d; want active", state)
	}
}

func TestFirewallLookupErrorIsNotAbsence(t *testing.T) {
	c := firewallCheck{lookPath: func(name string) (string, error) {
		if name == "ufw" {
			return "", os.ErrPermission
		}
		return "", exec.ErrNotFound
	}}
	got := c.Run(context.Background())
	if got.Status != check.StatusWarn || got.Message != "estado do UFW indisponível" {
		t.Fatalf("got %#v; want lookup warning", got)
	}
}

func TestFirewallProbeTimeout(t *testing.T) {
	for _, backend := range []string{"ufw", "firewall-cmd"} {
		t.Run(backend, func(t *testing.T) {
			c := firewallCheck{
				timeout: 5 * time.Millisecond,
				lookPath: func(name string) (string, error) {
					if name == backend {
						return "/fixture/" + name, nil
					}
					return "", exec.ErrNotFound
				},
				runCommand: func(ctx context.Context, _ string, _ ...string) firewallCommandResult {
					<-ctx.Done()
					return firewallCommandResult{stdout: []byte("Status: active\n")}
				},
				runSystemctl: func(ctx context.Context, _ ...string) ([]byte, error) {
					<-ctx.Done()
					return []byte(firewalldRunning), nil
				},
			}
			got := c.Run(context.Background())
			if got.Status != check.StatusWarn || !strings.Contains(got.Message, "estado do") {
				t.Fatalf("got %#v; timed out probe must warn", got)
			}
		})
	}
}

func TestFirewallCommandTimeoutAfterActivePreflight(t *testing.T) {
	c := firewallCheck{
		timeout: 5 * time.Millisecond,
		lookPath: func(name string) (string, error) {
			if name == "firewall-cmd" {
				return "/fixture/firewall-cmd", nil
			}
			return "", exec.ErrNotFound
		},
		runSystemctl: func(context.Context, ...string) ([]byte, error) {
			return []byte(firewalldRunning), nil
		},
		runCommand: func(ctx context.Context, path string, args ...string) firewallCommandResult {
			if path != "/fixture/firewall-cmd" || !slices.Equal(args, []string{"--state"}) {
				t.Fatalf("unexpected command: %q %v", path, args)
			}
			<-ctx.Done()
			return firewallCommandResult{stdout: []byte("running\n")}
		},
	}
	got := c.Run(context.Background())
	if got.Status != check.StatusWarn || got.Message != "estado do firewalld indisponível" {
		t.Fatalf("got %#v; timed out state query must warn", got)
	}
}

func TestFirewallCanceledContextSkipsRemainingProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	c := firewallCheck{
		lookPath: func(name string) (string, error) {
			calls++
			if name != "ufw" {
				t.Fatalf("lookup after cancellation: %q", name)
			}
			return "/fixture/ufw", nil
		},
		runCommand: func(context.Context, string, ...string) firewallCommandResult {
			cancel()
			return firewallCommandResult{stdout: []byte("Status: active\n")}
		},
	}
	got := c.Run(ctx)
	if got.Status != check.StatusWarn || !strings.Contains(got.Message, "varredura interrompida") || calls != 1 {
		t.Fatalf("got %#v after %d lookups; want interrupted warning", got, calls)
	}
	got = c.Run(ctx)
	if got.Status != check.StatusWarn || calls != 1 {
		t.Fatalf("got %#v after %d lookups; canceled scan must skip probes", got, calls)
	}
}

func TestFirewallCommandRunnerSeparatesStreamsAndKeepsBusLocal(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKTOR_FIREWALL_HELPER", "1")
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "tcp:host=example.invalid")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "tcp:host=example.invalid")
	t.Setenv("LC_ALL", "invalid")

	result := runFirewallCommand(context.Background(), path, "-test.run=^TestFirewallCommandHelper$")
	var exitError *exec.ExitError
	if !errors.As(result.err, &exitError) || result.exitCode != 252 || string(result.stdout) != "not running\n" || string(result.stderr) != "diagnostic\n" {
		t.Fatalf("command result = %#v; want separate streams and exit code 252", result)
	}
}

func TestFirewallCommandHelper(t *testing.T) {
	if os.Getenv("DOCKTOR_FIREWALL_HELPER") != "1" {
		return
	}
	if os.Getenv("DBUS_SYSTEM_BUS_ADDRESS") != "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" || os.Getenv("LC_ALL") != "C" {
		os.Exit(3)
	}
	fmt.Fprint(os.Stdout, "not running\n")
	fmt.Fprint(os.Stderr, "diagnostic\n")
	os.Exit(252)
}
