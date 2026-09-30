// Package check defines the contract shared by independent diagnostics.
package check

import "context"

// Status describes the health finding from a check.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Result is one check's finding. The runner supplies Name from Check.Name.
type Result struct {
	Name    string
	Status  Status
	Message string
}

// Check performs one diagnostic without modifying the host.
type Check interface {
	Name() string
	Run(context.Context) Result
}

// Summary counts findings by status.
type Summary struct {
	Passed int
	Warned int
	Failed int
}

func (s Summary) Total() int {
	return s.Passed + s.Warned + s.Failed
}

// Summarize counts the results from a scan. Unknown statuses count as warnings.
func Summarize(results []Result) Summary {
	var summary Summary
	for _, result := range results {
		switch result.Status {
		case StatusPass:
			summary.Passed++
		case StatusFail:
			summary.Failed++
		default:
			summary.Warned++
		}
	}
	return summary
}

// Report contains the ordered findings and their counts.
type Report struct {
	Results []Result
	Summary Summary
}

// Runner executes checks in registration order.
type Runner struct {
	checks []Check
}

func NewRunner(checks ...Check) Runner {
	return Runner{checks: append([]Check(nil), checks...)}
}

func (r Runner) Run(ctx context.Context) Report {
	results := make([]Result, 0, len(r.checks))
	for _, diagnostic := range r.checks {
		results = append(results, runOne(ctx, diagnostic))
	}
	return Report{Results: results, Summary: Summarize(results)}
}

func runOne(ctx context.Context, diagnostic Check) (result Result) {
	name := "Verificação"
	result = Result{Name: name, Status: StatusWarn, Message: "a verificação não pôde ser concluída"}
	defer func() {
		if recover() != nil {
			result = Result{Name: name, Status: StatusWarn, Message: "a verificação não pôde ser concluída"}
		}
	}()
	name = diagnostic.Name()
	result.Name = name
	if err := ctx.Err(); err != nil {
		result.Message = "varredura interrompida: " + contextErrorMessage(err)
		return result
	}
	result = diagnostic.Run(ctx)
	result.Name = name
	if result.Status != StatusPass && result.Status != StatusWarn && result.Status != StatusFail {
		result.Status = StatusWarn
		result.Message = "a verificação retornou um status inválido"
	}
	return result
}

func contextErrorMessage(err error) string {
	switch err {
	case context.Canceled:
		return "contexto cancelado"
	case context.DeadlineExceeded:
		return "prazo do contexto excedido"
	default:
		return err.Error()
	}
}
