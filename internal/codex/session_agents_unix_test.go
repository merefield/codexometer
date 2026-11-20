//go:build unix

package codex

import (
	"testing"
	"time"
)

func TestSessionAgentsOnlyReadVerifiedDescendantHistory(t *testing.T) {
	p, f := newQuotaDaemon(t)
	now := time.Now().Truncate(time.Second)
	answer := func(id string, started time.Time) map[string]any {
		return map[string]any{"id": id, "status": "completed", "startedAt": started.Unix(), "items": []any{map[string]any{"type": "agentMessage", "phase": "final_answer", "text": id}}}
	}
	f.catalogues = map[string]map[string]any{"thread/turns/list": {"data": []any{answer("child answer", now.Add(time.Second)), answer("parent prefix", now.Add(-time.Minute))}}}
	if err := p.ensureConnected(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := &LiveUsageReader{statusProvider: p, files: map[string]*rolloutCursor{
		"root":     {threadID: "root", startedAt: now},
		"child":    {threadID: "child", parentThreadID: "root", nonRoot: true, startedAt: now, agentName: "Worker"},
		"nested":   {threadID: "nested", parentThreadID: "child", nonRoot: true, startedAt: now, agentName: "Nested"},
		"other":    {threadID: "other", startedAt: now},
		"unknown":  {threadID: "unknown", parentThreadID: "missing", nonRoot: true, startedAt: now},
		"guardian": {threadID: "guardian", parentThreadID: "root", nonRoot: true, startedAt: now, guardianReviewer: true},
	}}
	for _, cursor := range r.files {
		cursor.lastModified = now
	}
	c := Client{LiveUsage: r}
	page, err := c.InspectSessionAgents(t.Context(), "root", "")
	if err != nil || len(page.Agents) != 2 || page.Agents[0].ID != "nested" || page.Agents[1].Name != "Worker" {
		t.Fatal(page, err)
	}
	for _, id := range []string{"root", "other", "unknown", "guardian"} {
		if _, err := c.InspectSessionAgents(t.Context(), "root", id); err == nil {
			t.Fatalf("inspected unverified child %q", id)
		}
	}
	page, err = c.InspectSessionAgents(t.Context(), "root", "child")
	if err != nil || len(page.Turns) != 1 || page.Turns[0].Reply != "child answer" {
		t.Fatal(page, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.historyReads) != 1 || f.historyReads[0]["threadId"] != "child" || len(f.resumeReads)+len(f.readUpdates)+len(f.writes) != 0 {
		t.Fatal("unexpected agent mutation or history read", f.historyReads)
	}
}
