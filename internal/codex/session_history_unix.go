//go:build unix

package codex

import (
	"context"
	"strings"
	"time"
)

type historyTurn struct {
	ID, Status  string
	CompletedAt *int64
	Items       []struct {
		ID, Type, Text, Phase string
		Content               []struct{ Type, Text string }
	}
}

func (p *daemonStatusProvider) SessionHistory(ctx context.Context, id string) ([]SessionContext, error) {
	if id == "" || len(id) > 512 {
		return nil, ErrSessionHistory
	}
	if err := p.ensureConnected(ctx); err != nil {
		return nil, err
	}
	// Bounded, newest-first history reads neither resume nor change a session.
	// Do not fall back to an unbounded includeTurns read on older servers.
	var response struct{ Data []historyTurn }
	if err := p.request(ctx, "thread/turns/list", map[string]any{
		"threadId": id, "limit": SessionHistoryLimit + 2, "sortDirection": "desc", "itemsView": "summary",
	}, &response); err != nil {
		return nil, err
	}
	var result []SessionContext
	for _, turn := range response.Data {
		if turn.Status != "completed" || turn.ID == "" {
			continue
		}
		c := SessionContext{ThreadID: id, TurnID: turn.ID, Kind: SessionContextReply, Source: "HISTORY"}
		if turn.CompletedAt != nil {
			c.At = time.Unix(*turn.CompletedAt, 0)
		}
		for _, item := range turn.Items {
			switch item.Type {
			case "userMessage":
				var text []string
				for _, content := range item.Content {
					if content.Type == "text" {
						text = append(text, content.Text)
					}
				}
				if c.CurrentTask == "" {
					c.CurrentTask = strings.Join(text, "\n")
				} else {
					c.LatestGuidance = strings.Join(text, "\n")
				}
			case "agentMessage":
				if item.Phase == "final_answer" || item.Phase == "" {
					c.Text, c.ItemID = item.Text, item.ID
				}
			}
		}
		c = HistoryExcerpt(c)
		if c.Text != "" {
			result = append(result, c)
		}
		if len(result) == SessionHistoryLimit {
			break
		}
	}
	// UI caches use oldest-first order, independent of completion timestamps.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}
