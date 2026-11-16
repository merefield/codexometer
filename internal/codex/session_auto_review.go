package codex

import "strings"

// Display-only, comparable projection. The exact native retry event is retained
// separately by the daemon adapter and is never exposed to either front end.
type SessionAutoReview struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Risk          string `json:"risk,omitempty"`
	Authorization string `json:"authorization,omitempty"`
	Rationale     string `json:"rationale,omitempty"`
	Action        string `json:"action,omitempty"`
}

func (r SessionAutoReview) Active() bool { return r.Status == "inProgress" }

func (r SessionAutoReview) Detail() string {
	if r.ID == "" {
		return ""
	}
	parts := []string{"AUTO REVIEW // " + r.Status}
	if r.Risk != "" {
		parts = append(parts, "Risk: "+r.Risk)
	}
	if r.Authorization != "" {
		parts = append(parts, "User authorisation: "+r.Authorization)
	}
	if r.Action != "" {
		parts = append(parts, r.Action)
	}
	if r.Rationale != "" {
		parts = append(parts, r.Rationale)
	}
	return strings.Join(parts, "\n")
}
