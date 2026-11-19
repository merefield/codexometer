package web

import (
	"context"
	"github.com/merefield/codexometer/internal/codex"
	"net/http/httptest"
	"testing"
)

type finderProbe struct{ calls int }

func (f *finderProbe) BrowseSavedSessions(_ context.Context, scope, cursor, id string) (codex.SavedPage, error) {
	f.calls++
	if scope != "scope" || cursor != "page" || id != "selected" {
		return codex.SavedPage{}, codex.ErrSavedSessions
	}
	return codex.SavedPage{Turns: []codex.SavedTurn{{Reply: "past reply"}}}, nil
}
func TestSavedSessionRouteUsesReadOnlyPairing(t *testing.T) {
	s := testServer()
	f := &finderProbe{}
	s.finder = f
	s.savedRequests = make(chan struct{}, 2)
	token := pairBrowser(t, s)
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {token, 200}} {
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, "GET", "/api/sessions/saved?scope=scope&cursor=page&id=selected", "", tc.token))
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if s.control != nil || f.calls != 1 {
		t.Fatal("read-only route coupling or authorization", f.calls)
	}
}
