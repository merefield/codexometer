package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/merefield/codexometer/internal/codex"
)

type terminalsProbe struct{ ids []string }

func (p *terminalsProbe) InspectSessionTerminals(_ context.Context, id string) (codex.TerminalInspection, error) {
	p.ids = append(p.ids, id)
	return codex.TerminalInspection{Terminals: []codex.SessionTerminal{{Command: "go test"}}}, nil
}

func TestSessionInspectorTerminalsReadOnlyAuthorization(t *testing.T) {
	s := testServer()
	p := &terminalsProbe{}
	s.terminals = p
	s.savedRequests = make(chan struct{}, 2)
	s.store.state.Sessions = []session{{ID: "one"}}
	token := pairBrowser(t, s)
	for _, tc := range []struct {
		method, root, token string
		status              int
	}{{"GET", "one", "", 401}, {"GET", "another", token, 404}, {"POST", "one", token, 404}, {"GET", "one", token, 200}} {
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, tc.method, "/api/sessions/inspect?kind=terminals&root="+tc.root, "", tc.token))
		if w.Code != tc.status || tc.status == 200 && !strings.Contains(w.Body.String(), "go test") {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	if len(p.ids) != 1 || p.ids[0] != "one" || s.control != nil {
		t.Fatal(p.ids)
	}
}
