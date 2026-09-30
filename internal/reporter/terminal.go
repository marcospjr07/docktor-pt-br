// Package reporter formats diagnostic reports for terminals and automation.
package reporter

import (
	"fmt"
	"io"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

// WriteTerminal writes findings and a summary without terminal control codes.
func WriteTerminal(w io.Writer, report check.Report) error {
	for _, result := range report.Results {
		symbol, label := statusDisplay(result.Status)
		if _, err := fmt.Fprintf(w, "%s %-4s %s: %s\n", symbol, label, result.Name, result.Message); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nResumo: %d %s, %d %s, %d %s\n",
		report.Summary.Passed, countLabel(report.Summary.Passed, "aprovado", "aprovados"),
		report.Summary.Warned, countLabel(report.Summary.Warned, "aviso", "avisos"),
		report.Summary.Failed, countLabel(report.Summary.Failed, "falha", "falhas"))
	return err
}

func statusDisplay(status check.Status) (string, string) {
	switch status {
	case check.StatusPass:
		return "✓", "PASS"
	case check.StatusWarn:
		return "!", "WARN"
	case check.StatusFail:
		return "✗", "FAIL"
	default:
		return "?", "DESCONHECIDO"
	}
}

func countLabel(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
