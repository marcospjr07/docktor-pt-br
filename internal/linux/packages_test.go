package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

var aptSimulationArgs = []string{"-s", "-o", "Dir::Cache::pkgcache=", "-o", "Dir::Cache::srcpkgcache=", "dist-upgrade"}

var aptFixtureNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

type aptStampInfo struct {
	modified time.Time
	mode     os.FileMode
}

func (aptStampInfo) Name() string         { return "update-stamp" }
func (aptStampInfo) Size() int64          { return 0 }
func (i aptStampInfo) Mode() os.FileMode  { return i.mode }
func (i aptStampInfo) ModTime() time.Time { return i.modified }
func (i aptStampInfo) IsDir() bool        { return i.mode.IsDir() }
func (aptStampInfo) Sys() any             { return nil }

func aptSummaryFixture(upgraded, installed, removed, kept int) string {
	return fmt.Sprintf("Reading package lists... Done\nBuilding dependency tree... Done\n%d upgraded, %d newly installed, %d to remove and %d not upgraded.\nInst a [1] (2)\nInst b (1)\n", upgraded, installed, removed, kept)
}

func packagesFixture(t *testing.T, summary string) packagesCheck {
	t.Helper()
	return packagesCheck{
		lookPath: func(name string) (string, error) {
			if name != "apt-get" {
				t.Fatalf("unexpected backend lookup: %q", name)
			}
			return "/fixture/apt-get", nil
		},
		runAPT: func(ctx context.Context, path string, args ...string) (aptCommandOutput, error) {
			if path != "/fixture/apt-get" || !slices.Equal(args, aptSimulationArgs) {
				t.Fatalf("unexpected APT command: %q %v", path, args)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("APT simulation has no deadline")
			}
			return aptCommandOutput{stdout: []byte(summary)}, nil
		},
		stat: func(path string) (os.FileInfo, error) {
			if path != aptRefreshStamp {
				t.Fatalf("unexpected metadata marker: %q", path)
			}
			return aptStampInfo{modified: aptFixtureNow.Add(-time.Hour)}, nil
		},
		now: func() time.Time { return aptFixtureNow },
	}
}

func TestPackagesUpgradeCountsAndUnusualSimulations(t *testing.T) {
	tests := []struct {
		name                               string
		upgraded, installed, removed, kept int
		message                            string
	}{
		{"zero", 0, 0, 0, 0, "nenhuma atualização listada"},
		{"one", 1, 0, 0, 0, "1 atualização disponível"},
		{"new dependency not counted", 59, 1, 0, 0, "59 atualizações disponíveis"},
		{"many", 10000, 0, 0, 0, "10000 atualizações disponíveis"},
		{"removals not counted", 12, 0, 3, 0, "12 atualizações disponíveis; a simulação inclui remoções"},
		{"only removals", 0, 0, 2, 0, "nenhuma atualização listada; a simulação inclui remoções"},
		{"only new packages", 0, 2, 0, 0, "nenhuma atualização listada; a simulação inclui novos pacotes"},
		{"kept back", 0, 0, 0, 4, "nenhuma atualização listada; 4 mantidos na versão atual"},
		{"upgrades and kept back", 10, 1, 0, 2, "10 atualizações disponíveis; 2 mantidos na versão atual"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := packagesFixture(t, aptSummaryFixture(tt.upgraded, tt.installed, tt.removed, tt.kept))
			got := c.Run(context.Background())
			want := tt.message + "; atualidade dos metadados de pacotes desconhecida"
			if got.Status != check.StatusWarn || got.Message != want {
				t.Fatalf("got %#v; want WARN %q", got, want)
			}
		})
	}
}

func TestAPTSummaryRejectsMissingMalformedAndAmbiguousData(t *testing.T) {
	valid := "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n"
	outputs := []string{
		"", "Inst a (1)\n", "0 upgraded\n",
		"0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded\n",
		"-1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n",
		"99999999999999999999999999999 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n",
		valid + valid,
		valid + "1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n",
		valid + "0 upgraded, 0 newly installed, 1 downgraded, 0 to remove and 0 not upgraded.\n",
	}
	for _, output := range outputs {
		t.Run(output, func(t *testing.T) {
			c := packagesFixture(t, output)
			got := c.Run(context.Background())
			if got.Status != check.StatusWarn || got.Message != "não foi possível interpretar o resumo do APT" {
				t.Fatalf("got %#v; malformed summary must warn", got)
			}
		})
	}
}

func TestPackagesLookupErrorsSkipAPT(t *testing.T) {
	for _, tt := range []struct {
		err     error
		message string
	}{
		{&exec.Error{Name: "apt-get", Err: exec.ErrNotFound}, "nenhum gerenciador de pacotes compatível encontrado"},
		{os.ErrNotExist, "nenhum gerenciador de pacotes compatível encontrado"},
		{os.ErrPermission, "busca pelo APT indisponível"},
	} {
		c := packagesCheck{lookPath: func(string) (string, error) { return "", tt.err }}
		got := c.Run(context.Background())
		if got.Status != check.StatusWarn || got.Message != tt.message {
			t.Fatalf("got %#v; want WARN %q", got, tt.message)
		}
	}
}

func TestPackagesQueryErrorsAndStderrCannotProducePASS(t *testing.T) {
	for _, tt := range []struct {
		name    string
		output  aptCommandOutput
		err     error
		message string
	}{
		{"exit error", aptCommandOutput{stdout: []byte(aptSummaryFixture(0, 0, 0, 0))}, errors.New("exit status 100"), "consulta APT indisponível"},
		{"stderr", aptCommandOutput{stdout: []byte(aptSummaryFixture(0, 0, 0, 0)), stderr: []byte("warning")}, nil, "a consulta APT relatou avisos"},
		{"output limit", aptCommandOutput{}, errAPTOutputLimit, "saída da consulta APT grande demais"},
		{"oversized injected output", aptCommandOutput{stdout: []byte(strings.Repeat("x", maxAPTStdout+1))}, nil, "saída da consulta APT grande demais"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := packagesFixture(t, "")
			c.runAPT = func(context.Context, string, ...string) (aptCommandOutput, error) { return tt.output, tt.err }
			got := c.Run(context.Background())
			if got.Status != check.StatusWarn || got.Message != tt.message {
				t.Fatalf("got %#v; want WARN %q", got, tt.message)
			}
		})
	}
}

func TestPackagesMetadataNeverTrustsRecentStamp(t *testing.T) {
	for _, tt := range []struct {
		name  string
		stamp os.FileInfo
		err   error
		old   bool
	}{
		{"recent", aptStampInfo{modified: aptFixtureNow.Add(-time.Hour)}, nil, false},
		{"exact age limit", aptStampInfo{modified: aptFixtureNow.Add(-aptRefreshMaxAge)}, nil, false},
		{"over age limit", aptStampInfo{modified: aptFixtureNow.Add(-aptRefreshMaxAge - time.Nanosecond)}, nil, true},
		{"old", aptStampInfo{modified: aptFixtureNow.Add(-7 * 24 * time.Hour)}, nil, true},
		{"missing", nil, os.ErrNotExist, false},
		{"unreadable", nil, os.ErrPermission, false},
		{"future", aptStampInfo{modified: aptFixtureNow.Add(time.Second)}, nil, false},
		{"zero timestamp", aptStampInfo{}, nil, false},
		{"before epoch", aptStampInfo{modified: time.Unix(-1, 0)}, nil, false},
		{"directory", aptStampInfo{modified: aptFixtureNow.Add(-48 * time.Hour), mode: os.ModeDir}, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, count := range []int{0, 12} {
				c := packagesFixture(t, aptSummaryFixture(count, 0, 0, 0))
				c.stat = func(string) (os.FileInfo, error) { return tt.stamp, tt.err }
				got := c.Run(context.Background())
				wantSuffix := "; atualidade dos metadados de pacotes desconhecida"
				if tt.old {
					wantSuffix = "; registro de atualização do APT com mais de 24h"
				}
				if got.Status != check.StatusWarn || !strings.HasSuffix(got.Message, wantSuffix) {
					t.Fatalf("got %#v for %d updates; want WARN with %q", got, count, wantSuffix)
				}
			}
		})
	}
}

func TestPackagesTimeoutAndCancellation(t *testing.T) {
	t.Run("deadline despite valid output", func(t *testing.T) {
		c := packagesFixture(t, "")
		c.timeout = 5 * time.Millisecond
		c.runAPT = func(ctx context.Context, _ string, _ ...string) (aptCommandOutput, error) {
			<-ctx.Done()
			return aptCommandOutput{stdout: []byte(aptSummaryFixture(0, 0, 0, 0))}, nil
		}
		got := c.Run(context.Background())
		if got.Status != check.StatusWarn || got.Message != "a consulta APT excedeu o tempo limite" {
			t.Fatalf("got %#v", got)
		}
	})
	for _, phase := range []string{"before scan", "lookup", "query", "metadata"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := packagesFixture(t, aptSummaryFixture(0, 0, 0, 0))
			switch phase {
			case "before scan":
				cancel()
				c.lookPath = func(string) (string, error) {
					t.Fatal("lookup on canceled scan")
					return "", nil
				}
			case "lookup":
				c.lookPath = func(string) (string, error) {
					cancel()
					return "/fixture/apt-get", nil
				}
				c.runAPT = func(context.Context, string, ...string) (aptCommandOutput, error) {
					t.Fatal("query after canceled lookup")
					return aptCommandOutput{}, nil
				}
			case "query":
				c.runAPT = func(context.Context, string, ...string) (aptCommandOutput, error) {
					cancel()
					return aptCommandOutput{stdout: []byte(aptSummaryFixture(0, 0, 0, 0))}, nil
				}
			case "metadata":
				c.stat = func(string) (os.FileInfo, error) {
					cancel()
					return aptStampInfo{modified: aptFixtureNow}, nil
				}
			}
			got := c.Run(ctx)
			if got.Status != check.StatusWarn || !strings.Contains(got.Message, "varredura interrompida") {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestPackagesCheckRegisteredInOrder(t *testing.T) {
	want := []string{"Sistema operacional", "Tempo ativo", "Memória", "Disco raiz", "Systemd", "SSH", "Firewall", "Pacotes", "Docker"}
	var names []string
	for _, c := range Checks() {
		names = append(names, c.Name())
	}
	if !slices.Equal(names, want) {
		t.Fatalf("check order = %v; want %v", names, want)
	}
}

func TestAPTCommandRunnerControlsLocaleAndSeparatesStreams(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKTOR_APT_HELPER", "streams")
	t.Setenv("LC_ALL", "invalid")
	t.Setenv("APT_CONFIG", "/fixture/apt-config")
	output, err := runAPTCommand(context.Background(), path, "-test.run=^TestAPTCommandHelper$")
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 100 || string(output.stdout) != "summary\n" || string(output.stderr) != "diagnostic\n" {
		t.Fatalf("got stdout %q, stderr %q, error %v", output.stdout, output.stderr, err)
	}
}

func TestAPTCommandRunnerCapsBothStreams(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			t.Setenv("DOCKTOR_APT_HELPER", stream)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			output, err := runAPTCommand(ctx, path, "-test.run=^TestAPTCommandHelper$")
			if !errors.Is(err, errAPTOutputLimit) || len(output.stdout) > maxAPTStdout || len(output.stderr) > maxAPTStderr {
				t.Fatalf("captured stdout %d, stderr %d, error %v", len(output.stdout), len(output.stderr), err)
			}
		})
	}
}

func TestAPTOutputLimitCannotBeBypassedByIOCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := aptOutputBuffer{limit: 16, cancel: cancel}
	_, err := io.Copy(&output, io.LimitReader(strings.NewReader(strings.Repeat("x", 17)), 17))
	if !errors.Is(err, errAPTOutputLimit) || len(output.Bytes()) > 16 || ctx.Err() == nil {
		t.Fatalf("captured %d bytes, error %v, context %v", len(output.Bytes()), err, ctx.Err())
	}
}

func TestAPTOutputLimitAllowsExactBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := aptOutputBuffer{limit: 16, cancel: cancel}
	if n, err := output.Write([]byte(strings.Repeat("x", 16))); n != 16 || err != nil || ctx.Err() != nil {
		t.Fatalf("exact boundary write = %d, %v; context = %v", n, err, ctx.Err())
	}
	if _, err := output.Write([]byte("x")); !errors.Is(err, errAPTOutputLimit) || len(output.Bytes()) != 16 || ctx.Err() == nil {
		t.Fatalf("overflow error = %v; captured %d bytes; context = %v", err, len(output.Bytes()), ctx.Err())
	}
}

func TestAPTCommandRunnerRespectsDeadline(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKTOR_APT_HELPER", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = runAPTCommand(ctx, path, "-test.run=^TestAPTCommandHelper$")
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("process error = %v; context = %v", err, ctx.Err())
	}
}

func TestAPTCommandHelper(t *testing.T) {
	switch os.Getenv("DOCKTOR_APT_HELPER") {
	case "":
		return
	case "streams":
		if os.Getenv("LC_ALL") != "C" || os.Getenv("APT_CONFIG") != "/fixture/apt-config" {
			os.Exit(3)
		}
		fmt.Fprint(os.Stdout, "summary\n")
		fmt.Fprint(os.Stderr, "diagnostic\n")
		os.Exit(100)
	case "stdout":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxAPTStdout+1))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", maxAPTStderr+1))
	case "wait":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}
