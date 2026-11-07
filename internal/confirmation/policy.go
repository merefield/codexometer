// Package confirmation owns launch-scoped UI confirmation policy. Skipping a
// second click never authorises an action or bypasses backend validation.
package confirmation

import "fmt"

type Mode string

const (
	Balanced Mode = "balanced"
	None     Mode = "none"
	Require  Mode = "require"
)

func Parse(value string) (Mode, error) {
	switch Mode(value) {
	case Balanced, None, Require:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("--confirmation must be balanced, none or require")
	}
}

type Action uint8

const (
	Guarded Action = iota // Unknown actions remain guarded by default.
	Narrow
	QuotaReset
)

type Policy struct{ Mode Mode }

func (p Policy) Required(action Action) bool {
	if action == QuotaReset {
		return true
	}
	switch p.Mode {
	case None:
		return false
	case Require:
		return true
	default:
		return action != Narrow
	}
}

func Command(reversible bool) Action {
	if reversible {
		return Narrow
	}
	return Guarded
}

func Approval(kind string) Action {
	switch kind {
	case "accept", "decline", "cancel", "cancelMcpTool":
		return Narrow
	default:
		return Guarded
	}
}
