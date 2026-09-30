package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestRunCompletedScanReturnsZeroForEveryFinding(t *testing.T) {
	for _, args := range [][]string{{"scan"}, {"scan", "--json"}} {
		for _, status := range []check.Status{check.StatusPass, check.StatusWarn, check.StatusFail} {
			t.Run(strings.Join(args, " ")+"/"+string(status), func(t *testing.T) {
				ctx := context.WithValue(context.Background(), contextKey{}, "scan context")
				var stdout, stderr bytes.Buffer
				calls := 0
				scanFn := func(got context.Context) check.Report {
					calls++
					if got != ctx {
						t.Error("scan did not receive the CLI context")
					}
					results := []check.Result{{Name: "Example", Status: status, Message: "finding"}}
					return check.Report{Results: results, Summary: check.Summarize(results)}
				}
				if code := run(ctx, args, &stdout, &stderr, scanFn); code != 0 {
					t.Errorf("run() exit code = %d, want 0", code)
				}
				if calls != 1 || stderr.Len() != 0 {
					t.Errorf("unexpected scan: calls=%d, stderr=%q", calls, stderr.String())
				}
				if len(args) == 1 {
					if !strings.Contains(stdout.String(), "Resumo:") {
						t.Errorf("default output is not terminal text: %q", stdout.String())
					}
					return
				}
				var output struct {
					Checks []struct {
						Name    string `json:"name"`
						Status  string `json:"status"`
						Message string `json:"message"`
					} `json:"checks"`
					Summary struct {
						Total int `json:"total"`
					} `json:"summary"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
					t.Fatalf("stdout is not a single JSON report: %v; output: %q", err, stdout.String())
				}
				if len(output.Checks) != 1 || output.Checks[0].Name != "Example" || output.Checks[0].Status != string(status) ||
					output.Checks[0].Message != "finding" || output.Summary.Total != 1 {
					t.Fatalf("scan data missing from JSON report: %#v", output)
				}
			})
		}
	}
}

func TestRunHelpDoesNotScan(t *testing.T) {
	for _, args := range [][]string{
		nil, {"--help"}, {"-h"}, {"help"}, {"scan", "--help"}, {"scan", "-h"},
		{"scan", "--json", "--help"}, {"scan", "--help", "--json"}, {"scan", "--json", "-h"},
	} {
		var stdout, stderr bytes.Buffer
		scanFn := func(context.Context) check.Report {
			t.Fatal("help executed a host scan")
			return check.Report{}
		}
		if code := run(context.Background(), args, &stdout, &stderr, scanFn); code != 0 {
			t.Errorf("run(%v) exit code = %d, want 0", args, code)
		}
		if !strings.Contains(stdout.String(), "Uso:") || !strings.Contains(stdout.String(), "--json") || stderr.Len() != 0 {
			t.Errorf("run(%v) output: stdout=%q, stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestRunInvalidUsageReturnsTwo(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"}, {"scan", "extra"}, {"--help", "extra"}, {"--json", "scan"},
		{"scan", "--unknown"}, {"scan", "--json", "extra"}, {"scan", "extra", "--json"},
		{"scan", "--json", "--unknown"}, {"scan", "--json", "--json"}, {"scan", "--json=false"},
		{"scan", "--help", "extra"}, {"scan", "--json", "--help", "extra"},
		{"scan", "--help", "-h"},
	} {
		var stdout, stderr bytes.Buffer
		scanFn := func(context.Context) check.Report {
			t.Fatal("invalid usage executed a host scan")
			return check.Report{}
		}
		if code := run(context.Background(), args, &stdout, &stderr, scanFn); code != 2 {
			t.Errorf("run(%v) exit code = %d, want 2", args, code)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) usage error streams: stdout=%q, stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestRunReportWriteErrorIsReported(t *testing.T) {
	for _, args := range [][]string{{"scan"}, {"scan", "--json"}} {
		var stderr bytes.Buffer
		scanFn := func(context.Context) check.Report { return check.Report{} }
		if code := run(context.Background(), args, failingWriter{}, &stderr, scanFn); code != 1 {
			t.Errorf("run(%v) exit code = %d, want 1", args, code)
		}
		if !strings.Contains(stderr.String(), "não foi possível escrever o relatório: output unavailable") {
			t.Errorf("missing report write error for %v: %q", args, stderr.String())
		}
	}
}

type contextKey struct{}
