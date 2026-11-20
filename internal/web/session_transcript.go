package web

import (
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/prose"
)

type transcriptTurn struct {
	codex.SavedTurn
	Prose []prose.ProseBlock `json:"prose,omitempty"`
}

// Both read-only preview endpoints use the same inert Markdown projection as
// live session replies. Prompts and the fallback remain plain text. A
// restricted reply never leaves the server in either form.
type transcriptPage struct {
	Sessions        []codex.SavedSession `json:"sessions,omitempty"`
	Next            string               `json:"next,omitempty"`
	Agents          []codex.SessionAgent `json:"agents,omitempty"`
	Turns           []transcriptTurn     `json:"turns,omitempty"`
	UnverifiedTurns bool                 `json:"unverifiedTurns,omitempty"`
}

func (s *store) projectTranscript(turns []codex.SavedTurn) []transcriptTurn {
	if len(turns) == 0 {
		return nil
	}
	out := make([]transcriptTurn, 0, len(turns))
	for _, turn := range turns {
		turn.Task = codex.SanitizeSessionContext(turn.Task)
		if turn.Restricted {
			turn.Reply = ""
		} else {
			turn.Reply = codex.SanitizeSessionContext(turn.Reply)
		}
		out = append(out, transcriptTurn{SavedTurn: turn, Prose: s.proseCache.Get(turn.Reply)})
	}
	return out
}
