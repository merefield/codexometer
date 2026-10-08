//go:build unix

package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gorilla/websocket"
)

type queueCapability struct {
	connection *websocket.Conn
	thread     string
	item       SessionQueuedMessage
	input      string
}

func (p *daemonStatusProvider) SessionQueueRevision() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queueRevision
}

func (p *daemonStatusProvider) SessionQueue(ctx context.Context, thread string) ([]SessionQueuedMessage, error) {
	p.mu.Lock()
	connection := p.connection
	_, subscribed := p.subscribed[thread]
	p.mu.Unlock()
	if connection == nil || !subscribed {
		return nil, errors.New("session unavailable")
	}
	var out []SessionQueuedMessage
	capabilities := map[string]queueCapability{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 16; page++ {
		var result struct {
			Data []struct {
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}
		params := map[string]any{"threadId": thread, "limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := p.requestOn(ctx, connection, "thread/queue/list", params, &result); err != nil {
			return nil, err
		}
		for _, raw := range result.Data {
			if raw.ID == "" || seen[raw.ID] {
				return nil, errors.New("inconsistent queue")
			}
			seen[raw.ID] = true
			var inputs []struct {
				Type     string            `json:"type"`
				Text     string            `json:"text"`
				Elements []json.RawMessage `json:"text_elements"`
			}
			if err := json.Unmarshal(raw.Input, &inputs); err != nil {
				return nil, err
			}
			item := SessionQueuedMessage{ID: raw.ID, Token: rand.Text()}
			var text []string
			for _, input := range inputs {
				if input.Type == "text" {
					text = append(text, input.Text)
				} else {
					text = append(text, "["+input.Type+"]")
				}
			}
			item.Text = SanitizeSessionContext(strings.Join(text, "\n"))
			item.Editable = len(inputs) == 1 && inputs[0].Type == "text" && len(inputs[0].Elements) == 0 && inputs[0].Text == item.Text && validPromptAnswers(SessionPromptOffer{}, []string{item.Text})
			// Future input metadata must not be silently discarded by our text editor.
			var fields []map[string]json.RawMessage
			if json.Unmarshal(raw.Input, &fields) == nil && len(fields) == 1 {
				for key := range fields[0] {
					if key != "type" && key != "text" && key != "text_elements" {
						item.Editable = false
					}
				}
			}
			capabilities[item.Token] = queueCapability{connection, thread, item, string(raw.Input)}
			out = append(out, item)
		}
		if result.Next == "" {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.connection != connection {
				return nil, errors.New("session connection changed")
			}
			// Only the selected session is retained; stale edits fail closed.
			p.queueCapabilities = capabilities
			return out, nil
		}
		if result.Next == cursor {
			return nil, errors.New("queue pagination stalled")
		}
		cursor = result.Next
	}
	return nil, errors.New("queue exceeds display limit")
}

func (p *daemonStatusProvider) ChangeSessionQueue(ctx context.Context, thread string, item SessionQueuedMessage, text string, remove bool) error {
	if !remove && IsSessionCommand(text) {
		return errors.New("slash commands cannot be queued; use the command menu")
	}
	text = literalSessionText(text)
	p.mu.Lock()
	cap, ok := p.queueCapabilities[item.Token]
	if !ok || cap.connection != p.connection || cap.thread != thread || cap.item.ID != item.ID || (!remove && !cap.item.Editable) {
		p.mu.Unlock()
		return errors.New("queue changed; reload before editing")
	}
	delete(p.queueCapabilities, item.Token)
	p.mu.Unlock()
	if !remove && !validPromptAnswers(SessionPromptOffer{}, []string{text}) {
		return errors.New("invalid queued message")
	}
	// Re-read the original item, never add a replacement if it was dispatched.
	current, err := p.SessionQueue(ctx, thread)
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range current {
		if candidate.ID == item.ID {
			p.mu.Lock()
			latest := p.queueCapabilities[candidate.Token]
			p.mu.Unlock()
			found = latest.connection == cap.connection && latest.input == cap.input
		}
	}
	if !found {
		return errors.New("queued message changed or already started")
	}
	params := map[string]any{"threadId": thread, "queuedSubmissionId": item.ID}
	method := "thread/queue/delete"
	if !remove {
		method = "thread/queue/update"
		params["input"] = []any{map[string]any{"type": "text", "text": text}}
	}
	var result struct {
		Deleted bool `json:"deleted"`
		Item    struct {
			ID string `json:"id"`
		} `json:"queuedSubmission"`
	}
	if err := p.requestOn(ctx, cap.connection, method, params, &result); err != nil {
		return err
	}
	if remove && !result.Deleted || !remove && result.Item.ID != item.ID {
		return errors.New("queue change not confirmed")
	}
	return nil
}
