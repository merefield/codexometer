//go:build unix

package codex

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSessionHistoryBoundedReadOnlyCompletedTurns(t *testing.T) {
	p, f := newQuotaDaemon(t)
	rows := []any{map[string]any{"id": "active", "status": "inProgress", "items": []any{map[string]any{"type": "agentMessage", "text": "Not finished"}}}}
	for i := 15; i >= 1; i-- {
		rows = append(rows, map[string]any{"id": fmt.Sprint(i), "status": "completed", "completedAt": int64(i), "items": []any{
			map[string]any{"type": "userMessage", "content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("Task %d", i)}}},
			map[string]any{"type": "agentMessage", "phase": "commentary", "text": "Planning"},
			map[string]any{"type": "agentMessage", "phase": "final_answer", "text": fmt.Sprintf("\x1b[31mAnswer %d\x00", i)},
			map[string]any{"type": "commandExecution", "command": "never run this"},
		}})
	}
	f.catalogues = map[string]map[string]any{"thread/turns/list": {"data": rows}}
	ctx := context.Background()
	turns, err := p.SessionHistory(ctx, "one")
	if err != nil || len(turns) != SessionHistoryLimit || turns[0].TurnID != "6" || turns[9].TurnID != "15" {
		t.Fatalf("history ordering/cap: %+v %v", turns, err)
	}
	for _, c := range turns {
		if c.CurrentTask != "Task "+c.TurnID || c.Text != "Answer "+c.TurnID || c.ApprovalToken != "" || c.InputToken != "" || strings.Contains(c.Text, "Planning") {
			t.Fatalf("wrong excerpt: %+v", c)
		}
	}
	f.mu.Lock()
	if len(f.writes) != 0 || len(f.historyReads) != 1 || f.historyReads[0]["limit"] != float64(SessionHistoryLimit+2) || f.historyReads[0]["itemsView"] != "summary" || f.historyReads[0]["sortDirection"] != "desc" || f.historyReads[0]["threadId"] != "one" {
		t.Errorf("unsafe/unbounded request: %+v", f.historyReads)
	}
	delete(f.catalogues, "thread/turns/list")
	f.mu.Unlock()
	if _, err = p.SessionHistory(ctx, "one"); err == nil {
		t.Fatal("unsupported history reported success")
	}
	if _, err = p.SessionHistory(ctx, ""); err == nil {
		t.Fatal("accepted empty session")
	}
	if _, err = (Client{}).SessionHistory(ctx, "one"); err == nil {
		t.Fatal("unavailable provider reported success")
	}
}
