package reporter

import (
	"encoding/json"
	"io"

	"github.com/marcospjr07/docktor-pt-br/internal/check"
)

// Keep the JSON contract separate from the check model so changes to internal
// field names do not change the format consumed by scripts.
type jsonReport struct {
	SchemaVersion int          `json:"schema_version"`
	Checks        []jsonResult `json:"checks"`
	Summary       jsonSummary  `json:"summary"`
}

type jsonResult struct {
	Name    string       `json:"name"`
	Status  check.Status `json:"status"`
	Message string       `json:"message"`
}

type jsonSummary struct {
	Pass  int `json:"pass"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Total int `json:"total"`
}

// WriteJSON writes one schema version 1 object followed by a newline. Checks
// retain their scan order; an empty report contains [] rather than null.
func WriteJSON(w io.Writer, report check.Report) error {
	checks := make([]jsonResult, len(report.Results))
	for i, result := range report.Results {
		checks[i] = jsonResult{Name: result.Name, Status: result.Status, Message: result.Message}
	}
	output := jsonReport{
		SchemaVersion: 1,
		Checks:        checks,
		Summary: jsonSummary{
			Pass:  report.Summary.Passed,
			Warn:  report.Summary.Warned,
			Fail:  report.Summary.Failed,
			Total: report.Summary.Total(),
		},
	}
	return json.NewEncoder(w).Encode(output)
}
