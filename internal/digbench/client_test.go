package digbench

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStartSessionUsesBearerAndDecodesState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/sessions" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer secret" {
			t.Fatalf("authorization = %q", authorization)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || !strings.Contains(string(body), `"game":"P-1"`) {
			t.Fatalf("body = %q, err = %v", body, err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"session_id":"session-1","game":"P-1","step_index":0,
			"done":false,"levels_beaten":0,"seed":42,"framework_version":"abc",
			"state":{"status":"in_progress","done":false,"level":1,"max_level":3,
			"observation":"___","actions":["a","b"]}
		}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()}
	model := "gpt-5.6-sol"
	session, err := client.StartSession(context.Background(), StartRequest{Game: "P-1", ModelName: &model})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "session-1" || session.Seed == nil || *session.Seed != 42 || session.State.MaxLevel == nil || *session.State.MaxLevel != 3 {
		t.Fatalf("session = %#v", session)
	}
}

func TestListGamesUsesBearerAndReturnsCompleteCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/games" || request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request = %s %s headers=%#v", request.Method, request.URL.Path, request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"games":["P-1","P-2","P-17"],"game_details":[{"name":"P-1","tier":1}]}`))
	}))
	defer server.Close()

	response, err := (Client{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()}).ListGames(context.Background())
	if err != nil || len(response.Games) != 3 || response.Games[2] != "P-17" {
		t.Fatalf("response=%#v error=%v", response, err)
	}
}

func TestStepUsesSessionScopeRetriesAndDetectsWin(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-Session-ID") != "session-1" {
			t.Fatalf("unexpected authentication headers: %#v", request.Header)
		}
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"detail":"temporary"}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"session_id":"session-1","step_index":7,"done":true,"levels_beaten":3,
			"state":{"status":"completed","done":true,"level":3,"max_level":3,
			"observation":"WIN","actions":[]}
		}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, HTTPClient: server.Client()}
	response, err := client.Step(context.Background(), "session-1", StepRequest{Action: "b", StepIndex: 7})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !response.Done || !response.State.Done || response.State.Status != "completed" || response.LevelsBeaten != 3 {
		t.Fatalf("calls = %d, response = %#v", calls.Load(), response)
	}
}

func TestStartSessionDoesNotRetryAnAmbiguousFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"detail":"unknown commit state"}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()}
	_, err := client.StartSession(context.Background(), StartRequest{Game: "P-1"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError || calls.Load() != 1 {
		t.Fatalf("calls = %d, error = %v", calls.Load(), err)
	}
}

func TestSessionValidationRejectsContradictoryOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{
			"session_id":"session-1","step_index":1,"done":true,"levels_beaten":1,
			"state":{"status":"completed","done":false,"level":1,"observation":"?","actions":[]}
		}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, HTTPClient: server.Client()}
	_, err := client.GetSession(context.Background(), "session-1")
	if err == nil || !strings.Contains(err.Error(), "terminal status did not set done") {
		t.Fatalf("error = %v", err)
	}
}

func TestBearerOperationsRequireToken(t *testing.T) {
	_, err := (Client{}).ListGames(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DIGBENCH_API_TOKEN") {
		t.Fatalf("error = %v", err)
	}
}
