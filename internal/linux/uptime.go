package linux

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

type uptimeCheck struct {
	readFile readFileFunc
}

func (uptimeCheck) Name() string { return "Tempo ativo" }

func (c uptimeCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}
	data, err := c.readFile("/proc/uptime")
	if err != nil {
		return check.Result{Status: check.StatusWarn, Message: "indisponível: " + err.Error()}
	}
	uptime, err := parseUptime(data)
	if err != nil {
		return check.Result{Status: check.StatusWarn, Message: "indisponível: " + err.Error()}
	}
	return check.Result{Status: check.StatusPass, Message: formatUptime(uptime)}
}

func parseUptime(data []byte) (time.Duration, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, errors.New("/proc/uptime vazio")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds >= float64(1<<63-1)/float64(time.Second) {
		return 0, fmt.Errorf("tempo ativo inválido %q", fields[0])
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func formatUptime(uptime time.Duration) string {
	seconds := int64(uptime / time.Second)
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dmin", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dmin", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dmin %ds", minutes, seconds%60)
	}
	return fmt.Sprintf("%ds", seconds)
}
