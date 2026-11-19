package codex

import (
	"encoding/json"
	"slices"
)

type answerItem struct {
	ID, Type, Text, Phase string
	Moderation            json.RawMessage
}

// The companion cannot implement native replacements, annotations or trusted
// contact checks. Only an explicit plain show (or no restriction) is renderable.
func restrictedAnswer(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var m struct {
		Moderated *bool
		Display   *struct {
			Version         int
			Mode            string
			TrustedContacts string
			Annotation      json.RawMessage
		}
	}
	if json.Unmarshal(raw, &m) != nil || m.Moderated == nil {
		return true
	}
	d := m.Display
	if d == nil {
		return *m.Moderated
	}
	return d.Version != 1 || d.Mode != "show" || d.TrustedContacts != "skip" ||
		(len(d.Annotation) != 0 && string(d.Annotation) != "null")
}

// Scoped to one thread. Keep identities, never message contents. Copy on
// append so a daemon snapshot can be read after releasing the provider lock.
type answerID struct{ turn, item string }
type answerGuards []answerID

func (g *answerGuards) note(turn, item string) {
	key := answerID{turn, item}
	if slices.Contains(*g, key) {
		return
	}
	const limit = 4 * SessionHistoryLimit
	start := max(0, len(*g)-limit+1)
	*g = append(slices.Clone((*g)[start:]), key)
}

func (g answerGuards) covers(turn, item string) bool {
	for _, k := range g {
		// Completion mirrors omit item IDs. Only use that fallback within
		// the same turn; a positive item identity also works without a turn.
		if k.turn == turn && (item == "" || k.item == "" || k.item == item) ||
			k.item != "" && k.item == item && (k.turn == "" || turn == "") {
			return true
		}
	}
	return false
}

func (g answerGuards) protect(c *SessionContext) {
	if c.Kind == SessionContextReply && g.covers(c.TurnID, c.ItemID) {
		c.withholdAnswer()
	}
}

func (c *rolloutCursor) inheritAnswerGuards(guards answerGuards) {
	for _, k := range guards {
		c.answers.note(k.turn, k.item)
	}
	c.answers.protect(&c.preview)
}

// SameHistoryTurn compares observations, not their displayed text. Legacy
// mirrors can lack a turn ID: use a shared item ID, or as a last resort
// the same nonzero observation time. Never match conflicting known IDs.
func (a SessionContext) SameHistoryTurn(b SessionContext) bool {
	if a.ThreadID != b.ThreadID {
		return false
	}
	if a.TurnID != "" && b.TurnID != "" {
		return a.TurnID == b.TurnID
	}
	if a.ItemID != "" && b.ItemID != "" {
		return a.ItemID == b.ItemID
	}
	return !a.At.IsZero() && a.At.Equal(b.At)
}

// ProtectReply updates an already-delivered excerpt when a matching decision
// arrives via another source. It never matches another thread or a command.
func (decision SessionContext) ProtectReply(c *SessionContext) {
	if decision.ReplyRestricted && c.Kind == SessionContextReply && decision.SameHistoryTurn(*c) &&
		(decision.ItemID == "" || c.ItemID == "" || decision.ItemID == c.ItemID) {
		c.withholdAnswer()
		if c.TurnID == "" {
			c.TurnID = decision.TurnID
		}
		if c.ItemID == "" {
			c.ItemID = decision.ItemID
		}
	}
}

func (c *SessionContext) withholdAnswer() {
	c.Text, c.ReplyRestricted, c.Streaming = "", true, false
	c.Activity = SessionActivity{}
}

// HasDisplayText includes restricted answers: observers carry only the flag;
// presenters supply the localised notice instead of assistant-authored text.
func (c SessionContext) HasDisplayText() bool { return c.ReplyRestricted || c.Text != "" }
