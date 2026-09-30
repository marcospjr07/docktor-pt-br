package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func sshFixture(t *testing.T, main string, includes map[string]string) sshCheck {
	t.Helper()
	dir := t.TempDir()
	for name, contents := range includes {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(path, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	return sshCheck{configPath: path, includeBase: dir, readFile: readSSHFile}
}

func assertSSHResult(t *testing.T, result check.Result, status check.Status, message string) {
	t.Helper()
	if result.Status != status || result.Message != message {
		t.Fatalf("got %#v; want %s %q", result, status, message)
	}
}

func TestSSHCheckPolicies(t *testing.T) {
	tests := []struct {
		name, config, message string
		status                check.Status
	}{
		{"hardened", "PermitRootLogin no\nPasswordAuthentication no\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"root enabled", "PermitRootLogin yes\nPasswordAuthentication no\n", "login de root ativado; autenticação por senha desativada", check.StatusWarn},
		{"password enabled", "PermitRootLogin no\nPasswordAuthentication yes\n", "login de root desativado; autenticação por senha ativada", check.StatusWarn},
		{"both permissive", "PermitRootLogin yes\nPasswordAuthentication yes\n", "login de root ativado; autenticação por senha ativada", check.StatusWarn},
		{"one unknown", "PermitRootLogin no\n", "login de root desativado; configuração de autenticação por senha desconhecida", check.StatusWarn},
		{"both unknown", "# no explicit policy\n", "configuração de login de root desconhecida; configuração de autenticação por senha desconhecida", check.StatusWarn},
		{"restricted root", "PermitRootLogin prohibit-password\nPasswordAuthentication no\n", "login de root limitado a métodos sem senha; autenticação por senha desativada", check.StatusWarn},
		{"forced commands", "PermitRootLogin forced-commands-only\nPasswordAuthentication no\n", "login de root limitado a comandos forçados; autenticação por senha desativada", check.StatusWarn},
		{"deprecated alias", "PermitRootLogin without-password\nPasswordAuthentication no\n", "login de root limitado a métodos sem senha; autenticação por senha desativada", check.StatusWarn},
		{"comments whitespace and case", "\t# PermitRootLogin yes\n\tPeRmItRoOtLoGiN = No  # comment\n PasswordAuthentication\t=\tNO\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"quoted value", "PermitRootLogin \"no\"\nPasswordAuthentication 'no'\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"first global value wins", "PermitRootLogin no\nPasswordAuthentication no\nPermitRootLogin yes\nPasswordAuthentication yes\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertSSHResult(t, sshFixture(t, tt.config, nil).Run(context.Background()), tt.status, tt.message)
		})
	}
}

func TestSSHCheckIncludesAndPrecedence(t *testing.T) {
	tests := []struct {
		name, main, message string
		includes            map[string]string
		status              check.Status
	}{
		{
			name:     "simple relative include",
			main:     "Include policy.conf\nPasswordAuthentication no\n",
			includes: map[string]string{"policy.conf": "PermitRootLogin no\n"},
			status:   check.StatusPass,
			message:  "login de root desativado; autenticação por senha desativada",
		},
		{
			name:     "relative include uses server config directory",
			main:     "Include nested/outer.conf\nPasswordAuthentication no\n",
			includes: map[string]string{"nested/outer.conf": "Include inner.conf\n", "inner.conf": "PermitRootLogin no\n"},
			status:   check.StatusPass,
			message:  "login de root desativado; autenticação por senha desativada",
		},
		{
			name: "glob sorts before applying first value",
			main: "Include sshd_config.d/*.conf\nPermitRootLogin yes\nPasswordAuthentication yes\n",
			includes: map[string]string{
				"sshd_config.d/20.conf": "PermitRootLogin yes\nPasswordAuthentication yes\n",
				"sshd_config.d/10.conf": "PermitRootLogin no\nPasswordAuthentication no\n",
			},
			status:  check.StatusPass,
			message: "login de root desativado; autenticação por senha desativada",
		},
		{
			name: "glob does not include hidden files",
			main: "Include sshd_config.d/*.conf\nPasswordAuthentication no\n",
			includes: map[string]string{
				"sshd_config.d/.00.conf": "PermitRootLogin no\n",
				"sshd_config.d/10.conf":  "PermitRootLogin yes\n",
			},
			status:  check.StatusWarn,
			message: "login de root ativado; autenticação por senha desativada",
		},
		{
			name:    "unmatched glob is valid",
			main:    "Include sshd_config.d/*.conf\nPermitRootLogin no\nPasswordAuthentication no\n",
			status:  check.StatusPass,
			message: "login de root desativado; autenticação por senha desativada",
		},
		{
			name:    "unmatched literal include is valid",
			main:    "Include missing.conf\nPermitRootLogin no\nPasswordAuthentication no\n",
			status:  check.StatusPass,
			message: "login de root desativado; autenticação por senha desativada",
		},
		{
			name:     "quoted include path",
			main:     "Include \"policy files/root.conf\"\nPasswordAuthentication no\n",
			includes: map[string]string{"policy files/root.conf": "PermitRootLogin no\n"},
			status:   check.StatusPass,
			message:  "login de root desativado; autenticação por senha desativada",
		},
		{
			name:     "include in Match is conditional",
			main:     "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n Include admin.conf\n",
			includes: map[string]string{"admin.conf": "PermitRootLogin yes\n"},
			status:   check.StatusWarn,
			message:  "login de root condicional; autenticação por senha desativada",
		},
		{
			name:     "included Match does not leak into parent",
			main:     "Include conditional.conf\nPermitRootLogin no\nPasswordAuthentication no\n",
			includes: map[string]string{"conditional.conf": "Match User admin\n X11Forwarding no\n"},
			status:   check.StatusPass,
			message:  "login de root desativado; autenticação por senha desativada",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertSSHResult(t, sshFixture(t, tt.main, tt.includes).Run(context.Background()), tt.status, tt.message)
		})
	}
}

func TestSSHCheckMatchPolicy(t *testing.T) {
	tests := []struct {
		name, config, message string
		status                check.Status
	}{
		{"root varies", "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n PermitRootLogin yes\n", "login de root condicional; autenticação por senha desativada", check.StatusWarn},
		{"password varies", "PermitRootLogin no\nPasswordAuthentication no\nMatch Address 192.0.2.*\n PasswordAuthentication yes\n", "login de root desativado; autenticação por senha condicional", check.StatusWarn},
		{"irrelevant Match", "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n X11Forwarding no\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"same Match policy", "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n PermitRootLogin no\n PasswordAuthentication no\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"Match all overrides global", "PermitRootLogin yes\nPasswordAuthentication yes\nMatch all\n PermitRootLogin no\n PasswordAuthentication no\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"earlier Match overrides Match all", "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n PermitRootLogin yes\nMatch all\n PermitRootLogin no\n", "login de root condicional; autenticação por senha desativada", check.StatusWarn},
		{"first value in Match block wins", "PermitRootLogin no\nPasswordAuthentication no\nMatch User admin\n PermitRootLogin no\n PermitRootLogin yes\n", "login de root desativado; autenticação por senha desativada", check.StatusPass},
		{"conditional with unknown global", "PermitRootLogin no\nMatch User admin\n PasswordAuthentication no\n", "login de root desativado; autenticação por senha condicional", check.StatusWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertSSHResult(t, sshFixture(t, tt.config, nil).Run(context.Background()), tt.status, tt.message)
		})
	}
}

func TestSSHCheckUnavailableAndAmbiguousConfig(t *testing.T) {
	t.Run("main absent", func(t *testing.T) {
		c := sshCheck{configPath: filepath.Join(t.TempDir(), "sshd_config"), readFile: readSSHFile}
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "configuração do servidor SSH não encontrada")
	})
	t.Run("main unreadable", func(t *testing.T) {
		c := sshFixture(t, "PermitRootLogin no\nPasswordAuthentication no\n", nil)
		c.readFile = func(context.Context, string) ([]byte, error) { return nil, os.ErrPermission }
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "configuração do servidor SSH ilegível")
	})
	t.Run("include unreadable", func(t *testing.T) {
		c := sshFixture(t, "Include policy.conf\nPermitRootLogin no\nPasswordAuthentication no\n", map[string]string{"policy.conf": "PermitRootLogin yes\n"})
		c.readFile = func(ctx context.Context, path string) ([]byte, error) {
			if filepath.Base(path) == "policy.conf" {
				return nil, os.ErrPermission
			}
			return readSSHFile(ctx, path)
		}
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "não foi possível ler um arquivo incluído por Include no SSH")
	})
	t.Run("include cycle", func(t *testing.T) {
		c := sshFixture(t, "Include a.conf\nPermitRootLogin no\nPasswordAuthentication no\n", map[string]string{"a.conf": "Include b.conf\n", "b.conf": "Include a.conf\n"})
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "ciclo de inclusão de arquivos por Include no SSH detectado")
	})
	t.Run("wildcard directory unsupported", func(t *testing.T) {
		c := sshFixture(t, "Include */policy.conf\nPermitRootLogin no\nPasswordAuthentication no\n", nil)
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "não foi possível interpretar a configuração SSH")
	})
	t.Run("malformed tracked value", func(t *testing.T) {
		c := sshFixture(t, "PermitRootLogin no extra\nPasswordAuthentication no\n", nil)
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "não foi possível interpretar a configuração SSH")
	})
	t.Run("malformed quote", func(t *testing.T) {
		c := sshFixture(t, "PermitRootLogin \"no\nPasswordAuthentication no\n", nil)
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "não foi possível interpretar a configuração SSH")
	})
	t.Run("Match all cannot have other criteria", func(t *testing.T) {
		c := sshFixture(t, "PermitRootLogin no\nPasswordAuthentication no\nMatch all User admin\n PermitRootLogin no\n", nil)
		assertSSHResult(t, c.Run(context.Background()), check.StatusWarn, "não foi possível interpretar a configuração SSH")
	})
}

func TestSSHCheckCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	c := sshFixture(t, "PermitRootLogin no\nPasswordAuthentication no\n", nil)
	c.readFile = func(_ context.Context, _ string) ([]byte, error) {
		calls++
		cancel()
		return []byte("PermitRootLogin no\nPasswordAuthentication no\n"), nil
	}
	result := c.Run(ctx)
	if result.Status != check.StatusWarn || !strings.Contains(result.Message, "varredura interrompida") || calls != 1 {
		t.Fatalf("got %#v after %d reads; want interrupted warning", result, calls)
	}
	result = c.Run(ctx)
	if result.Status != check.StatusWarn || calls != 1 {
		t.Fatalf("got %#v after %d reads; canceled context must skip file reads", result, calls)
	}
}

func TestSSHCheckRegisteredInOrder(t *testing.T) {
	want := []string{"Sistema operacional", "Tempo ativo", "Memória", "Disco raiz", "Systemd", "SSH", "Firewall", "Pacotes", "Docker"}
	checks := Checks()
	names := make([]string, len(checks))
	for i, diagnostic := range checks {
		names[i] = diagnostic.Name()
	}
	if !slices.Equal(names, want) {
		t.Fatalf("check order = %v, want %v", names, want)
	}
}

func TestSSHReadFileRejectsNonRegularAndOversized(t *testing.T) {
	dir := t.TempDir()
	if _, err := readSSHFile(context.Background(), dir); !errors.Is(err, errSSHSyntax) {
		t.Fatalf("directory read error = %v; want non-regular file error", err)
	}
	path := filepath.Join(dir, "large.conf")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxSSHFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSSHFile(context.Background(), path); !errors.Is(err, errSSHLimit) {
		t.Fatalf("oversized file error = %v; want limit error", err)
	}
}
