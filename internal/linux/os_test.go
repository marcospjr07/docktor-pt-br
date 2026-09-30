package linux

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "pretty name with escape",
			data: "# comment\nNAME=Fallback\nPRETTY_NAME=\"Example \\\"Linux\\\" 1\"\n",
			want: "Example \"Linux\" 1",
		},
		{
			name: "name and version ID",
			data: "NAME='Example Linux'\nVERSION_ID=2.3\n",
			want: "Example Linux 2.3",
		},
		{
			name: "name and version fallback",
			data: "NAME=Example\nVERSION='Rolling Release'\n",
			want: "Example Rolling Release",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOSRelease([]byte(test.data))
			if err != nil || got != test.want {
				t.Fatalf("parseOSRelease() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := parseOSRelease([]byte("ID=example\n")); err == nil {
		t.Fatal("expected missing name to return an error")
	}
}

func TestOSCheckFallsBackAndWarnsWhenFilesAreMissing(t *testing.T) {
	var paths []string
	c := osCheck{readFile: func(path string) ([]byte, error) {
		paths = append(paths, path)
		if path == "/etc/os-release" {
			return nil, errors.New("missing")
		}
		return []byte("PRETTY_NAME=Fallback\n"), nil
	}}
	result := c.Run(context.Background())
	if result.Status != check.StatusPass || result.Message != "Fallback" {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
	if len(paths) != 2 || paths[0] != "/etc/os-release" || paths[1] != "/usr/lib/os-release" {
		t.Fatalf("unexpected read paths: %v", paths)
	}

	c.readFile = func(string) ([]byte, error) { return nil, errors.New("missing") }
	result = c.Run(context.Background())
	if result.Status != check.StatusWarn || !strings.Contains(result.Message, "indisponível") {
		t.Fatalf("expected an unavailable warning, got %+v", result)
	}
}
