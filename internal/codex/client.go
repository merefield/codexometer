package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const requestTimeout = 15 * time.Second

// Client fetches the current quota snapshot through Codex's stable app-server API.
type Client struct {
	Binary    string
	LiveUsage *LiveUsageReader
	// BenchmarkAPIKey is used only by isolated benchmark app-server sessions.
	// Fetch and local monitoring continue to use the prevailing Codex login.
	BenchmarkAPIKey string
	// DigBenchToken is retained in memory only and enables the external P-1
	// option in the Benchmark tab.
	DigBenchToken string
}

// FetchTokenUsage reads live telemetry from locally observed Codex sessions.
func (c Client) FetchTokenUsage(ctx context.Context) (LiveUsageSnapshot, error) {
	if c.LiveUsage == nil {
		return LiveUsageSnapshot{}, errors.New("local Codex session telemetry is not configured")
	}
	return c.LiveUsage.FetchTokenUsage(ctx)
}

// FetchTokenUsageFresh forces complete local rollout discovery for a final
// Monitor reading.
func (c Client) FetchTokenUsageFresh(ctx context.Context) (LiveUsageSnapshot, error) {
	if c.LiveUsage == nil {
		return LiveUsageSnapshot{}, errors.New("local Codex session telemetry is not configured")
	}
	return c.LiveUsage.FetchTokenUsageFresh(ctx)
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// Fetch starts a short-lived app-server, performs the initialization handshake,
// reads the authenticated account limits, and shuts the server down again.
func (c Client) Fetch(ctx context.Context) (Snapshot, error) {
	binary := c.Binary
	if binary == "" {
		binary = "codex"
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Snapshot{}, fmt.Errorf("open Codex input: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Snapshot{}, fmt.Errorf("open Codex output: %w", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("Codex CLI not found; install it or pass --codex PATH")
		}
		return Snapshot{}, fmt.Errorf("start Codex app-server: %w", err)
	}

	var waitOnce sync.Once
	wait := func() { waitOnce.Do(func() { _ = cmd.Wait() }) }
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		wait()
	}()

	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(bufio.NewReader(stdout))

	if err := encoder.Encode(map[string]any{
		"method": "initialize",
		"id":     1,
		"params": map[string]any{"clientInfo": codexometerClientInfo()},
	}); err != nil {
		return Snapshot{}, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if _, err := responseFor(decoder, 1); err != nil {
		return Snapshot{}, withServerError("initialize Codex app-server", err, stderr.String())
	}

	if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		return Snapshot{}, fmt.Errorf("acknowledge Codex app-server: %w", err)
	}
	accountFingerprint := ""
	if err := encoder.Encode(map[string]any{
		"method": "account/read",
		"id":     2,
		"params": map[string]any{},
	}); err != nil {
		return Snapshot{}, fmt.Errorf("request Codex account identity: %w", err)
	}
	if accountResult, accountErr := responseFor(decoder, 2); accountErr == nil {
		accountFingerprint = fingerprintAccount(accountResult)
	}
	if err := encoder.Encode(map[string]any{
		"method": "account/rateLimits/read",
		"id":     3,
	}); err != nil {
		return Snapshot{}, fmt.Errorf("request Codex quotas: %w", err)
	}

	result, err := responseFor(decoder, 3)
	if err != nil {
		return Snapshot{}, withServerError("read Codex quotas", err, stderr.String())
	}

	var snapshot Snapshot
	if err := json.Unmarshal(result, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode Codex quotas: %w", err)
	}
	snapshot.AccountFingerprint = accountFingerprint
	snapshot.FetchedAt = time.Now()
	return snapshot, nil
}

func fingerprintAccount(result json.RawMessage) string {
	var response struct {
		Account *struct {
			Type  string  `json:"type"`
			Email *string `json:"email"`
		} `json:"account"`
	}
	if json.Unmarshal(result, &response) != nil || response.Account == nil || response.Account.Email == nil {
		return ""
	}
	email := strings.ToLower(strings.TrimSpace(*response.Account.Email))
	if email == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(response.Account.Type)) + "\x00" + email))
	return fmt.Sprintf("%x", sum[:16])
}

func responseFor(decoder *json.Decoder, id int) (json.RawMessage, error) {
	for {
		var response rpcResponse
		if err := decoder.Decode(&response); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("Codex app-server closed unexpectedly")
			}
			return nil, err
		}
		if response.ID == nil || *response.ID != id {
			continue
		}
		if response.Error != nil {
			return nil, fmt.Errorf("%s (RPC %d)", response.Error.Message, response.Error.Code)
		}
		return response.Result, nil
	}
}

func withServerError(action string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr != "" {
		return fmt.Errorf("%s: %w: %s", action, err, stderr)
	}
	return fmt.Errorf("%s: %w", action, err)
}
