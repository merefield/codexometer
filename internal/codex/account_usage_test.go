package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountUsageIgnoresLegacyLedgerAndDoesNotPersist(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	directory := filepath.Join(config, "codexometer")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "usage-history.json")
	const legacy = `{"version":2,"accounts":{"obsolete":{"summary":{"lifetimeTokens":999999}}}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := Client{Binary: exe}
	first, err := client.FetchAccountUsage(context.Background())
	if err != nil || first.Summary.LifetimeTokens == nil || *first.Summary.LifetimeTokens != 123456 {
		t.Fatalf("backend result not used: %v", err)
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_RESULT", `{"summary":{},"dailyUsageBuckets":[]}`)
	next, err := client.FetchAccountUsage(context.Background())
	if err != nil || len(next.DailyUsageBuckets) != 0 || next.Summary.LifetimeTokens != nil {
		t.Fatal("retained old usage instead of replacing with official response")
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_ERROR", "1")
	if _, err := client.FetchAccountUsage(context.Background()); err == nil {
		t.Fatal("failure unexpectedly recovered from disk")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != legacy {
		t.Fatal("legacy ledger modified")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal("created additional usage cache/lock files")
	}
}

func TestFetchAccountUsage(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := Client{Binary: exe}
	data, err := client.FetchAccountUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data.AccountFingerprint == "" || data.FetchedAt.IsZero() || data.Summary.LifetimeTokens == nil || *data.Summary.LifetimeTokens != 123456 || len(data.DailyUsageBuckets) != 1 || data.DailyUsageBuckets[0].Tokens != 3000 {
		t.Fatalf("unexpected usage: %+v", data)
	}
	for _, test := range []struct {
		json      string
		available bool
	}{
		{`{"summary":{}}`, false},
		{`{"summary":{},"dailyUsageBuckets":null}`, false},
		{`{"summary":{},"dailyUsageBuckets":[]}`, true},
	} {
		t.Setenv("CODEXOMETER_FAKE_USAGE_RESULT", test.json)
		data, err := client.FetchAccountUsage(context.Background())
		if err != nil || (data.DailyUsageBuckets != nil) != test.available || data.Summary.LifetimeTokens != nil {
			t.Fatalf("optional data = %+v, %v", data, err)
		}
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_RESULT", `{"dailyUsageBuckets":"bad"}`)
	if _, err := client.FetchAccountUsage(context.Background()); err == nil {
		t.Fatal("malformed buckets accepted")
	}
	t.Setenv("CODEXOMETER_FAKE_USAGE_ERROR", "1")
	if _, err := client.FetchAccountUsage(context.Background()); err == nil || !strings.Contains(err.Error(), "Method not found") {
		t.Fatalf("unsupported API: %v", err)
	}
}

func TestAccountUsageRequiresVerifiedAccount(t *testing.T) {
	t.Setenv("CODEXOMETER_FAKE_APP_SERVER", "1")
	t.Setenv("CODEXOMETER_FAKE_ACCOUNT_ERROR", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := (Client{Binary: exe}).FetchAccountUsage(context.Background())
	if err == nil {
		t.Fatal("unverified account accepted")
	}
	for _, detail := range []string{"read Codex account identity for usage", "unknown method", "RPC -32601"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("account error %q lost detail %q", err, detail)
		}
	}
	if data.DailyUsageBuckets != nil || !data.FetchedAt.IsZero() {
		t.Fatal("usage fetched despite failed account verification")
	}
}
