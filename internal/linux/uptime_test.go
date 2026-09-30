package linux

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestParseAndFormatUptime(t *testing.T) {
	got, err := parseUptime([]byte("90061.50 12345.00\n"))
	if err != nil || got != 90061*time.Second+500*time.Millisecond {
		t.Fatalf("parseUptime() = %v, %v", got, err)
	}
	if formatted := formatUptime(got); formatted != "1d 1h 1min" {
		t.Fatalf("formatUptime() = %q", formatted)
	}
	for _, input := range []string{"", "NaN 0", "-1 0", "bogus 0", "1000000000000 0"} {
		if _, err := parseUptime([]byte(input)); err == nil {
			t.Errorf("parseUptime(%q) should fail", input)
		}
	}
}

func TestUptimeCheckWarnsOnReadError(t *testing.T) {
	c := uptimeCheck{readFile: func(path string) ([]byte, error) {
		if path != "/proc/uptime" {
			t.Errorf("read unexpected path %q", path)
		}
		return nil, errors.New("missing")
	}}
	if result := c.Run(context.Background()); result.Status != check.StatusWarn {
		t.Fatalf("expected warning, got %+v", result)
	}
}
