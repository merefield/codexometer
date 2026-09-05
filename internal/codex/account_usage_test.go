package codex

import (
	"context"
	"os"
	"strings"
	"testing"
)

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
	if _, err := (Client{Binary: exe}).FetchAccountUsage(context.Background()); err == nil {
		t.Fatal("unverified account accepted")
	}
}
