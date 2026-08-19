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
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		switch request.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{}})
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
