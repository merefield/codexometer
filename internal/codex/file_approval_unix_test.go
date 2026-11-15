//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func missingFileApproval(t *testing.T, p *daemonStatusProvider, overrides map[string]any) {
	t.Helper()
	if err := p.ensureConnected(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"threadId": "one", "turnId": "turn", "itemId": "patch", "reason": "Review this change"}
	for k, v := range overrides {
		raw[k] = v
	}
	params, _ := json.Marshal(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contexts == nil {
		p.contexts = map[string]*daemonContextState{}
	}
	p.statuses = map[string]sessionRuntimeStatus{"one": sessionRuntimeApproval}
	daemonContextEvent(p.contexts, "turn/started", nil, json.RawMessage(`{"threadId":"one","turn":{"id":"turn"}}`), time.Now())
	daemonContextEvent(p.contexts, "item/fileChange/requestApproval", json.RawMessage(`"request"`), params, time.Now())
}

func recoveredFileItem() map[string]any {
	return map[string]any{"turnId": "turn", "item": map[string]any{
		"id": "patch", "type": "fileChange", "status": "inProgress", "changes": json.RawMessage(testFilePatch),
	}}
}

func TestFileApprovalFetchRecoversMissingDiff(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "zstd"}[compressed], func(t *testing.T) {
			p, f := newQuotaDaemonTransport(t, compressed)
			missingFileApproval(t, p, nil)
			f.mu.Lock()
			f.commandStatus, f.commandFlags = "active", []string{"waitingOnApproval"}
			f.catalogues = map[string]map[string]any{"thread/items/read": {"data": []any{recoveredFileItem()}}}
			f.mu.Unlock()
			snapshot, ok := p.Fetch(context.Background(), []string{"one"})
			c := snapshot.Contexts["one"]
			if !ok || c.ApprovalToken == "" || c.ApprovalBlocked != "" || c.FileChanges == "" || c.fileApprovalRecoverable {
				t.Fatal("complete pending patch was not recovered", ok, c)
			}
			if add, remove := FileDiffTotals(FileDiffLines(c.FileChanges)); add != 1 || remove != 1 {
				t.Fatal("recovered diff was not rendered completely", add, remove)
			}
			p.mu.Lock()
			p.contexts["one"].capturePatch("turn", "patch", json.RawMessage(strings.Replace(testFilePatch, "new", "changed again", 1)))
			p.mu.Unlock()
			p.refreshFileApprovals(context.Background(), []string{"one"})
			if p.SessionApprovalPending(c.ApprovalToken) {
				t.Fatal("recovered capability survived a subsequent patch change")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.itemReads) != 1 || f.itemReads[0]["threadId"] != "one" || f.itemReads[0]["turnId"] != "turn" {
				t.Fatal("recovery was not scoped to the pending item", f.itemReads)
			}
			ids := f.itemReads[0]["itemIds"].([]any)
			if len(ids) != 1 || ids[0] != "patch" || len(f.writes) != 0 {
				t.Fatal("recovery fetched another item or changed settings", f.itemReads)
			}
		})
	}
}

func TestFileApprovalRecoveryRejectsIncompleteOrUnrelatedItems(t *testing.T) {
	for _, variant := range []string{"missing", "multiple", "wrong turn", "wrong item", "wrong type", "completed", "deferred", "unknown field", "unsafe", "oversized"} {
		t.Run(variant, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			missingFileApproval(t, p, nil)
			entry := recoveredFileItem()
			item := entry["item"].(map[string]any)
			switch variant {
			case "wrong turn":
				entry["turnId"] = "old"
			case "wrong item":
				item["id"] = "other"
			case "wrong type":
				item["type"] = "commandExecution"
			case "completed":
				item["status"] = "completed"
			case "deferred":
				entry["outputDeferred"] = true
			case "unknown field":
				item["changes"] = json.RawMessage(strings.Replace(testFilePatch, `"diff":`, `"permissions":true,"diff":`, 1))
			case "unsafe":
				item["changes"] = json.RawMessage(strings.Replace(testFilePatch, "old", `\u001b[31mold`, 1))
			case "oversized":
				item["changes"] = json.RawMessage(strings.Replace(testFilePatch, "old", strings.Repeat("x", fileApprovalLimit), 1))
			}
			data := []any{entry}
			if variant == "missing" {
				data = nil
			} else if variant == "multiple" {
				data = append(data, recoveredFileItem())
			}
			f.mu.Lock()
			f.catalogues = map[string]map[string]any{"thread/items/read": {"data": data}}
			f.mu.Unlock()
			p.refreshFileApprovals(context.Background(), []string{"one"})
			p.mu.Lock()
			c := p.contexts["one"].requests[`"request"`]
			p.mu.Unlock()
			if c.ApprovalToken != "" || c.FileChanges != "" {
				t.Fatal("incomplete or unrelated item became actionable", c)
			}
		})
	}
}

func TestFileApprovalRecoveryAfterAttachWithoutTurnStart(t *testing.T) {
	p, f := newQuotaDaemon(t)
	missingFileApproval(t, p, nil)
	p.mu.Lock()
	p.contexts["one"].activeTurn = "" // Replayed pending request, no turn/started.
	p.mu.Unlock()
	f.mu.Lock()
	f.catalogues = map[string]map[string]any{"thread/items/read": {"data": []any{recoveredFileItem()}}}
	f.mu.Unlock()
	p.refreshFileApprovals(context.Background(), []string{"one"})
	p.mu.Lock()
	c := p.contexts["one"].requests[`"request"`]
	p.mu.Unlock()
	if c.ApprovalToken == "" || c.FileChanges == "" {
		t.Fatal("replayed approval could not recover its exact item", c)
	}
}

func TestFileApprovalRecoveryDoesNotReviveChangedRequests(t *testing.T) {
	for _, variant := range []string{"resolved", "turn ended", "replacement state", "replayed request", "disconnected", "new live patch"} {
		t.Run(variant, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			missingFileApproval(t, p, nil)
			f.mu.Lock()
			f.catalogues = map[string]map[string]any{"thread/items/read": {"data": []any{recoveredFileItem()}}}
			f.itemReadHook = func() {
				if variant == "disconnected" {
					p.disconnect(nil)
					return
				}
				p.mu.Lock()
				defer p.mu.Unlock()
				s := p.contexts["one"]
				switch variant {
				case "resolved":
					delete(s.requests, `"request"`)
				case "turn ended":
					s.ended = true
				case "replacement state":
					p.contexts["one"] = &daemonContextState{activeTurn: "next"}
				case "replayed request":
					c := s.requests[`"request"`]
					c.Text = "A new review of the request"
					s.requests[`"request"`] = c
				case "new live patch":
					s.capturePatch("turn", "patch", json.RawMessage(strings.Replace(testFilePatch, "new", "newest", 1)))
				}
			}
			f.mu.Unlock()
			p.refreshFileApprovals(context.Background(), []string{"one"})
			p.mu.Lock()
			var c SessionContext
			if s := p.contexts["one"]; s != nil {
				c = s.requests[`"request"`]
			}
			p.mu.Unlock()
			if variant == "new live patch" {
				if c.ApprovalToken == "" || !strings.Contains(c.FileChanges, "newest") || strings.Contains(c.FileChanges, `+new\n`) {
					t.Fatal("stale read replaced the newer live patch", c)
				}
			} else if c.ApprovalToken != "" || c.FileChanges != "" {
				t.Fatal("stale read revived an approval", c)
			}
		})
	}
}

func TestFileApprovalRecoveryCapsReadsPerRefresh(t *testing.T) {
	p, f := newQuotaDaemon(t)
	missingFileApproval(t, p, nil)
	p.mu.Lock()
	for i := range 5 {
		raw, _ := json.Marshal(map[string]any{"threadId": "one", "turnId": "turn", "itemId": string(rune('a' + i)), "reason": "Review this change"})
		id, _ := json.Marshal(i)
		daemonContextEvent(p.contexts, "item/fileChange/requestApproval", id, raw, time.Now())
	}
	p.mu.Unlock()
	p.refreshFileApprovals(context.Background(), []string{"one"})
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.itemReads) != 4 {
		t.Fatal("one refresh exceeded its bounded read budget", f.itemReads)
	}
}

func TestFileApprovalRecoveryDoesNotChargeUnstartedReads(t *testing.T) {
	p, f := newQuotaDaemon(t)
	missingFileApproval(t, p, nil)
	p.mu.Lock()
	s := p.contexts["one"]
	first := s.requests[`"request"`]
	first.RequestID = `"a-first"`
	later := first
	later.RequestID, later.ItemID = `"z-later"`, "later-patch"
	s.requests = map[string]SessionContext{first.RequestID: first, later.RequestID: later}
	p.mu.Unlock()
	for attempt := range 3 {
		ctx, cancel := context.WithCancel(context.Background())
		f.mu.Lock()
		f.itemNoReply = true
		f.itemReadHook = cancel // First RPC consumes the shared budget deterministically.
		f.mu.Unlock()
		p.refreshFileApprovals(ctx, []string{"one"})
		cancel()
		p.mu.Lock()
		started := s.requests[first.RequestID].fileApprovalFetches
		unstarted := s.requests[later.RequestID].fileApprovalFetches
		p.mu.Unlock()
		if int(started) != attempt+1 || unstarted != 0 {
			t.Fatal("shared cancellation charged a read that never started", started, unstarted)
		}
	}
	entry := recoveredFileItem()
	entry["item"].(map[string]any)["id"] = later.ItemID
	f.mu.Lock()
	f.itemNoReply, f.itemReadHook = false, nil
	f.catalogues = map[string]map[string]any{"thread/items/read": {"data": []any{entry}}}
	f.mu.Unlock()
	p.refreshFileApprovals(context.Background(), []string{"one"})
	p.mu.Lock()
	recovered := s.requests[later.RequestID]
	p.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if recovered.ApprovalToken == "" || recovered.fileApprovalFetches != 1 || len(f.itemReads) != 4 {
		t.Fatal("unstarted request lost eligibility after earlier reads stalled", recovered, f.itemReads)
	}
}

func TestFileApprovalRecoveryRetriesHistoryThatIsNotYetFlushed(t *testing.T) {
	p, f := newQuotaDaemon(t)
	missingFileApproval(t, p, nil)
	f.mu.Lock()
	f.catalogues = map[string]map[string]any{"thread/items/read": {"data": nil}}
	f.mu.Unlock()
	p.refreshFileApprovals(context.Background(), []string{"one"})
	f.mu.Lock()
	f.catalogues["thread/items/read"] = map[string]any{"data": []any{recoveredFileItem()}}
	f.mu.Unlock()
	p.refreshFileApprovals(context.Background(), []string{"one"})
	p.refreshFileApprovals(context.Background(), []string{"one"})
	p.mu.Lock()
	c := p.contexts["one"].requests[`"request"`]
	p.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ApprovalToken == "" || len(f.itemReads) != 2 {
		t.Fatal("transiently missing history failed recovery or kept polling", c, f.itemReads)
	}
}

func TestFileApprovalRecoveryIsBoundedAndKeepsUnsafeRequestsInCodex(t *testing.T) {
	p, f := newQuotaDaemon(t)
	missingFileApproval(t, p, nil)
	for range 8 {
		p.refreshFileApprovals(context.Background(), []string{"one"}) // unsupported endpoint
	}
	f.mu.Lock()
	reads := len(f.itemReads)
	f.mu.Unlock()
	if reads != 3 {
		t.Fatal("unsupported/missing history did not stop retrying", reads)
	}
	for _, overrides := range []map[string]any{{"grantRoot": "/work"}, {"reason": "hidden\x1b[31m"}, {"reason": strings.Repeat("x", approvalTextLimit+1)}} {
		missingFileApproval(t, p, overrides)
		p.refreshFileApprovals(context.Background(), []string{"one"})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.itemReads) != reads {
		t.Fatal("root grant or unsafe justification attempted recovery")
	}
}
