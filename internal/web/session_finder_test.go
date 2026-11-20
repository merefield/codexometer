package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/confirmation"
)

type finderProbe struct{ calls int }

func (f *finderProbe) BrowseSavedSessions(_ context.Context, scope, cursor, id string) (codex.SavedPage, error) {
	f.calls++
	if scope == "scope" && cursor == "" && id == "" {
		return codex.SavedPage{Sessions: []codex.SavedSession{
			{ID: "saved", Name: "Blue dashboard", UpdatedAt: 1_730_000_000},
			{ID: "blue", Name: "Other", Directory: "/blue"},
		}}, nil
	}
	if scope != "scope" || cursor != "page" || id != "selected" {
		return codex.SavedPage{}, codex.ErrSavedSessions
	}
	return codex.SavedPage{Turns: []codex.SavedTurn{
		{Task: "**literal prompt**", Reply: "**past reply**\n\n| Name | Count |\n| --- | ---: |\n| Items | 2 |"},
		{Reply: "**private answer**", Restricted: true},
	}}, nil
}

func TestSavedRouteFiltersNamesEvenWithListOnlySource(t *testing.T) {
	s := testServer()
	s.finder = &finderProbe{}
	s.savedRequests = make(chan struct{}, 2)
	token := pairBrowser(t, s)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, request(s, "GET", "/api/sessions/saved?scope=scope&q=bLuE", "", token))
	var page transcriptPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); w.Code != 200 || err != nil || len(page.Sessions) != 1 {
		t.Fatal("name filter returned unexpected results", w.Code, w.Body.String(), err)
	}
	if row := page.Sessions[0]; row.ID != "saved" || row.UpdatedAt != 1_730_000_000 {
		t.Fatal("name filter lost timestamp or matched the path/ID instead", row)
	}
}

type resumeProbe struct{ calls int }

func (p *resumeProbe) ResumeSavedSession(context.Context, string, string, string) error {
	p.calls++
	return nil
}

func TestStrictResumeRequiresExactOneUseConsent(t *testing.T) {
	s := testServer()
	p := &resumeProbe{}
	s.resumer = p
	s.savedRequests = make(chan struct{}, 2)
	s.store.state.ConfirmationMode = confirmation.Require
	token := pairBrowser(t, s)
	post := func(body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, request(s, "POST", "/api/sessions/resume", body, token))
		if w.Code != status {
			t.Fatal(w.Code, w.Body.String())
		}
		return w
	}
	prepare := func() string {
		t.Helper()
		w := post(`{"scope":"scope","id":"one","mode":"prepare"}`, 200)
		var reply struct{ Confirmation string }
		if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Confirmation == "" {
			t.Fatal(w.Body.String())
		}
		return reply.Confirmation
	}
	post(`{"scope":"scope","id":"one"}`, 409)
	consent := prepare()
	post(`{"scope":"scope","id":"other","confirmation":"`+consent+`"}`, 409)
	post(`{"scope":"scope","id":"one","confirmation":"`+consent+`"}`, 409)
	if p.calls != 0 {
		t.Fatal("an unconfirmed action reached the daemon")
	}
	consent = prepare()
	body := `{"scope":"scope","id":"one","confirmation":"` + consent + `"}`
	post(body, 204)
	post(body, 409)
	if p.calls != 1 {
		t.Fatal("consent was reused")
	}
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
		if tc.code == 200 {
			var page transcriptPage
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Turns) != 2 {
				t.Fatal(w.Body.String(), err)
			}
			reply, restricted := page.Turns[0], page.Turns[1]
			if reply.Task != "**literal prompt**" || len(reply.Prose) != 2 || !reply.Prose[0].Runs[0].Bold ||
				len(reply.Prose[1].Rows) != 2 || reply.Prose[1].Rows[1][0][0].Text != "Items" {
				t.Fatal("reply was not projected as Markdown", reply)
			}
			if !restricted.Restricted || restricted.Reply != "" || len(restricted.Prose) != 0 {
				t.Fatal("restricted reply was exposed", restricted)
			}
		}
	}
	if s.control != nil || f.calls != 1 {
		t.Fatal("read-only route coupling or authorization", f.calls)
	}
}
