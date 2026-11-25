package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merefield/codexometer/internal/codex"
)

type sessionLinkProbe struct{ reads int }

func (p *sessionLinkProbe) PreviewSessionLink(_ context.Context, _, id string) (codex.SessionLinkPreview, error) {
	p.reads++
	return codex.SessionLinkPreview{Root: codex.SavedSession{ID: id}, Turns: []codex.SavedTurn{
		{Reply: "[Follow](codex://threads/" + id + ")"},
		{Reply: "SECRET", Restricted: true},
	}}, nil
}

func TestLinkedSessionPreviewWebIsPairedScopedAndReadOnly(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	s, probe := testServer(), &sessionLinkProbe{}
	s.savedRequests = make(chan struct{}, 2)
	s.threadLinks = probe
	s.store.state.SavedScope = "scope"
	token := pairBrowser(t, s)
	for _, tc := range []struct {
		method, scope, id, token string
		code                     int
	}{
		{"GET", "scope", id, "", 401}, {"GET", "stale", id, token, 404},
		{"GET", "scope", "not-an-id", token, 404}, {"POST", "scope", id, token, 404},
		{"GET", "scope", id, token, 200},
	} {
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, tc.method, "/api/sessions/linked?scope="+tc.scope+"&id="+tc.id, "", tc.token))
		if w.Code != tc.code || tc.code == 200 && (!strings.Contains(w.Body.String(), `"threadId":"`+id) || strings.Contains(w.Body.String(), "SECRET")) {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	if probe.reads != 1 || s.control != nil {
		t.Fatal("paired scoped GET must be the only request that reaches the client", probe.reads)
	}
}
