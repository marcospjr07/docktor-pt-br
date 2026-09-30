package linux

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func testDockerCheck(path string) dockerCheck {
	return dockerCheck{
		socketPath: path,
		dialSocket: dialLocalDockerSocket,
		lookPath: func(name string) (string, error) {
			return "/usr/bin/" + name, nil
		},
	}
}

func serveDockerVersion(t *testing.T, handler http.Handler) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return path
}

func TestDockerCheckReachableAndMissingCLI(t *testing.T) {
	path := serveDockerVersion(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/version" {
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"29.7.2","ApiVersion":"1.52"}`))
	}))
	c := testDockerCheck(path)
	if result := c.Run(context.Background()); result.Status != check.StatusPass ||
		result.Message != "daemon local acessível (versão 29.7.2)" {
		t.Fatalf("reachable daemon: %+v", result)
	}

	c.lookPath = func(name string) (string, error) {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if result := c.Run(context.Background()); result.Status != check.StatusPass ||
		result.Message != "daemon local acessível (versão 29.7.2); CLI do Docker não encontrada" {
		t.Fatalf("reachable daemon without CLI: %+v", result)
	}
}

func TestDockerCheckMissingSocket(t *testing.T) {
	c := testDockerCheck(filepath.Join(t.TempDir(), "missing.sock"))
	if result := c.Run(context.Background()); result.Status != check.StatusWarn ||
		result.Message != "socket local do Docker não encontrado" {
		t.Fatalf("missing socket: %+v", result)
	}
}

func TestDockerCheckTypedConnectionErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"permission denied", syscall.EACCES, "acesso ao socket local do Docker negado"},
		{"connection refused", syscall.ECONNREFUSED, "daemon local do Docker indisponível (conexão recusada)"},
		{"socket invalid", syscall.ENOTSOCK, "socket local do Docker inválido"},
		{"other connection failure", syscall.ECONNRESET, "daemon local do Docker indisponível"},
		{"timeout", context.DeadlineExceeded, "a verificação do daemon local excedeu o tempo limite"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := testDockerCheck("/unused/docker.sock")
			c.dialSocket = func(context.Context, string) (net.Conn, error) {
				return nil, &net.OpError{Op: "dial", Net: "unix", Err: test.err}
			}
			if result := c.Run(context.Background()); result.Status != check.StatusWarn ||
				result.Message != test.want {
				t.Fatalf("Run() = %+v; want warning %q", result, test.want)
			}
		})
	}
}

func TestDockerCheckInvalidResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"HTTP error", http.StatusServiceUnavailable, `{"message":"unavailable"}`, "a API local do Docker retornou HTTP 503"},
		{"malformed JSON", http.StatusOK, `{"Version":`, "JSON da resposta de versão do Docker local inválido"},
		{"missing version", http.StatusOK, `{"ApiVersion":"1.52"}`, "versão do daemon local do Docker ausente"},
		{"control character", http.StatusOK, `{"Version":"29.7.2\nPASS"}`, "versão do daemon local do Docker inválida"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := serveDockerVersion(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			if result := testDockerCheck(path).Run(context.Background()); result.Status != check.StatusWarn ||
				result.Message != test.want {
				t.Fatalf("Run() = %+v; want warning %q", result, test.want)
			}
		})
	}
}

func TestDockerCheckMalformedHTTP(t *testing.T) {
	c := testDockerCheck("/unused/docker.sock")
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			for {
				line, err := reader.ReadString('\n')
				if err != nil || line == "\r\n" {
					break
				}
			}
			_, _ = server.Write([]byte("not HTTP\r\n\r\n"))
		}()
		return client, nil
	}
	if result := c.Run(context.Background()); result.Status != check.StatusWarn ||
		result.Message != "resposta HTTP local do Docker inválida" {
		t.Fatalf("malformed HTTP response: %+v", result)
	}
}

func TestDockerCheckDoesNotFollowRedirect(t *testing.T) {
	var requests atomic.Int32
	path := serveDockerVersion(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://elsewhere/version", http.StatusFound)
	}))
	result := testDockerCheck(path).Run(context.Background())
	if result.Status != check.StatusWarn || result.Message != "a API local do Docker retornou HTTP 302" ||
		requests.Load() != 1 {
		t.Fatalf("redirect result = %+v; requests = %d", result, requests.Load())
	}
}

func TestDockerCheckHTTPTimeout(t *testing.T) {
	path := serveDockerVersion(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if result := testDockerCheck(path).probe(ctx); result.Status != check.StatusWarn ||
		result.Message != "a verificação do daemon local excedeu o tempo limite" {
		t.Fatalf("timed out HTTP request: %+v", result)
	}
}

func TestDockerCheckCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := testDockerCheck("/unused/docker.sock")
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		t.Fatal("dialed after cancellation")
		return nil, nil
	}
	if result := c.Run(ctx); result.Status != check.StatusWarn ||
		result.Message != "varredura interrompida: contexto cancelado" {
		t.Fatalf("canceled before probe: %+v", result)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		cancel()
		return nil, context.Canceled
	}
	if result := c.Run(ctx); result.Status != check.StatusWarn ||
		result.Message != "varredura interrompida: contexto cancelado" {
		t.Fatalf("canceled during probe: %+v", result)
	}
}

func TestDockerCheckSystemdSocketActivation(t *testing.T) {
	path := serveDockerVersion(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"29.7.2"}`))
	}))
	tests := []struct {
		name         string
		socketState  string
		serviceState string
		wantStatus   check.Status
		wantMessage  string
		wantDials    int32
	}{
		{"service running", "active", "active", check.StatusPass, "daemon local acessível (versão 29.7.2)", 1},
		{"service stopped", "active", "inactive", check.StatusWarn, "o daemon local do Docker não está em execução; consulta não realizada para evitar a ativação do serviço pelo socket", 0},
		{"service reloading", "active", "reloading", check.StatusWarn, "o serviço local do Docker está mudando de estado; consulta não realizada para evitar a ativação do serviço pelo socket", 0},
		{"socket inactive", "inactive", "inactive", check.StatusPass, "daemon local acessível (versão 29.7.2)", 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := testDockerCheck(path)
			c.scope = dockerSystemSystemd
			c.unitState = func(_ context.Context, scope dockerSystemdScope, unit string) (string, error) {
				if scope != dockerSystemSystemd {
					t.Fatalf("queried wrong systemd scope: %d", scope)
				}
				switch unit {
				case "docker.socket":
					return test.socketState, nil
				case "docker.service":
					return test.serviceState, nil
				default:
					t.Fatalf("queried unexpected unit: %s", unit)
					return "", nil
				}
			}
			var dials atomic.Int32
			c.dialSocket = func(ctx context.Context, path string) (net.Conn, error) {
				dials.Add(1)
				return dialLocalDockerSocket(ctx, path)
			}
			result := c.Run(context.Background())
			if result.Status != test.wantStatus || result.Message != test.wantMessage || dials.Load() != test.wantDials {
				t.Fatalf("Run() = %+v, dial calls = %d; want %s, %q, %d dials", result, dials.Load(), test.wantStatus, test.wantMessage, test.wantDials)
			}
		})
	}
}

func TestDockerCheckUnknownSystemdStateSkipsProbe(t *testing.T) {
	tests := []struct {
		name         string
		socketState  string
		serviceState string
		errUnit      string
		err          error
	}{
		{"socket query error", "", "", "docker.socket", errors.New("systemd unavailable")},
		{"service query error", "active", "", "docker.service", errors.New("systemd unavailable")},
		{"socket activating", "activating", "", "", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := testDockerCheck("/run/docker.sock")
			c.scope = dockerSystemSystemd
			c.unitState = func(_ context.Context, _ dockerSystemdScope, unit string) (string, error) {
				if unit == test.errUnit {
					return "", test.err
				}
				if unit == "docker.socket" {
					return test.socketState, nil
				}
				return test.serviceState, nil
			}
			var dials atomic.Int32
			c.dialSocket = func(context.Context, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("unexpected dial")
			}
			result := c.Run(context.Background())
			if result.Status != check.StatusWarn ||
				result.Message != "estado do serviço local do Docker indisponível; consulta não realizada para evitar a ativação do serviço pelo socket" || dials.Load() != 0 {
				t.Fatalf("Run() = %+v, dial calls = %d", result, dials.Load())
			}
		})
	}
}

func TestDockerCheckSystemdCancellationSkipsProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testDockerCheck("/run/docker.sock")
	c.scope = dockerSystemSystemd
	c.unitState = func(ctx context.Context, _ dockerSystemdScope, _ string) (string, error) {
		cancel()
		return "", ctx.Err()
	}
	var dials atomic.Int32
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	result := c.Run(ctx)
	if result.Status != check.StatusWarn || result.Message != "varredura interrompida: contexto cancelado" || dials.Load() != 0 {
		t.Fatalf("Run() = %+v, dial calls = %d", result, dials.Load())
	}
}

func TestDockerCheckSystemdDeadlineSkipsProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c := testDockerCheck("/run/docker.sock")
	c.scope = dockerSystemSystemd
	c.unitState = func(ctx context.Context, _ dockerSystemdScope, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	var dials atomic.Int32
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	result := c.Run(ctx)
	if result.Status != check.StatusWarn || result.Message != "varredura interrompida: tempo limite do contexto excedido" || dials.Load() != 0 {
		t.Fatalf("Run() = %+v, dial calls = %d", result, dials.Load())
	}
}

func TestDockerCheckRootlessSystemdSocketActivation(t *testing.T) {
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, "docker.sock")
	c := testDockerCheck(path)
	c.scope = dockerSocketSystemdScope(path, runtimeDir)
	if c.scope != dockerUserSystemd {
		t.Fatalf("rootless socket scope = %d", c.scope)
	}
	c.unitState = func(_ context.Context, scope dockerSystemdScope, unit string) (string, error) {
		if scope != dockerUserSystemd {
			t.Fatalf("queried wrong systemd scope: %d", scope)
		}
		if unit == "docker.socket" {
			return "active", nil
		}
		return "inactive", nil
	}
	var dials atomic.Int32
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	result := c.Run(context.Background())
	if result.Status != check.StatusWarn ||
		result.Message != "o daemon local do Docker não está em execução; consulta não realizada para evitar a ativação do serviço pelo socket" || dials.Load() != 0 {
		t.Fatalf("Run() = %+v, dial calls = %d", result, dials.Load())
	}
}

func TestDockerCheckRootlessUnknownUserManagerSkipsProbe(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "missing-runtime-dir")
	path := filepath.Join(runtimeDir, "docker.sock")
	c := testDockerCheck(path)
	c.scope = dockerSocketSystemdScope(path, runtimeDir)
	if c.scope != dockerUnknownSystemd {
		t.Fatalf("unverifiable rootless socket scope = %d", c.scope)
	}
	var dials atomic.Int32
	c.dialSocket = func(context.Context, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}
	result := c.Run(context.Background())
	if result.Status != check.StatusWarn ||
		result.Message != "estado do serviço local do Docker indisponível; consulta não realizada para evitar a ativação do serviço pelo socket" || dials.Load() != 0 {
		t.Fatalf("Run() = %+v, dial calls = %d", result, dials.Load())
	}
}

func TestResolveDockerSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	rootless := filepath.Join(runtimeDir, "docker.sock")
	listener, err := net.Listen("unix", rootless)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	explicit := filepath.Join(t.TempDir(), "explicit.sock")
	tests := []struct {
		name       string
		dockerHost string
		runtimeDir string
		want       string
	}{
		{"explicit local socket", "unix://" + explicit, runtimeDir, explicit},
		{"rootless socket", "", runtimeDir, rootless},
		{"SSH endpoint ignored", "ssh://user@remote", runtimeDir, rootless},
		{"TCP endpoint ignored", "tcp://remote:2376", runtimeDir, rootless},
		{"malformed Unix endpoint ignored", "unix://remote/docker.sock", runtimeDir, rootless},
		{"default socket", "", t.TempDir(), defaultDockerSocket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveDockerSocket(test.dockerHost, test.runtimeDir); got != test.want {
				t.Fatalf("resolveDockerSocket() = %q; want %q", got, test.want)
			}
		})
	}
}

func TestDockerSocketSystemdScope(t *testing.T) {
	runtimeDir := t.TempDir()
	rootless := filepath.Join(runtimeDir, "docker.sock")
	listener, err := net.Listen("unix", rootless)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	alias := filepath.Join(t.TempDir(), "alias.sock")
	if err := os.Symlink(rootless, alias); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want dockerSystemdScope
	}{
		{"/run/docker.sock", dockerSystemSystemd},
		{"/var/run/docker.sock", dockerSystemSystemd},
		{rootless, dockerUserSystemd},
		{alias, dockerUserSystemd},
		{filepath.Join(t.TempDir(), "custom.sock"), dockerNoSystemd},
	}
	for _, test := range tests {
		if got := dockerSocketSystemdScope(test.path, runtimeDir); got != test.want {
			t.Errorf("dockerSocketSystemdScope(%q) = %d; want %d", test.path, got, test.want)
		}
	}
}

func TestDockerCheckRegisteredAndIgnoresContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "ssh://user@remote")
	t.Setenv("DOCKER_CONTEXT", "remote")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	for _, diagnostic := range Checks() {
		if diagnostic.Name() == "Docker" {
			c, ok := diagnostic.(dockerCheck)
			if !ok || c.socketPath != defaultDockerSocket || c.scope != dockerSystemSystemd || c.unitState == nil {
				t.Fatalf("Docker check selected a nonlocal endpoint: %#v", diagnostic)
			}
			return
		}
	}
	t.Fatal("Docker check is not registered")
}
