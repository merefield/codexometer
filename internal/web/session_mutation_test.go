package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

type startSessionProbe struct {
	prompt string
	err    error
}

func (p *startSessionProbe) StartSession(_ context.Context, _, _, prompt string) (string, error) {
	p.prompt = prompt
	return "new-thread", p.err
}

func TestNewSessionRouteRequiresWriteModePairingAndExactConsent(t *testing.T) {
	s := testServer()
	s.savedRequests = make(chan struct{}, 2)
	s.store.state.ConfirmationMode = confirmation.Require
	token := pairBrowser(t, s)
	post := func(bearer string, b map[string]string, code int) []byte {
		t.Helper()
		body, _ := json.Marshal(b)
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, "POST", "/api/sessions/new", string(body), bearer))
		if w.Code != code {
			t.Fatalf("want %d, got %d: %s", code, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	body := map[string]string{"scope": "scope", "directory": t.TempDir(), "prompt": "first"}
	post(token, body, 404)
	probe := &startSessionProbe{}
	s.starter = probe
	post("", body, 401)
	post(token, body, 409)
	prepare := func() {
		t.Helper()
		delete(body, "confirmation")
		body["mode"] = "prepare"
		var reply struct{ Confirmation string }
		if err := json.Unmarshal(post(token, body, 200), &reply); err != nil || reply.Confirmation == "" {
			t.Fatal("missing confirmation", err)
		}
		delete(body, "mode")
		body["confirmation"] = reply.Confirmation
	}
	prepare()
	body["prompt"] = "changed"
	post(token, body, 409)
	if probe.prompt != "" {
		t.Fatal("changed prompt escaped confirmation")
	}
	body["prompt"] = "first"
	prepare()
	post(token, body, 200)
	post(token, body, 409)
	if probe.prompt != "first" {
		t.Fatal("confirmed prompt did not reach backend", probe.prompt)
	}
	for _, tc := range []struct {
		err              error
		code             int
		firstMessageOnly bool
	}{
		{codex.ErrSessionStartTrust, 400, false},
		{codex.ErrSessionStartRejected, 400, false},
		{codex.ErrSessionStartUncertain, 502, false},
		{codex.ErrSessionFirstPrompt, 200, true},
	} {
		probe.err = tc.err
		prepare()
		reply := post(token, body, tc.code)
		if tc.firstMessageOnly {
			var result struct {
				ID                   string
				FirstMessageRejected bool
			}
			if err := json.Unmarshal(reply, &result); err != nil || result.ID != "new-thread" || !result.FirstMessageRejected {
				t.Fatal("definite first prompt rejection must preserve the known session ID", string(reply))
			}
		}
	}
}
