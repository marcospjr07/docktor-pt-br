package reporter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

func TestWriteTerminal(t *testing.T) {
	results := []check.Result{
		{Name: "Sistema operacional", Status: check.StatusPass, Message: "Example Linux"},
		{Name: "Memória", Status: check.StatusWarn, Message: "80.0% em uso"},
		{Name: "Disco raiz", Status: check.StatusFail, Message: "95.0% em uso"},
	}
	var output bytes.Buffer
	if err := WriteTerminal(&output, check.Report{Results: results, Summary: check.Summarize(results)}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"✓ PASS Sistema operacional: Example Linux",
		"! WARN Memória: 80.0% em uso",
		"✗ FAIL Disco raiz: 95.0% em uso",
		"Resumo: 1 aprovado, 1 aviso, 1 falha",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}
