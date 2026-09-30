package linux

import (
	"context"
	"errors"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestParseMeminfo(t *testing.T) {
	data := []byte("MemTotal:       1000000 kB\nMemFree: 1 kB\nMemAvailable:   200000 kB\n")
	got, err := parseMeminfo(data)
	if err != nil || got != (memoryStats{totalKB: 1000000, availableKB: 200000}) {
		t.Fatalf("parseMeminfo() = %+v, %v", got, err)
	}
	for _, input := range []string{
		"MemTotal: 100 kB\n",
		"MemTotal: 0 kB\nMemAvailable: 0 kB\n",
		"MemTotal: 100 kB\nMemAvailable: 101 kB\n",
		"MemTotal: broken kB\nMemAvailable: 20 kB\n",
	} {
		if _, err := parseMeminfo([]byte(input)); err == nil {
			t.Errorf("parseMeminfo(%q) should fail", input)
		}
	}
}

func TestMemoryCheckUsesAvailableMemory(t *testing.T) {
	c := memoryCheck{readFile: func(path string) ([]byte, error) {
		if path != "/proc/meminfo" {
			t.Errorf("read unexpected path %q", path)
		}
		return []byte("MemTotal: 1000000 kB\nMemAvailable: 200000 kB\n"), nil
	}}
	if result := c.Run(context.Background()); result.Status != check.StatusWarn {
		t.Fatalf("expected 80%% utilization warning, got %+v", result)
	}
	c.readFile = func(string) ([]byte, error) { return nil, errors.New("missing") }
	if result := c.Run(context.Background()); result.Status != check.StatusWarn {
		t.Fatalf("expected unavailable warning, got %+v", result)
	}
}
