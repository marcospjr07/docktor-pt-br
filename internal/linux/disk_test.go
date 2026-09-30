package linux

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestCalculateDiskUsage(t *testing.T) {
	stats := filesystemStats{blockSize: 1024 * 1024, blocks: 1000, free: 300, available: 200}
	got, err := calculateDiskUsage(stats)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.usedGiB-700.0/1024) > 0.000001 || math.Abs(got.totalGiB-900.0/1024) > 0.000001 || math.Abs(got.percent-700.0/900*100) > 0.000001 {
		t.Fatalf("calculateDiskUsage() = %+v", got)
	}
	for _, invalid := range []filesystemStats{
		{blockSize: 0, blocks: 100, free: 10, available: 10},
		{blockSize: 4096, blocks: 100, free: 101, available: 10},
		{blockSize: 4096, blocks: 100, free: 10, available: 11},
		{blockSize: 4096, blocks: 100, free: 100, available: 0},
	} {
		if _, err := calculateDiskUsage(invalid); err == nil {
			t.Errorf("calculateDiskUsage(%+v) should fail", invalid)
		}
	}
}

func TestUtilizationThresholds(t *testing.T) {
	tests := []struct {
		percent float64
		want    check.Status
	}{
		{79.9, check.StatusPass},
		{80, check.StatusWarn},
		{94.9, check.StatusWarn},
		{95, check.StatusFail},
	}
	for _, test := range tests {
		if got := statusForPercent(test.percent); got != test.want {
			t.Errorf("statusForPercent(%v) = %q, want %q", test.percent, got, test.want)
		}
	}
}

func TestDiskCheckWarnsOnStatError(t *testing.T) {
	c := diskCheck{statFS: func(path string) (filesystemStats, error) {
		if path != "/" {
			t.Errorf("stat unexpected path %q", path)
		}
		return filesystemStats{}, errors.New("missing")
	}}
	if result := c.Run(context.Background()); result.Status != check.StatusWarn {
		t.Fatalf("expected warning, got %+v", result)
	}
}
