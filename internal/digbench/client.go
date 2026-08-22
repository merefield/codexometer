package digbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL  = "https://api.digbench.ai/api/agent"
	maxResponseSize = 1 << 20
	maxStepAttempts = 4
)

// Client is the minimal DigBench Agent REST API client used by the proof of concept.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type GameDetail struct {
	Name string `json:"name"`
	Tier *int   `json:"tier"`
}

type GamesResponse struct {
	Games       []string     `json:"games"`
	GameDetails []GameDetail `json:"game_details"`
}

type State struct {
	Actions        []string `json:"actions"`
	Done           bool     `json:"done"`
	Level          int      `json:"level"`
	LivesLeft      *int     `json:"lives_left"`
	MaxLevel       *int     `json:"max_level"`
	MaxSteps       *int     `json:"max_steps"`
	Mode           *string  `json:"mode"`
	Observation    string   `json:"observation"`
	StartingLives  *int     `json:"starting_lives"`
	Status         string   `json:"status"`
	StepsRemaining any      `json:"steps_remaining"`
	Transition     *string  `json:"transition"`
}

type Session struct {
	Description      string          `json:"description"`
	Done             bool            `json:"done"`
	FrameworkVersion *string         `json:"framework_version"`
	Game             string          `json:"game"`
	LevelsBeaten     int             `json:"levels_beaten"`
	MoveSchema       json.RawMessage `json:"move_schema"`
	Seed             *int64          `json:"seed"`
	SessionID        string          `json:"session_id"`
	State            State           `json:"state"`
	StepIndex        int             `json:"step_index"`
}

type StartRequest struct {
	Game         string  `json:"game"`
	ModelName    *string `json:"model_name,omitempty"`
	ModelVersion *string `json:"model_version,omitempty"`
	Notes        *string `json:"notes,omitempty"`
}

type StepRequest struct {
	Action    string  `json:"action"`
	Reasoning *string `json:"reasoning,omitempty"`
	StepIndex int     `json:"step_index"`
}

type StepResponse struct {
	Session
	InvalidAction *bool `json:"invalid_action"`
}

type APIError struct {
	StatusCode int
	Detail     string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if strings.TrimSpace(e.Detail) == "" {
		return fmt.Sprintf("DigBench API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("DigBench API returned HTTP %d: %s", e.StatusCode, e.Detail)
}

func (c Client) ListGames(ctx context.Context) (GamesResponse, error) {
	var response GamesResponse
	err := c.do(ctx, http.MethodGet, "/games", nil, true, true, &response)
	return response, err
}

// StartSession intentionally does not retry. POST /sessions is not documented
// as idempotent, so retrying a lost response could create an orphaned second run.
func (c Client) StartSession(ctx context.Context, request StartRequest) (Session, error) {
	var response Session
	if strings.TrimSpace(request.Game) == "" {
		return response, errors.New("DigBench game is required")
	}
	err := c.do(ctx, http.MethodPost, "/sessions", request, true, false, &response)
	if err == nil {
		err = validateSession(response)
	}
	return response, err
}

func (c Client) GetSession(ctx context.Context, sessionID string) (Session, error) {
	var response Session
	if strings.TrimSpace(sessionID) == "" {
		return response, errors.New("DigBench session id is required")
	}
	path := "/sessions/" + url.PathEscape(sessionID)
	err := c.do(ctx, http.MethodGet, path, nil, false, true, &response)
	if err == nil {
		err = validateSession(response)
	}
	return response, err
}

func (c Client) Step(ctx context.Context, sessionID string, request StepRequest) (StepResponse, error) {
	var response StepResponse
	if strings.TrimSpace(sessionID) == "" {
		return response, errors.New("DigBench session id is required")
	}
	if strings.TrimSpace(request.Action) == "" {
		return response, errors.New("DigBench action is required")
	}
	if request.StepIndex < 1 {
		return response, errors.New("DigBench step index must be positive")
	}
	path := "/sessions/" + url.PathEscape(sessionID) + "/step"
	err := c.do(ctx, http.MethodPost, path, request, false, true, &response)
	if err == nil {
		err = validateSession(response.Session)
	}
	return response, err
}

func (c Client) do(ctx context.Context, method, path string, body any, bearer, retry bool, output any) error {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	if bearer && strings.TrimSpace(c.Token) == "" {
		return errors.New("DIGBENCH_API_TOKEN is required")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode DigBench request: %w", err)
	}
	if body == nil {
		payload = nil
	}
	attempts := 1
	if retry {
		attempts = maxStepAttempts
	}
	var retryAfter time.Duration
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := retryAfter
			if delay <= 0 {
				delay = time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
			}
			retryAfter = 0
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create DigBench request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if bearer {
			request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
		} else {
			request.Header.Set("X-Session-ID", strings.TrimSpace(pathSessionID(path)))
		}
		client := c.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		response, err := client.Do(request)
		if err != nil {
			if retry && attempt+1 < attempts && ctx.Err() == nil {
				continue
			}
			return fmt.Errorf("call DigBench API: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			if retry && attempt+1 < attempts {
				continue
			}
			return fmt.Errorf("read DigBench response: %w", readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close DigBench response: %w", closeErr)
		}
		if len(data) > maxResponseSize {
			return errors.New("DigBench response exceeds 1 MiB")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			apiErr := decodeAPIError(response, data)
			if retry && attempt+1 < attempts && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500) {
				retryAfter = apiErr.RetryAfter
				continue
			}
			return apiErr
		}
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("decode DigBench response: %w", err)
		}
		return nil
	}
	return errors.New("DigBench request exhausted retries")
}

func pathSessionID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sessions" {
		return ""
	}
	value, err := url.PathUnescape(parts[1])
	if err != nil {
		return parts[1]
	}
	return value
}

func decodeAPIError(response *http.Response, data []byte) *APIError {
	var payload struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(data, &payload)
	retryAfter := time.Duration(0)
	if seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After"))); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	}
	return &APIError{StatusCode: response.StatusCode, Detail: payload.Detail, RetryAfter: retryAfter}
}

func validateSession(session Session) error {
	if strings.TrimSpace(session.SessionID) == "" {
		return errors.New("DigBench response omitted session id")
	}
	if session.StepIndex < 0 || session.LevelsBeaten < 0 {
		return errors.New("DigBench response contained a negative progress counter")
	}
	switch session.State.Status {
	case "in_progress":
		if session.Done || session.State.Done {
			return errors.New("DigBench response marked an in-progress game done")
		}
	case "completed", "game_over":
		if !session.Done || !session.State.Done {
			return errors.New("DigBench terminal status did not set done")
		}
	default:
		return fmt.Errorf("DigBench response returned unknown status %q", session.State.Status)
	}
	return nil
}
