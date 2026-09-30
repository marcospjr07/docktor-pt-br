package linux

import "github.com/marcospjr07/docktor-pt-br/internal/check"

func statusForPercent(percent float64) check.Status {
	switch {
	case percent >= 95:
		return check.StatusFail
	case percent >= 80:
		return check.StatusWarn
	default:
		return check.StatusPass
	}
}
