package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// SessionPromptOffer is connection-local. Neither drafts nor capabilities are persisted.
type SessionPromptOffer struct {
	Token     string
	ThreadID  string
	Questions []PromptQuestion
}

type PromptQuestion struct {
	ID       string
	Text     string
	Secret   bool
	FreeText bool
	Options  []string
}

type SessionPromptClient interface {
	SessionPrompt(string) SessionPromptOffer
	SendSessionPrompt(context.Context, string, []string) error
}

func (c Client) SessionPrompt(thread string) SessionPromptOffer {
	if c.LiveUsage != nil {
		if p, ok := c.LiveUsage.statusProvider.(SessionPromptClient); ok {
			return p.SessionPrompt(thread)
		}
	}
	return SessionPromptOffer{}
}

func (c Client) SendSessionPrompt(ctx context.Context, token string, answers []string) error {
	if c.LiveUsage != nil {
		if p, ok := c.LiveUsage.statusProvider.(SessionPromptClient); ok {
			return p.SendSessionPrompt(ctx, token, answers)
		}
	}
	return errors.New("prompt unavailable; reply in Codex")
}

func validPromptQuestions(qs []contextQuestion) bool {
	if len(qs) < 1 || len(qs) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if q.ID == "" || seen[q.ID] || q.Question == "" || len(q.Options) > 16 {
			return false
		}
		seen[q.ID] = true
	}
	return true
}

func inputOffer(c SessionContext) SessionPromptOffer {
	var qs []contextQuestion
	if c.InputToken == "" || json.Unmarshal([]byte(c.InputQuestions), &qs) != nil {
		return SessionPromptOffer{}
	}
	o := SessionPromptOffer{Token: c.InputToken, ThreadID: c.ThreadID}
	for _, q := range qs {
		p := PromptQuestion{ID: q.ID, Text: q.Question, Secret: q.IsSecret, FreeText: q.IsOther || len(q.Options) == 0}
		for _, option := range q.Options {
			p.Options = append(p.Options, option.Label)
		}
		o.Questions = append(o.Questions, p)
	}
	return o
}

func validPromptAnswers(o SessionPromptOffer, answers []string) bool {
	want := max(len(o.Questions), 1)
	if len(answers) != want {
		return false
	}
	for i, a := range answers {
		if strings.TrimSpace(a) == "" || len([]rune(a)) > sessionContextLimit || SanitizeSessionContext(a) != strings.TrimSpace(a) {
			return false
		}
		if len(o.Questions) > 0 && !o.Questions[i].FreeText {
			found := false
			for _, option := range o.Questions[i].Options {
				if a == option {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
