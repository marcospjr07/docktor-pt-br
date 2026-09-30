package reporter_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
	"github.com/marcospjr07/docktor-pt-br/internal/reporter"
)

func TestWriteJSONContract(t *testing.T) {
	results := []check.Result{
		{Name: "OS", Status: check.StatusPass, Message: "Example Linux"},
		{Name: "Memory", Status: check.StatusWarn, Message: "80% used"},
		{Name: "Root disk", Status: check.StatusFail, Message: ""},
	}
	var output bytes.Buffer
	if err := reporter.WriteJSON(&output, check.Report{Results: results, Summary: check.Summarize(results)}); err != nil {
		t.Fatal(err)
	}
	assertJSONContract(t, output.Bytes(), `{
		"schema_version": 1,
		"checks": [
			{"name": "OS", "status": "pass", "message": "Example Linux"},
			{"name": "Memory", "status": "warn", "message": "80% used"},
			{"name": "Root disk", "status": "fail", "message": ""}
		],
		"summary": {"pass": 1, "warn": 1, "fail": 1, "total": 3}
	}`)
}

func TestWriteJSONEmptyReports(t *testing.T) {
	for _, results := range [][]check.Result{nil, {}} {
		var output bytes.Buffer
		if err := reporter.WriteJSON(&output, check.Report{Results: results}); err != nil {
			t.Fatal(err)
		}
		assertJSONContract(t, output.Bytes(), `{
			"schema_version": 1,
			"checks": [],
			"summary": {"pass": 0, "warn": 0, "fail": 0, "total": 0}
		}`)
	}
}

func TestWriteJSONPreservesMessagesWithSpecialCharacters(t *testing.T) {
	message := "ação 🩺: \"quoted\" \\path\nnew line\ttab\rreturn\x1bescape <>&"
	results := []check.Result{{Name: "SSH", Status: check.StatusWarn, Message: message}}
	var output bytes.Buffer
	if err := reporter.WriteJSON(&output, check.Report{Results: results, Summary: check.Summarize(results)}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Checks []struct {
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v; output: %q", err, output.String())
	}
	if len(decoded.Checks) != 1 || decoded.Checks[0].Message != message {
		t.Fatalf("message did not round trip: %#v", decoded.Checks)
	}
}

func TestWriteJSONPropagatesWriteError(t *testing.T) {
	want := errors.New("output unavailable")
	if err := reporter.WriteJSON(jsonFailingWriter{err: want}, check.Report{}); !errors.Is(err, want) {
		t.Fatalf("write error = %v; want %v", err, want)
	}
}

type jsonFailingWriter struct{ err error }

func (w jsonFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func assertJSONContract(t *testing.T, output []byte, expected string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("output is not a single JSON document: %v; output: %q", err, output)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON contract mismatch:\ngot:  %s\nwant: %s", output, expected)
	}
	if len(output) == 0 || output[len(output)-1] != '\n' {
		t.Fatalf("JSON report has no final newline: %q", output)
	}
}
