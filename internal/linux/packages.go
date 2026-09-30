package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

const (
	aptQueryTimeout  = 10 * time.Second
	aptRefreshMaxAge = 24 * time.Hour
	aptRefreshStamp  = "/var/lib/apt/periodic/update-stamp"
	maxAPTStdout     = 1 << 20
	maxAPTStderr     = 64 << 10
)

var (
	errAPTOutputLimit = errors.New("a saída da consulta APT excedeu o limite")
	aptSummaryPattern = regexp.MustCompile(`^([0-9]+) upgraded, ([0-9]+) newly installed, ([0-9]+) to remove and ([0-9]+) not upgraded\.$`)
)

type aptCommandOutput struct {
	stdout []byte
	stderr []byte
}

type aptCommandRunner func(context.Context, string, ...string) (aptCommandOutput, error)

type packagesCheck struct {
	lookPath func(string) (string, error)
	runAPT   aptCommandRunner
	stat     func(string) (os.FileInfo, error)
	now      func() time.Time
	timeout  time.Duration
}

func (packagesCheck) Name() string { return "Pacotes" }

func (c packagesCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	path, err := c.lookPath("apt-get")
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return packagesWarning("nenhum gerenciador de pacotes compatível encontrado")
		}
		return packagesWarning("busca pelo APT indisponível")
	}
	if path == "" {
		return packagesWarning("busca pelo APT indisponível")
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = aptQueryTimeout
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Simulation disables locks and does not invoke dpkg. Disable persistent
	// cache generation too, as documented by apt.conf(5).
	output, err := c.runAPT(queryCtx, path, "-s", "-o", "Dir::Cache::pkgcache=", "-o", "Dir::Cache::srcpkgcache=", "dist-upgrade")
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	if errors.Is(queryCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return packagesWarning("a consulta APT excedeu o tempo limite")
	}
	if errors.Is(err, context.Canceled) {
		return packagesWarning("a consulta APT foi interrompida")
	}
	if errors.Is(err, errAPTOutputLimit) || len(output.stdout) > maxAPTStdout || len(output.stderr) > maxAPTStderr {
		return packagesWarning("saída da consulta APT grande demais")
	}
	if err != nil {
		return packagesWarning("consulta APT indisponível")
	}
	if len(output.stderr) != 0 {
		return packagesWarning("a consulta APT relatou avisos")
	}
	summary, err := parseAPTSummary(output.stdout)
	if err != nil {
		return packagesWarning("não foi possível interpretar o resumo do APT")
	}

	message := "nenhuma atualização listada"
	if summary.upgraded > 0 {
		label := "atualizações"
		availability := "disponíveis"
		if summary.upgraded == 1 {
			label = "atualização"
			availability = "disponível"
		}
		message = fmt.Sprintf("%d %s %s", summary.upgraded, label, availability)
	}
	if summary.removed > 0 {
		message += "; a simulação inclui remoções"
	} else if summary.upgraded == 0 && summary.installed > 0 {
		message += "; a simulação inclui novos pacotes"
	}
	if summary.kept > 0 {
		message += fmt.Sprintf("; %d mantidos na versão atual", summary.kept)
	}
	stamp, err := c.stat(aptRefreshStamp)
	now := c.now()
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	if oldAPTRefresh(now, stamp, err) {
		message += "; registro de atualização do APT com mais de 24h"
	} else {
		message += "; atualidade dos metadados de pacotes desconhecida"
	}
	// Neither update-stamp nor Post-Invoke-Success proves every repository was
	// refreshed: APT can retain old indexes after transient failures. A recent
	// stamp must therefore never turn a zero-upgrade summary into PASS.
	return packagesWarning(message)
}

type aptSummary struct {
	upgraded  int
	installed int
	removed   int
	kept      int
}

func parseAPTSummary(output []byte) (aptSummary, error) {
	var summary aptSummary
	found := false
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		matches := aptSummaryPattern.FindSubmatch(line)
		if matches == nil {
			// Reject an unfamiliar summary as well as a missing one. In
			// particular, do not silently ignore a second, changed format.
			if bytes.Contains(line, []byte("newly installed,")) || bytes.Contains(line, []byte("to remove and")) {
				return aptSummary{}, errors.New("resumo do APT não reconhecido")
			}
			continue
		}
		if found {
			return aptSummary{}, errors.New("resumo do APT ambíguo")
		}
		counts := []*int{&summary.upgraded, &summary.installed, &summary.removed, &summary.kept}
		for i, count := range counts {
			value, err := strconv.Atoi(string(matches[i+1]))
			if err != nil {
				return aptSummary{}, err
			}
			*count = value
		}
		found = true
	}
	if !found {
		return aptSummary{}, errors.New("resumo do APT ausente")
	}
	return summary, nil
}

// This is evidence of an old recorded periodic refresh, not proof that a
// subsequent manual refresh did not happen. Recent, absent, or incoherent
// timestamps leave freshness unknown; no release-file date is trusted.
func oldAPTRefresh(now time.Time, stamp os.FileInfo, err error) bool {
	if err != nil || stamp == nil || !stamp.Mode().IsRegular() || stamp.ModTime().IsZero() || now.IsZero() ||
		stamp.ModTime().Before(time.Unix(0, 0)) || stamp.ModTime().After(now) {
		return false
	}
	return now.Sub(stamp.ModTime()) > aptRefreshMaxAge
}

func packagesWarning(message string) check.Result {
	return check.Result{Status: check.StatusWarn, Message: message}
}

func runAPTCommand(ctx context.Context, path string, args ...string) (aptCommandOutput, error) {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(processCtx, path, args...)
	// LC_ALL overrides the other locale variables; preserve host APT config
	// and HOME so the query describes the host's normal package policy.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "LC_ALL=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	cmd.WaitDelay = 200 * time.Millisecond
	stdout := aptOutputBuffer{limit: maxAPTStdout, cancel: cancel}
	stderr := aptOutputBuffer{limit: maxAPTStderr, cancel: cancel}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := aptCommandOutput{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	if stdout.exceeded || stderr.exceeded {
		return output, errAPTOutputLimit
	}
	return output, err
}

// Each stream has one exec copy goroutine; Run waits for both before reading
// these buffers. Overflow cancels the process and never yields usable data.
type aptOutputBuffer struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (b *aptOutputBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errAPTOutputLimit
	}
	return b.buffer.Write(data)
}

func (b *aptOutputBuffer) Bytes() []byte { return b.buffer.Bytes() }
