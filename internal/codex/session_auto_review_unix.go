//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/gorilla/websocket"
)

const autoReviewLimit = 10

type nativeAutoReview struct {
	turn    string
	view    SessionAutoReview
	started int64
	event   json.RawMessage
	details string
}
type autoReviewState struct {
	latest          nativeAutoReview
	denials         []nativeAutoReview
	running         map[string]nativeAutoReview
	finished        map[string]int64
	finishedThrough int64
	// IDs are not reusable after ambiguous writes. Bounded by the denial
	// inventory; replayed notifications must not recreate spent capabilities.
	consumed        map[string]int64
	consumedThrough int64
}

func (s *autoReviewState) display() SessionAutoReview {
	r := s.latest
	for _, active := range s.running {
		if !r.view.Active() || active.started > r.started || active.started == r.started && active.view.ID < r.view.ID {
			r = active
		}
	}
	return r.view
}

// The timestamp watermark keeps evicted consent IDs spent without retaining
// an unbounded set of historical requests. Ties are conservatively spent too.
func pruneReviewIDs(ids map[string]int64) int64 {
	if len(ids) <= 128 {
		return 0
	}
	oldest := ""
	var at int64
	for id, stamp := range ids {
		if oldest == "" || stamp < at || stamp == at && id < oldest {
			oldest, at = id, stamp
		}
	}
	delete(ids, oldest)
	return at
}

func (p *daemonStatusProvider) autoReviewNotificationLocked(method string, data json.RawMessage) {
	if method == "thread/closed" {
		var e struct{ ThreadID string }
		if json.Unmarshal(data, &e) == nil {
			delete(p.autoReviews, e.ThreadID)
			delete(p.permissionSettings, e.ThreadID)
		}
		return
	}
	if method != "item/autoApprovalReview/started" && method != "item/autoApprovalReview/completed" {
		if method == "turn/started" || method == "turn/completed" || method == "turn/interrupted" {
			var e struct {
				ThreadID, TurnID string
				Turn             struct{ ID string }
			}
			if json.Unmarshal(data, &e) == nil {
				if e.TurnID == "" {
					e.TurnID = e.Turn.ID
				}
				if s := p.autoReviews[e.ThreadID]; s != nil && s.latest.view.Active() {
					if s.latest.turn == e.TurnID && method != "turn/started" || s.latest.turn != e.TurnID && method == "turn/started" {
						s.latest.view.Status = "unconfirmed"
					}
				}
				if s := p.autoReviews[e.ThreadID]; s != nil {
					for id, r := range s.running {
						if r.turn == e.TurnID && method != "turn/started" || r.turn != e.TurnID && method == "turn/started" {
							delete(s.running, id)
						}
					}
				}
			}
		}
		return
	}
	var n struct {
		ThreadID, TurnID, ReviewID string
		StartedAtMs, CompletedAtMs int64
		DecisionSource             string
		Review                     struct{ Status, RiskLevel, UserAuthorization, Rationale string }
		Action                     json.RawMessage
	}
	if len(data) > 128*1024 || json.Unmarshal(data, &n) != nil || !commandSafe(n.ThreadID) || !commandSafe(n.TurnID) || !commandSafe(n.ReviewID) || len(n.ReviewID) > 512 || n.StartedAtMs <= 0 {
		return
	}
	status := map[string]string{"inProgress": "in_progress", "approved": "approved", "denied": "denied", "timedOut": "timed_out", "aborted": "aborted"}[n.Review.Status]
	if status == "" || method == "item/autoApprovalReview/started" && status != "in_progress" {
		return
	}
	if method == "item/autoApprovalReview/completed" && (status == "in_progress" || n.CompletedAtMs < n.StartedAtMs || n.DecisionSource != "agent") {
		return
	}
	r := nativeAutoReview{turn: n.TurnID, started: n.StartedAtMs, view: SessionAutoReview{ID: n.ReviewID, Status: n.Review.Status, Risk: commandLabel(n.Review.RiskLevel), Authorization: commandLabel(n.Review.UserAuthorization), Rationale: commandText(n.Review.Rationale), Action: commandText(string(n.Action))}}
	action, err := nativeReviewAction(n.Action)
	if err == nil {
		event := map[string]any{"id": n.ReviewID, "turn_id": n.TurnID, "started_at_ms": n.StartedAtMs, "status": status, "action": action}
		if n.CompletedAtMs != 0 {
			event["completed_at_ms"] = n.CompletedAtMs
			event["decision_source"] = "agent"
		}
		if n.Review.RiskLevel != "" {
			event["risk_level"] = n.Review.RiskLevel
		}
		if n.Review.UserAuthorization != "" {
			event["user_authorization"] = n.Review.UserAuthorization
		}
		if n.Review.Rationale != "" {
			event["rationale"] = n.Review.Rationale
		}
		// Full authority is not reconstructed from a clipped/sanitised display.
		full := r.view
		full.Rationale, full.Action = n.Review.Rationale, string(n.Action)
		details := full.Detail()
		if approvalTextFits(details) && strings.TrimSpace(SanitizeApprovalText(details)) == strings.TrimSpace(details) {
			r.details = details
			r.event, _ = json.Marshal(event)
		}
	}
	if p.autoReviews == nil {
		p.autoReviews = map[string]*autoReviewState{}
	}
	s := p.autoReviews[n.ThreadID]
	if s == nil {
		// Never evict a live connection's spent-consent ledger to admit another
		// thread. Like context tracking, stop admission at a fixed global cap.
		if len(p.autoReviews) >= 256 {
			return
		}
		s = &autoReviewState{consumed: map[string]int64{}, running: map[string]nativeAutoReview{}, finished: map[string]int64{}}
		p.autoReviews[n.ThreadID] = s
	}
	if r.view.Active() {
		if _, ended := s.finished[r.view.ID]; ended || s.consumed[r.view.ID] != 0 || r.started <= s.consumedThrough || r.started <= s.finishedThrough {
			return
		}
		if state := p.contexts[n.ThreadID]; state != nil && (state.ended || state.activeTurn != "" && state.activeTurn != n.TurnID) {
			return
		}
		if len(s.running) < 32 {
			s.running[r.view.ID] = r
		}
	} else {
		delete(s.running, r.view.ID)
		s.finished[r.view.ID] = r.started
		s.finishedThrough = max(s.finishedThrough, pruneReviewIDs(s.finished))
	}
	if r.started >= s.latest.started && !(s.latest.view.ID == r.view.ID && !s.latest.view.Active() && r.view.Active()) {
		s.latest = r
	}
	if status == "denied" && len(r.event) > 0 && s.consumed[r.view.ID] == 0 && r.started > s.consumedThrough {
		for _, old := range s.denials {
			if old.view.ID == r.view.ID {
				return
			}
		}
		s.denials = append(s.denials, r)
		sort.SliceStable(s.denials, func(i, j int) bool { return s.denials[i].started > s.denials[j].started })
		if len(s.denials) > autoReviewLimit {
			s.denials = s.denials[:autoReviewLimit]
		}
		p.commandGeneration++
	}
}

func (p *daemonStatusProvider) autoReviewMenu(t commandThread, path string) (SessionCommandMenu, error) {
	m := SessionCommandMenu{Title: "/approve", Help: "Choose a recent native Auto-review denial. Explicit consent records one retry of that exact action; it does not run it directly. The retry still goes through Auto-review and cannot bypass mandatory human approval or a critical-risk denial.", Choices: []SessionCommandChoice{}}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.autoReviews[t.ID]
	if s == nil || len(s.denials) == 0 {
		m.Choices = append(m.Choices, SessionCommandChoice{Label: "No recent Auto-review denials", Help: "Only denials observed on this shared app-server connection are available."})
		return m, nil
	}
	for _, r := range s.denials {
		next := "approve/" + url.PathEscape(r.view.ID)
		if path == "approve" {
			m.Choices = append(m.Choices, SessionCommandChoice{Label: "Denied // " + r.view.ID, Help: r.details, Next: next})
		} else if path == next {
			m.Choices = append(m.Choices, SessionCommandChoice{Label: "Record approval for one retry", Help: r.details, method: "thread/approveGuardianDeniedAction", params: map[string]any{"threadId": t.ID, "event": r.event}})
		}
	}
	m.Revision = commandDigest(m.Choices)
	return m, nil
}

func (p *daemonStatusProvider) approveAutoReview(ctx context.Context, conn *websocket.Conn, id string, o SessionCommandChoice) error {
	p.mu.Lock()
	s := p.autoReviews[id]
	valid := false
	if p.connection == conn && s != nil && !p.commandHumanWaitLocked(id) {
		for i, r := range s.denials {
			if r.started > s.consumedThrough && commandDigest(r.event) == commandDigest(o.params["event"]) {
				// Reserve before the RPC. Lost acknowledgements never trigger a retry.
				s.consumed[r.view.ID] = r.started
				s.consumedThrough = max(s.consumedThrough, pruneReviewIDs(s.consumed))
				s.denials = append(s.denials[:i], s.denials[i+1:]...)
				s.denials = slices.DeleteFunc(s.denials, func(r nativeAutoReview) bool { return r.started <= s.consumedThrough })
				valid = true
				p.commandGeneration++
				break
			}
		}
	}
	p.mu.Unlock()
	if !valid {
		return ErrSessionCommand
	}
	if err := p.requestOn(ctx, conn, o.method, o.params, nil); err != nil {
		return fmt.Errorf("retry approval unconfirmed; check Codex before trying again: %w", err)
	}
	return nil
}

// Convert the public app-server action to the core event expected by
// thread/approveGuardianDeniedAction, matching the native CLI's conversion.
// Unknown action fields fail closed, never silently weaken the retry identity.
func nativeReviewAction(data json.RawMessage) (map[string]any, error) {
	var a map[string]any
	if json.Unmarshal(data, &a) != nil {
		return nil, ErrSessionCommand
	}
	kind, _ := a["type"].(string)
	fields := map[string]string{
		"command": "source command cwd", "execve": "source program argv cwd",
		"writeStdin": "approvalId processId stdin cwd", "applyPatch": "cwd files",
		"networkAccess": "target host protocol port", "mcpToolCall": "server toolName connectorId connectorName toolTitle",
		"requestPermissions": "reason permissions",
	}[kind]
	if fields == "" {
		return nil, ErrSessionCommand
	}
	optional := map[string]bool{"reason": true, "connectorId": true, "connectorName": true, "toolTitle": true}
	for _, key := range strings.Fields(fields) {
		v, exists := a[key]
		if !exists || v == nil {
			if optional[key] {
				continue
			}
			return nil, ErrSessionCommand
		}
		switch key {
		case "argv", "files":
			list, ok := v.([]any)
			if !ok {
				return nil, ErrSessionCommand
			}
			for _, item := range list {
				if _, ok := item.(string); !ok {
					return nil, ErrSessionCommand
				}
			}
		case "permissions":
			if _, ok := v.(map[string]any); !ok {
				return nil, ErrSessionCommand
			}
		case "port":
			if n, ok := v.(float64); !ok || n < 0 || n > 65535 || n != float64(int(n)) {
				return nil, ErrSessionCommand
			}
		default:
			if _, ok := v.(string); !ok {
				return nil, ErrSessionCommand
			}
		}
	}
	for key := range a {
		if key != "type" && !strings.Contains(" "+fields+" ", " "+key+" ") {
			return nil, ErrSessionCommand
		}
	}
	names := map[string]string{"writeStdin": "write_stdin", "applyPatch": "apply_patch", "networkAccess": "network_access", "mcpToolCall": "mcp_tool_call", "requestPermissions": "request_permissions"}
	if name := names[kind]; name != "" {
		a["type"] = name
	}
	for _, pair := range [][2]string{{"approvalId", "approval_id"}, {"processId", "process_id"}, {"toolName", "tool_name"}, {"connectorId", "connector_id"}, {"connectorName", "connector_name"}, {"toolTitle", "tool_title"}} {
		if v, ok := a[pair[0]]; ok {
			a[pair[1]] = v
			delete(a, pair[0])
		}
	}
	if source, ok := a["source"]; ok {
		if source == "unifiedExec" {
			a["source"] = "unified_exec"
		} else if source != "shell" {
			return nil, ErrSessionCommand
		}
	}
	if protocol, ok := a["protocol"]; ok {
		switch protocol {
		case "socks5Tcp":
			a["protocol"] = "socks5_tcp"
		case "socks5Udp":
			a["protocol"] = "socks5_udp"
		case "http", "https":
		default:
			return nil, ErrSessionCommand
		}
	}
	if kind == "writeStdin" {
		path, ok := a["cwd"].(string)
		if !ok {
			return nil, ErrSessionCommand
		}
		converted, err := permissionPathURI(path)
		if err != nil {
			return nil, err
		}
		a["cwd"] = converted
	}
	if kind == "requestPermissions" {
		if err := nativeRequestedPermissions(a); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func permissionPathURI(path string) (string, error) {
	if strings.HasPrefix(path, "/") {
		return (&url.URL{Scheme: "file", Path: path}).String(), nil
	}
	u, err := url.Parse(path)
	if err != nil || u.Scheme == "" {
		return "", ErrSessionCommand
	}
	return path, nil
}

func nativeRequestedPermissions(action map[string]any) error {
	p, ok := action["permissions"].(map[string]any)
	if !ok {
		return ErrSessionCommand
	}
	for key := range p {
		if key != "network" && key != "fileSystem" {
			return ErrSessionCommand
		}
	}
	if n, ok := p["network"].(map[string]any); ok {
		for key := range n {
			if key != "enabled" {
				return ErrSessionCommand
			}
		}
	}
	if raw, exists := p["fileSystem"]; exists && raw != nil {
		fs, ok := raw.(map[string]any)
		if !ok {
			return ErrSessionCommand
		}
		for key := range fs {
			if key != "read" && key != "write" && key != "entries" && key != "globScanMaxDepth" {
				return ErrSessionCommand
			}
		}
		entries, hasEntries := fs["entries"].([]any)
		if entries == nil {
			entries = []any{}
		}
		if !hasEntries {
			for _, mode := range []string{"read", "write"} {
				if paths, ok := fs[mode].([]any); ok {
					for _, path := range paths {
						entries = append(entries, map[string]any{"path": map[string]any{"type": "path", "path": path}, "access": mode})
					}
				}
			}
		}
		for _, raw := range entries {
			e, ok := raw.(map[string]any)
			if !ok {
				return ErrSessionCommand
			}
			path, ok := e["path"].(map[string]any)
			if !ok {
				return ErrSessionCommand
			}
			if path["type"] == "path" {
				s, ok := path["path"].(string)
				if !ok {
					return ErrSessionCommand
				}
				converted, err := permissionPathURI(s)
				if err != nil {
					return err
				}
				path["path"] = converted
			}
		}
		converted := map[string]any{"entries": entries}
		if v, ok := fs["globScanMaxDepth"]; ok {
			converted["glob_scan_max_depth"] = v
		}
		p["file_system"] = converted
		delete(p, "fileSystem")
	}
	if v, exists := p["fileSystem"]; exists && v == nil {
		p["file_system"] = nil
		delete(p, "fileSystem")
	}
	return nil
}
