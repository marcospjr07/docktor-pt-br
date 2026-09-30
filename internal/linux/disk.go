package linux

import (
	"context"
	"errors"
	"fmt"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

var errInvalidFilesystem = errors.New("estatísticas do sistema de arquivos inválidas")

type diskCheck struct {
	statFS statFSFunc
}

type diskUsage struct {
	usedGiB  float64
	totalGiB float64
	percent  float64
}

func (diskCheck) Name() string { return "Disco raiz" }

func (c diskCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	stats, err := c.statFS("/")
	if err != nil {
		return check.Result{Status: check.StatusWarn, Message: "indisponível: " + err.Error()}
	}
	usage, err := calculateDiskUsage(stats)
	if err != nil {
		return check.Result{Status: check.StatusWarn, Message: "indisponível: " + err.Error()}
	}
	return check.Result{
		Status:  statusForPercent(usage.percent),
		Message: fmt.Sprintf("%.1f / %.1f GiB em uso (%.1f%%)", usage.usedGiB, usage.totalGiB, usage.percent),
	}
}

func calculateDiskUsage(stats filesystemStats) (diskUsage, error) {
	if stats.blockSize == 0 || stats.blocks == 0 || stats.free > stats.blocks || stats.available > stats.free {
		return diskUsage{}, errInvalidFilesystem
	}
	usedBlocks := stats.blocks - stats.free
	usableBlocks := float64(usedBlocks) + float64(stats.available)
	if usableBlocks == 0 {
		return diskUsage{}, errInvalidFilesystem
	}
	const bytesPerGiB = 1024 * 1024 * 1024
	return diskUsage{
		usedGiB:  float64(usedBlocks) * float64(stats.blockSize) / bytesPerGiB,
		totalGiB: usableBlocks * float64(stats.blockSize) / bytesPerGiB,
		percent:  100 * float64(usedBlocks) / usableBlocks,
	}, nil
}
