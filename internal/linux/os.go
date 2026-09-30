package linux

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

type osCheck struct {
	readFile readFileFunc
}

func (osCheck) Name() string { return "Sistema operacional" }

func (c osCheck) Run(ctx context.Context) check.Result {
	if result, interrupted := contextWarning(ctx); interrupted {
		return result
	}

	var failures []string
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := c.readFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		name, err := parseOSRelease(data)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		return check.Result{Status: check.StatusPass, Message: name}
	}
	return check.Result{Status: check.StatusWarn, Message: "identificação indisponível: " + strings.Join(failures, "; ")}
}

func parseOSRelease(data []byte) (string, error) {
	var prettyName, name, version, versionID string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch key {
		case "PRETTY_NAME", "NAME", "VERSION", "VERSION_ID":
		default:
			continue
		}
		value, err := parseOSReleaseValue(strings.TrimSpace(rawValue))
		if err != nil {
			continue
		}
		switch key {
		case "PRETTY_NAME":
			prettyName = value
		case "NAME":
			name = value
		case "VERSION":
			version = value
		case "VERSION_ID":
			versionID = value
		}
	}

	if prettyName != "" {
		return prettyName, nil
	}
	if name == "" {
		return "", errors.New("PRETTY_NAME e NAME ausentes")
	}
	if versionID != "" {
		return name + " " + versionID, nil
	}
	if version != "" {
		return name + " " + version, nil
	}
	return name, nil
}

func parseOSReleaseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	quote := raw[0]
	if quote != '\'' && quote != '"' {
		return raw, nil
	}
	if len(raw) < 2 || raw[len(raw)-1] != quote {
		return "", errors.New("valor com aspas não fechadas")
	}
	var value strings.Builder
	inner := raw[1 : len(raw)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			next := inner[i+1]
			if next == '\\' || next == quote || (quote == '"' && (next == '$' || next == '`')) {
				value.WriteByte(next)
				i++
				continue
			}
		}
		value.WriteByte(inner[i])
	}
	return value.String(), nil
}
