package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const (
	systemdQueryTimeout = 3 * time.Second
	maxFailedUnitNames  = 3
)

type systemdCheck struct {
	runSystemctl systemctlRunner
	timeout      time.Duration
}

func (systemdCheck) Name() string { return "Systemd" }

func (c systemdCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = systemdQueryTimeout
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := c.runSystemctl(queryCtx, "--system", "list-units", "--type=service", "--state=failed", "--output=json", "--no-pager")
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	if errors.Is(queryCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return systemdWarning("a consulta de serviços do systemd excedeu o tempo limite")
	}
	if errors.Is(err, context.Canceled) {
		return systemdWarning("a consulta de serviços do systemd foi interrompida")
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return systemdWarning("systemctl não encontrado")
	}
	if err != nil {
		return systemdWarning("não foi possível consultar os serviços do systemd")
	}

	units, err := parseFailedServiceUnits(output)
	if err != nil {
		return systemdWarning("dados de serviços do systemd inválidos")
	}
	if len(units) == 0 {
		return check.Result{Status: check.StatusPass, Message: "nenhuma unidade de serviço com falha"}
	}

	visible := units
	if len(visible) > maxFailedUnitNames {
		visible = visible[:maxFailedUnitNames]
	}
	label := "serviço com falha"
	if len(units) != 1 {
		label = "serviços com falha"
	}
	message := fmt.Sprintf("%d %s: %s", len(units), label, strings.Join(visible, ", "))
	if remaining := len(units) - len(visible); remaining > 0 {
		message += fmt.Sprintf(" (+%d a mais)", remaining)
	}
	return check.Result{Status: check.StatusFail, Message: message}
}

func parseFailedServiceUnits(output []byte) ([]string, error) {
	output = bytes.TrimSpace(output)
	// Only an explicit JSON array can establish that no units failed. Empty,
	// null, or truncated output must not turn into a false PASS.
	if len(output) == 0 || output[0] != '[' {
		return nil, errors.New("era esperada uma lista de unidades do systemd")
	}
	var entries []struct {
		Unit   string `json:"unit"`
		Active string `json:"active"`
	}
	if err := json.Unmarshal(output, &entries); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(entries))
	units := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Unit
		if entry.Active != "failed" || len(name) <= len(".service") || !strings.HasSuffix(name, ".service") ||
			strings.IndexFunc(name, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
			return nil, errors.New("unidade de serviço com falha inválida")
		}
		if _, exists := seen[name]; !exists {
			seen[name] = struct{}{}
			units = append(units, name)
		}
	}
	sort.Strings(units)
	return units, nil
}

func systemdWarning(message string) check.Result {
	return check.Result{Status: check.StatusWarn, Message: message}
}
