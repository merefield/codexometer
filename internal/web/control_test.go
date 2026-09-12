package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

type actionSource struct {
	cancelledSource
	mu       sync.Mutex
	pending  bool
	prompt   codex.SessionPromptOffer
	calls    int
	decision string
	answers  []string
	err      error
}

func (f *actionSource) SessionApprovalPending(token string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending && token == "private-approval"
}
func (f *actionSource) RespondSessionApproval(_ context.Context, token, decision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pending || token != "private-approval" {
		return errUnavailable
	}
	f.pending = false
	f.calls++
	f.decision = decision
	return f.err
}
func (f *actionSource) SessionPrompt(thread string) codex.SessionPromptOffer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if thread != f.prompt.ThreadID {
		return codex.SessionPromptOffer{}
	}
	return f.prompt
}
func (f *actionSource) SendSessionPrompt(_ context.Context, token string, answers []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if token == "" || token != f.prompt.Token {
		return errUnavailable
	}
	f.prompt.Token = ""
	f.calls++
	f.answers = append([]string{}, answers...)
	return f.err
}

func controlServer(t *testing.T) (*server, *actionSource, string) {
	t.Helper()
	s := testServer()
	s.store.state.Control = true
	f := &actionSource{pending: true}
	s.control = newControl(f, s.store)
	s.store.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "parent", WorkingDirectory: "/project", Context: codex.SessionContext{
		Kind: codex.SessionContextApproval, ThreadID: "child", ApprovalToken: "private-approval",
		CommandDetails:  codex.ApprovalCommandDetails{Command: "git status", Directory: "/project/child"},
		ApprovalOptions: [8]codex.ApprovalOption{{Kind: "accept", Value: "accept"}, {Kind: "cancel", Value: "cancel"}},
	}}}}, nil, time.Now())
	return s, f, pairBrowser(t, s)
}

func actionCall(s *server, token, action string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "POST", "/api/control/"+action, string(data), token))
	return w
}
func getOffer(t *testing.T, s *server, token string) actionOffer {
	t.Helper()
	w := actionCall(s, token, "offer", actionRequest{Session: "parent"})
	var o actionOffer
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &o) != nil || o.ID == "" {
		t.Fatalf("offer %d: %s", w.Code, w.Body)
	}
	return o
}
func prepareAction(t *testing.T, s *server, token string, r actionRequest) actionRequest {
	t.Helper()
	w := actionCall(s, token, "prepare", r)
	var result struct {
		Confirmation string `json:"confirmation"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Confirmation == "" {
		t.Fatalf("prepare %d: %s", w.Code, w.Body)
	}
	return actionRequest{Session: r.Session, Offer: r.Offer, Confirmation: result.Confirmation}
}

func TestControlApprovalBindingAndExactlyOnce(t *testing.T) {
	s, f, token := controlServer(t)
	o := getOffer(t, s, token)
	if o.Thread != "child" || o.Directory != "/project/child" || o.Command != "git status" || len(o.Choices) != 2 {
		t.Fatalf("wrong target: %+v", o)
	}
	index := 0
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Choice: &index})
	if f.calls != 0 {
		t.Fatal("prepare dispatched action")
	}
	wrong := r
	wrong.Session = "another"
	if w := actionCall(s, token, "commit", wrong); w.Code != 409 {
		t.Fatal("cross-session confirmation accepted")
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Go(func() { statuses <- actionCall(s, token, "commit", r).Code })
	}
	wg.Wait()
	close(statuses)
	success := 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status != 409 {
			t.Fatal(status)
		}
	}
	if success != 1 || f.calls != 1 || f.decision != "accept" {
		t.Fatalf("duplicate dispatch: %d/%d", success, f.calls)
	}
}

func TestControlStaleConfirmation(t *testing.T) {
	for _, kind := range []string{"expired", "stale", "error", "removed", "changed-command", "changed-choice", "source-consumed", "connection-change"} {
		t.Run(kind, func(t *testing.T) {
			s, f, token := controlServer(t)
			o := getOffer(t, s, token)
			index := 0
			r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Choice: &index})
			switch kind {
			case "expired":
				s.control.pending.until = time.Now().Add(-time.Second)
			case "stale":
				s.store.state.SessionsAt = time.Now().Add(-11 * time.Second)
			case "error":
				s.store.state.SessionsError = true
			case "removed":
				delete(s.store.contexts, "parent")
			case "source-consumed":
				f.pending = false
			default:
				c := s.store.contexts["parent"]
				if kind == "changed-command" {
					c.CommandDetails.Command = "different command"
				}
				if kind == "changed-choice" {
					c.ApprovalOptions[0].Value = "different decision"
				}
				if kind == "connection-change" {
					c.ApprovalToken = "another-connection"
				}
				s.store.contexts["parent"] = c
			}
			if w := actionCall(s, token, "commit", r); w.Code != 409 || f.calls != 0 {
				t.Fatalf("stale action accepted: %d", w.Code)
			}
		})
	}
}

func TestControlValidationAndUncertainOutcome(t *testing.T) {
	s, f, token := controlServer(t)
	o := getOffer(t, s, token)
	for _, index := range []int{-1, 2, 500} {
		if w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, Choice: &index}); w.Code != 409 {
			t.Fatal("invented choice accepted")
		}
	}
	index := 1
	r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Choice: &index})
	f.err = errors.New("private command/token failure")
	w := actionCall(s, token, "commit", r)
	if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("error leaked: %s", w.Body)
	}
	if w = actionCall(s, token, "commit", r); w.Code != 409 || f.calls != 1 {
		t.Fatal("uncertain action retried")
	}
}

func TestControlPromptAndQuestions(t *testing.T) {
	for _, question := range []bool{false, true} {
		t.Run(map[bool]string{false: "follow-up", true: "structured question"}[question], func(t *testing.T) {
			s, f, token := controlServer(t)
			c := codex.SessionContext{Kind: codex.SessionContextReply}
			f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "private-input"}
			if question {
				c.Kind = codex.SessionContextQuestion
				c.ThreadID = "child"
				c.InputToken = "private-input"
				f.prompt.ThreadID = "child"
				f.prompt.Questions = []codex.PromptQuestion{{ID: "q", Text: "Which?", Options: []string{"Yes", "No"}}}
			}
			s.store.contexts["parent"] = c
			o := getOffer(t, s, token)
			if question && (o.Thread != "child" || o.Directory != "") {
				t.Fatal("child question misattributed to parent directory")
			}
			for _, answers := range [][]string{nil, {""}, {"\x1b[31mYes"}, {strings.Repeat("a", 4097)}, {"Yes", "No"}} {
				if w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, Answers: answers}); w.Code != 409 {
					t.Fatal("invalid answers accepted")
				}
			}
			if question {
				if w := actionCall(s, token, "prepare", actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"Invented"}}); w.Code != 409 {
					t.Fatal("unadvertised answer accepted")
				}
			}
			r := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"Yes"}})
			r.Answers = []string{"tampered after preparation"}
			if w := actionCall(s, token, "commit", r); w.Code != 200 || f.calls != 1 || f.answers[0] != "Yes" {
				t.Fatalf("wrong prompt: %d %v", w.Code, f.answers)
			}
			if w := actionCall(s, token, "commit", r); w.Code != 409 {
				t.Fatal("replayed prompt accepted")
			}
		})
	}
}

func TestControlSecurityBoundary(t *testing.T) {
	for _, action := range []string{"offer", "prepare", "commit"} {
		for _, kind := range []string{"read-only", "unauthenticated", "expired-token", "origin", "missing-origin", "fetch-site", "host", "content-type", "unknown-field", "trailing-json", "oversize", "method"} {
			t.Run(action+"/"+kind, func(t *testing.T) {
				s, f, token := controlServer(t)
				r := request(s, "POST", "/api/control/"+action, `{"session":"parent"}`, token)
				want := 403
				switch kind {
				case "read-only":
					s.control = nil
					want = 404
				case "unauthenticated":
					r.Header.Del("Authorization")
					want = 401
				case "expired-token":
					s.tokenUntil = time.Now().Add(-time.Second)
					want = 401
				case "origin":
					r.Header.Set("Origin", "https://evil.example")
				case "missing-origin":
					r.Header.Del("Origin")
				case "fetch-site":
					r.Header.Set("Sec-Fetch-Site", "cross-site")
				case "host":
					r.Host = "evil.example"
				case "content-type":
					r.Header.Set("Content-Type", "text/plain")
				case "method":
					r.Method = http.MethodGet
					want = 404
				default:
					body := `{"session":"parent","unknown":true}`
					if kind == "trailing-json" {
						body = `{"session":"parent"}{}`
					}
					if kind == "oversize" {
						body = `{"session":"` + strings.Repeat("a", 70<<10) + `"}`
					}
					r = request(s, "POST", "/api/control/"+action, body, token)
					want = 400
				}
				w := httptest.NewRecorder()
				s.handler().ServeHTTP(w, r)
				if w.Code != want || f.calls != 0 {
					t.Fatalf("got %d want %d", w.Code, want)
				}
			})
		}
	}
}

func TestControlCapabilitiesStayPrivateAndUnsupportedFailClosed(t *testing.T) {
	s, _, token := controlServer(t)
	for _, data := range [][]byte{func() []byte { d, _ := s.store.snapshot(); return d }(), actionCall(s, token, "offer", actionRequest{Session: "parent"}).Body.Bytes()} {
		if strings.Contains(string(data), "private-approval") || strings.Contains(string(data), "ApprovalToken") {
			t.Fatal("raw capability exposed")
		}
	}
	c := s.store.contexts["parent"]
	c.ApprovalBlocked = "sanitised"
	s.store.contexts["parent"] = c
	w := actionCall(s, token, "offer", actionRequest{Session: "parent"})
	var o actionOffer
	_ = json.Unmarshal(w.Body.Bytes(), &o)
	if w.Code != 200 || o.ID != "" {
		t.Fatal("unsupported command made actionable")
	}
	// Working/quiet/local rows cannot invent a follow-up capability.
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextActivity, Source: "LOCAL"}
	w = actionCall(s, token, "offer", actionRequest{Session: "parent"})
	_ = json.Unmarshal(w.Body.Bytes(), &o)
	if o.ID != "" {
		t.Fatal("inferred state granted control")
	}
}

func TestControlPreparationReplacementAndPromptInvalidation(t *testing.T) {
	s, f, token := controlServer(t)
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextReply}
	f.prompt = codex.SessionPromptOffer{ThreadID: "parent", Token: "idle-1"}
	o := getOffer(t, s, token)
	first := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"First"}})
	second := prepareAction(t, s, token, actionRequest{Session: "parent", Offer: o.ID, Answers: []string{"Second"}})
	if w := actionCall(s, token, "commit", first); w.Code != 409 {
		t.Fatal("replaced preparation accepted")
	}
	f.prompt.Token = "idle-2" // Reconnect/new turn must invalidate the draft.
	if w := actionCall(s, token, "commit", second); w.Code != 409 || f.calls != 0 {
		t.Fatal("changed prompt capability accepted")
	}
	// A question from another child/request is not the current selected context.
	s.store.contexts["parent"] = codex.SessionContext{Kind: codex.SessionContextQuestion, ThreadID: "parent", InputToken: "different"}
	f.prompt.Questions = []codex.PromptQuestion{{ID: "q", Text: "Question", FreeText: true}}
	w := actionCall(s, token, "offer", actionRequest{Session: "parent"})
	var offer actionOffer
	_ = json.Unmarshal(w.Body.Bytes(), &offer)
	if w.Code != 200 || offer.ID != "" {
		t.Fatal("mismatched input offered")
	}
}

func TestReadOnlyStoreDoesNotRetainControlCapabilities(t *testing.T) {
	s := newStore()
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{{ID: "private", Context: codex.SessionContext{ApprovalToken: "secret"}}}}, nil, time.Now())
	if len(s.contexts) != 0 || len(s.directories) != 0 {
		t.Fatal("read-only store retained control context")
	}
}

func TestReplyIsNotHiddenByPreviousApprovalJustification(t *testing.T) {
	s := newStore()
	c := codex.SessionContext{Kind: codex.SessionContextApproval, Text: "Original request", CommandDetails: codex.ApprovalCommandDetails{Command: "git status", Justification: "Why this command"}}
	row := codex.LiveUsageSession{ID: "session", Context: c}
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{row}}, nil, time.Now())
	if s.state.Sessions[0].Text != c.CommandDetails.Justification {
		t.Fatal("approval justification not projected")
	}
	row.Context.Kind, row.Context.Text = codex.SessionContextReply, "New reply"
	s.live(codex.LiveUsageSnapshot{Sessions: []codex.LiveUsageSession{row}}, nil, time.Now())
	if s.state.Sessions[0].Text != "New reply" {
		t.Fatal("old approval hid new reply")
	}
}
