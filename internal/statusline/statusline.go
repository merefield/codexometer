// Package statusline defines Codexometer's display-only session footer fields.
// Codex CLI's private picker enum is not an app-server capability catalogue.
package statusline

import (
	"fmt"
	"strings"

	"github.com/merefield/codexometer/internal/codex"
)

type Field struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Help    string `json:"help"`
	Default bool   `json:"default"`
}

func Fields() []Field {
	return []Field{
		{"model-with-reasoning", "Model and reasoning", "Observed model and reasoning level for this session.", true},
		{"fast-mode", "Speed", "Observed service tier, when available.", true},
		{"used-tokens", "Tokens", "Total observed tokens for this session, including linked agents.", true},
		{"current-dir", "Directory", "Working directory for this session.", true},
		{"model", "Model", "Observed model name without reasoning.", false},
		{"reasoning", "Reasoning", "Observed reasoning level.", false},
		{"thread-name", "Session name", "Session name, when available.", false},
		{"thread-id", "Session ID", "Full session identifier.", false},
		{"run-state", "State", "Observed state, including stale or attention signals.", false},
		{"agents", "Agents", "Number of linked agents, when reported.", false},
		{"source", "Source", "Source of the observed session context.", false},
	}
}

// Nil means defaults; an explicit empty selection hides the footer.
func Normalize(ids []string) []string {
	if ids == nil {
		ids = []string{}
		for _, f := range Fields() {
			if f.Default {
				ids = append(ids, f.ID)
			}
		}
	}
	valid := map[string]bool{}
	for _, f := range Fields() {
		valid[f.ID] = true
	}
	out := make([]string, 0, len(valid))
	seen := map[string]bool{}
	for _, id := range ids {
		if valid[id] && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}

type Data struct {
	Model, Effort, Speed, Directory, Name, ID, State, Source string
	Tokens                                                   int64
	Agents                                                   int
}

func Values(d Data) map[string]string {
	combined := strings.TrimSpace(d.Model + " " + d.Effort)
	values := map[string]string{"model-with-reasoning": combined, "model": d.Model, "reasoning": d.Effort, "fast-mode": d.Speed, "current-dir": d.Directory, "thread-name": d.Name, "thread-id": d.ID, "run-state": d.State, "source": d.Source}
	if d.Tokens >= 0 {
		values["used-tokens"] = fmt.Sprintf("%s tokens", count(d.Tokens))
	}
	if d.Agents > 0 {
		values["agents"] = fmt.Sprintf("%d agents", d.Agents)
	}
	for k, v := range values {
		values[k] = strings.Join(strings.Fields(codex.SanitizeSessionContext(v)), " ")
	}
	return values
}

func Text(ids []string, values map[string]string) string {
	var parts []string
	for _, id := range Normalize(ids) {
		if v := strings.Join(strings.Fields(codex.SanitizeSessionContext(values[id])), " "); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

func count(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
