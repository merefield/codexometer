package codex

import (
	"fmt"
	"net/url"
	"strings"
)

const SessionGoalInputLimit = 4096

// SessionGoal is an observed, session-specific goal, not an instruction to this
// client. Only explicit, separately validated command actions can mutate it.
type SessionGoal struct {
	ThreadID        string `json:"threadId"`
	Objective       string `json:"objective"`
	Status          string `json:"status"`
	TokenBudget     *int64 `json:"tokenBudget"`
	TokensUsed      int64  `json:"tokensUsed"`
	TimeUsedSeconds int64  `json:"timeUsedSeconds"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	Stale           bool   `json:"stale,omitempty"`
}

func (g *SessionGoal) Clone() *SessionGoal {
	if g == nil {
		return nil
	}
	copy := *g
	if g.TokenBudget != nil {
		budget := *g.TokenBudget
		copy.TokenBudget = &budget
	}
	return &copy
}

func (g *SessionGoal) Label() string {
	if g == nil {
		return ""
	}
	if g.Stale {
		return "GOAL STALE"
	}
	switch g.Status {
	case "active":
		return "PURSUING GOAL"
	case "blocked":
		return "GOAL STALLED"
	case "paused":
		return "GOAL PAUSED"
	case "usageLimited":
		return "GOAL USAGE LIMITED"
	case "budgetLimited":
		return "GOAL BUDGET LIMITED"
	case "complete":
		return "GOAL COMPLETE"
	default:
		return "GOAL STATUS UNKNOWN"
	}
}

func (g *SessionGoal) Usage() string {
	if g == nil {
		return ""
	}
	if g.TokenBudget != nil {
		return fmt.Sprintf("%d / %d TOKENS", g.TokensUsed, *g.TokenBudget)
	}
	return fmt.Sprintf("%d TOKENS // %ds", g.TokensUsed, g.TimeUsedSeconds)
}

func knownGoalStatus(status string) bool {
	switch status {
	case "active", "paused", "blocked", "usageLimited", "budgetLimited", "complete":
		return true
	}
	return false
}

// Input command paths have an explicit allowlist shared by adapters and views.
// The value is URL-encoded as a single segment, including any literal slashes.
func SessionCommandInputValue(path string) (parent, value string, ok bool) {
	for _, prefix := range []string{"rename", "cd", "goal/set", "goal/edit", "goal/budget"} {
		if encoded, found := strings.CutPrefix(path, prefix+"/"); found {
			decoded, err := url.PathUnescape(encoded)
			return prefix, decoded, err == nil
		}
	}
	return "", "", false
}

func SessionCommandInputPath(path string) bool {
	switch path {
	case "rename", "cd", "goal/set", "goal/edit", "goal/budget":
		return true
	}
	return false
}
