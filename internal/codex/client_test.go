package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/merefield/codexometer/internal/version"
)

func TestCodexometerClientInfoUsesResolvedVersion(t *testing.T) {
	info := codexometerClientInfo()
	if info["name"] != "codexometer" || info["title"] != "Codexometer" || info["version"] != version.Current() {
		t.Fatalf("client info = %#v", info)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("CODEXOMETER_FAKE_APP_SERVER") == "1" {
		runFakeAppServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestFetchUsesAppServerHandshakeAndDecodesSnapshot(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := (Client{Binary: executable}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	meters := snapshot.Meters()
	if len(meters) != 3 || meters[0].Window.UsedPercent != 42 || meters[1].Window.UsedPercent != 5 ||
		meters[2].Kind != MeterIndividualLimit || meters[2].Window.UsedPercent != 32 {
		t.Fatalf("unexpected fetched meters: %#v", meters)
	}
	if snapshot.RateLimits.SpendControlReached == nil || *snapshot.RateLimits.SpendControlReached {
		t.Fatalf("spend-control state was not decoded: %#v", snapshot.RateLimits)
	}
	if credits, ok := snapshot.CreditStatus(); !ok || credits.Balance == nil || *credits.Balance != "17000" {
		t.Fatalf("credit status was not decoded: %#v, %v", credits, ok)
	}
	if snapshot.FetchedAt.IsZero() {
		t.Fatal("fetch time was not recorded")
	}
	if snapshot.AccountFingerprint == "" || strings.Contains(snapshot.AccountFingerprint, "user@example.com") {
		t.Fatalf("account fingerprint was not anonymized: %q", snapshot.AccountFingerprint)
	}
}

func TestFetchReportsRPCError(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	t.Setenv("CODEXOMETER_FAKE_RPC_ERROR", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	_, err = (Client{Binary: executable}).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "login required (RPC -32000)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchToleratesUnavailableAccountIdentity(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	t.Setenv("CODEXOMETER_FAKE_ACCOUNT_ERROR", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (Client{Binary: executable}).Fetch(context.Background())
	if err != nil || len(snapshot.Meters()) != 3 || snapshot.AccountFingerprint != "" {
		t.Fatalf("optional account identity changed quota fetch: %#v, %v", snapshot, err)
	}
}

func TestFetchReportsMissingCodexBinary(t *testing.T) {
	_, err := (Client{Binary: "/definitely/missing/codex"}).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Codex CLI not found") {
		t.Fatalf("unexpected missing binary error: %v", err)
	}
}

func TestResponseForSkipsNotifications(t *testing.T) {
	input := strings.NewReader("{\"method\":\"notice\"}\n{\"id\":7,\"result\":{\"ok\":true}}\n")
	result, err := responseFor(json.NewDecoder(input), 7)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestResponseForReportsUnexpectedEOF(t *testing.T) {
	_, err := responseFor(json.NewDecoder(strings.NewReader("")), 1)
	if err == nil || !strings.Contains(err.Error(), "closed unexpectedly") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithServerErrorIncludesUsefulStderr(t *testing.T) {
	err := withServerError("read quotas", fmt.Errorf("boom"), "  server detail  \n")
	if err.Error() != "read quotas: boom: server detail" {
		t.Fatalf("unexpected error: %q", err)
	}
	err = withServerError("read quotas", fmt.Errorf("boom"), "")
	if err.Error() != "read quotas: boom" {
		t.Fatalf("unexpected error without stderr: %q", err)
	}
}

func runFakeAppServer() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		switch request.Method {
		case "account/rateLimitResetCredit/consume":
			switch os.Getenv("CODEXOMETER_FAKE_RESET_ERROR") {
			case "rpc":
				fmt.Fprintln(os.Stderr, "reset service unavailable")
				_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32000, "message": "reset rejected"}})
				continue
			case "decode":
				_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{"outcome": 123}})
				continue
			}
			var params struct {
				Key string `json:"idempotencyKey"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Key != "test-attempt" {
				_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32602, "message": "invalid attempt"}})
				continue
			}
			outcome := os.Getenv("CODEXOMETER_FAKE_RESET_OUTCOME")
			_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{"outcome": outcome}})
		case "initialize":
			if os.Getenv("CODEXOMETER_FAKE_EXPECT_BENCHMARK_LOGIN") == "1" {
				args := strings.Join(os.Args, " ")
				if !strings.Contains(args, `cli_auth_credentials_store="ephemeral"`) || os.Getenv("CODEX_HOME") == "" ||
					os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEXOMETER_BENCHMARK_API_KEY") != "" || os.Getenv("DIGBENCH_API_TOKEN") != "" {
					_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32000, "message": "benchmark auth was not isolated"}})
					continue
				}
			}
			_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{}})
		case "account/login/start":
			var params struct {
				Type   string `json:"type"`
				APIKey string `json:"apiKey"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Type != "apiKey" || params.APIKey != "benchmark-secret" {
				_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32602, "message": "invalid benchmark login"}})
				continue
			}
			_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{"type": "apiKey"}})
		case "account/read":
			if os.Getenv("CODEXOMETER_FAKE_ACCOUNT_ERROR") == "1" {
				_ = encoder.Encode(map[string]any{
					"id": *request.ID, "error": map[string]any{"code": -32601, "message": "unknown method"},
				})
				continue
			}
			_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{
				"account":            map[string]any{"type": "chatgpt", "email": "user@example.com", "planType": "pro"},
				"requiresOpenaiAuth": true,
			}})
		case "account/rateLimits/read":
			if os.Getenv("CODEXOMETER_FAKE_RPC_ERROR") == "1" {
				_ = encoder.Encode(map[string]any{
					"id":    *request.ID,
					"error": map[string]any{"code": -32000, "message": "login required"},
				})
				continue
			}
			_ = encoder.Encode(map[string]any{
				"id": *request.ID,
				"result": map[string]any{
					"rateLimits": map[string]any{
						"primary":             map[string]any{"usedPercent": 42, "windowDurationMins": 300},
						"secondary":           map[string]any{"usedPercent": 5, "windowDurationMins": 10_080},
						"individualLimit":     map[string]any{"limit": "25000", "used": "8000", "remainingPercent": 68, "resetsAt": 1_800_000_000},
						"spendControlReached": false,
						"credits":             map[string]any{"hasCredits": true, "unlimited": false, "balance": "17000"},
					},
				},
			})
		}
	}
}

func TestConsumeResetAccountBindingAndOutcomes(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Client{Binary: executable}
	snapshot, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"reset", "alreadyRedeemed", "nothingToReset", "noCredit"} {
		t.Setenv("CODEXOMETER_FAKE_RESET_OUTCOME", outcome)
		got, err := c.ConsumeReset(context.Background(), "test-attempt", snapshot.AccountFingerprint)
		if err != nil || got != outcome {
			t.Fatalf("%s: %q %v", outcome, got, err)
		}
	}
	if _, err := c.ConsumeReset(context.Background(), "test-attempt", "different-account"); err == nil {
		t.Fatal("account mismatch accepted")
	}
	if _, err := c.ConsumeReset(context.Background(), "", snapshot.AccountFingerprint); err == nil {
		t.Fatal("empty key accepted")
	}
	t.Setenv("CODEXOMETER_FAKE_RESET_OUTCOME", "future-outcome")
	if _, err := c.ConsumeReset(context.Background(), "test-attempt", snapshot.AccountFingerprint); err == nil {
		t.Fatal("unknown outcome accepted")
	}
	t.Setenv("CODEXOMETER_FAKE_ACCOUNT_ERROR", "1")
	if _, err := c.ConsumeReset(context.Background(), "test-attempt", snapshot.AccountFingerprint); err == nil {
		t.Fatal("unverified account accepted")
	}
}

func TestConsumeResetErrorsHaveContext(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Client{Binary: executable}
	snapshot, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ mode, want string }{
		{"rpc", "read Codex quota reset response"}, {"decode", "decode Codex quota reset response"},
	} {
		t.Setenv("CODEXOMETER_FAKE_RESET_ERROR", test.mode)
		_, err := c.ConsumeReset(context.Background(), "test-attempt", snapshot.AccountFingerprint)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", test.mode, err)
		}
		if test.mode == "rpc" && !strings.Contains(err.Error(), "reset rejected") {
			t.Fatalf("RPC cause lost: %v", err)
		}
	}
}
