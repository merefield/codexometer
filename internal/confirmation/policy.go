// Package confirmation owns launch-scoped UI confirmation policy. Skipping a
// second click never authorises an action or bypasses backend validation.
package confirmation

type Action uint8

const (
	Guarded Action = iota // Unknown actions fail closed by default.
	Reversible
	QuotaReset
)

type Policy struct {
	NoConfirm bool
}

func (p Policy) Required(action Action) bool {
	return action == QuotaReset || !p.NoConfirm && action != Reversible
}

func Command(reversible bool) Action {
	if reversible {
		return Reversible
	}
	return Guarded
}
