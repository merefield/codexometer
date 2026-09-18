package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryStoreReconcilesPersistsAndIsolatesAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage-history.json")
	store := &HistoryStore{Path: path}
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	lifetime, peak, turn, current, longest := int64(1_000), int64(100), int64(45), int64(2), int64(9)
	remote := AccountUsage{
		AccountFingerprint: "account-a", FetchedAt: now,
		Summary:           AccountUsageSummary{LifetimeTokens: &lifetime, PeakDailyTokens: &peak, LongestRunningTurnSec: &turn, CurrentStreakDays: &current, LongestStreakDays: &longest},
		DailyUsageBuckets: []AccountUsageDay{{StartDate: "2026-09-17", Tokens: 100}},
	}
	recovered := []RecoveredUsageDay{
		{StartDate: "2026-09-16", TotalTokens: 5, InputTokens: 4, OutputTokens: 1},
		{StartDate: "2026-09-17", TotalTokens: 70, InputTokens: 60, CachedInputTokens: 30, OutputTokens: 10},
	}
	got, err := store.Reconcile(remote, recovered)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Persisted || got.Stale || got.Coverage.Status != "PARTIAL" || got.Coverage.AttributedPct != 70 || got.Coverage.RecoveredDays != 1 || len(got.DailyUsageBuckets) != 2 {
		t.Fatalf("reconciled usage = %+v", got)
	}
	if got.DailyUsageBuckets[0].Provenance != "RECOVERED" || got.DailyUsageBuckets[1].Tokens != 100 || got.DailyUsageBuckets[1].LocalTokens != 70 {
		t.Fatalf("daily provenance = %+v", got.DailyUsageBuckets)
	}

	reopened := &HistoryStore{Path: path}
	verified := DemoSnapshot()
	verified.AccountFingerprint = "account-a"
	verified.FetchedAt = now.Add(time.Minute)
	if err := reopened.RecordQuota(verified); err != nil {
		t.Fatal(err)
	}
	cached, err := reopened.Latest()
	if err != nil || !cached.Stale || cached.AccountFingerprint != "account-a" || cached.Summary.LongestStreakDays == nil || *cached.Summary.LongestStreakDays != 9 {
		t.Fatalf("reopened cache = %+v, %v", cached, err)
	}
	other := remote
	other.AccountFingerprint = "account-b"
	other.DailyUsageBuckets = []AccountUsageDay{{StartDate: "2026-09-18", Tokens: 3}}
	if _, err := reopened.Reconcile(other, []RecoveredUsageDay{}); err != nil {
		t.Fatal(err)
	}
	latest, _ := reopened.Latest()
	if latest.AccountFingerprint != "account-b" || len(latest.DailyUsageBuckets) != 1 {
		t.Fatalf("latest account was mixed: %+v", latest)
	}
	data, err := os.ReadFile(path)
	if err != nil || os.FileMode(0o077)&fileMode(t, path) != 0 {
		t.Fatalf("history permissions/read = %v, %v", fileMode(t, path), err)
	}
	if len(data) == 0 {
		t.Fatal("empty history file")
	}
	if _, err := (&HistoryStore{Path: path}).Latest(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified restart selected a cached account: %v", err)
	}
}

func TestHistoryStoreDistinguishesUnavailableAndEmptyBuckets(t *testing.T) {
	store := &HistoryStore{Path: filepath.Join(t.TempDir(), "history.json")}
	missing, err := store.Reconcile(AccountUsage{AccountFingerprint: "missing", FetchedAt: time.Now()}, nil)
	if err != nil || missing.DailyUsageBuckets != nil {
		t.Fatalf("missing buckets became available: %+v, %v", missing, err)
	}
	empty, err := store.Reconcile(AccountUsage{AccountFingerprint: "empty", FetchedAt: time.Now(), DailyUsageBuckets: []AccountUsageDay{}}, []RecoveredUsageDay{})
	if err != nil || empty.DailyUsageBuckets == nil || len(empty.DailyUsageBuckets) != 0 {
		t.Fatalf("empty buckets became unavailable: %+v, %v", empty, err)
	}
}

func TestHistoryStoreQuotaObservationsCollapsePerWindow(t *testing.T) {
	store := &HistoryStore{Path: filepath.Join(t.TempDir(), "history.json")}
	snapshot := DemoSnapshot()
	snapshot.AccountFingerprint = "account"
	snapshot.FetchedAt = time.Now()
	if err := store.RecordQuota(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.FetchedAt = snapshot.FetchedAt.Add(time.Minute)
	if err := store.RecordQuota(snapshot); err != nil {
		t.Fatal(err)
	}
	file, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(file.Accounts["account"].Quota); got != 2 {
		t.Fatalf("identical two-window snapshot produced %d observations", got)
	}
	snapshot.RateLimits.Primary.UsedPercent++
	if err := store.RecordQuota(snapshot); err != nil {
		t.Fatal(err)
	}
	file, _ = store.load()
	if got := len(file.Accounts["account"].Quota); got != 3 {
		t.Fatalf("changed window was not appended: %d", got)
	}
	snapshot.RateLimits.Primary = nil
	if err := store.RecordQuota(snapshot); err != nil {
		t.Fatal(err)
	}
	file, _ = store.load()
	if got := len(file.Accounts["account"].Quota); got != 3 {
		t.Fatalf("secondary window changed identity when primary disappeared: %d", got)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
