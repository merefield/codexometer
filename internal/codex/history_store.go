package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	historySchemaVersion = 1
	historyRetentionDays = 400
	maxQuotaObservations = 12_000
)

// HistoryStore is a versioned, content-free local ledger. It stores numeric
// account usage and quota observations, never prompts, replies, commands,
// paths, email addresses, or authentication material.
type HistoryStore struct {
	Path          string
	mu            sync.Mutex
	activeAccount string // process-local: a persisted cache never selects identity
}

type historyFile struct {
	Version  int                        `json:"version"`
	Accounts map[string]*historyAccount `json:"accounts"`
}

type historyAccount struct {
	Summary      AccountUsageSummary       `json:"summary"`
	FetchedAt    time.Time                 `json:"fetchedAt"`
	BucketsKnown bool                      `json:"bucketsKnown,omitempty"`
	Days         map[string]historyDay     `json:"days"`
	Quota        []historyQuotaObservation `json:"quota,omitempty"`
}

type historyDay struct {
	OpenAITokens *int64            `json:"openaiTokens,omitempty"`
	Local        RecoveredUsageDay `json:"local,omitempty"`
}

type historyQuotaObservation struct {
	At       time.Time `json:"at"`
	LimitID  string    `json:"limitId"`
	Window   int       `json:"window"`
	Used     int       `json:"used"`
	Duration *int64    `json:"duration,omitempty"`
	Reset    *int64    `json:"reset,omitempty"`
}

// NewDefaultHistoryStore follows the same cross-platform application-data
// convention as presentation preferences while keeping history in its own
// independently migratable file.
func NewDefaultHistoryStore() (*HistoryStore, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &HistoryStore{Path: filepath.Join(directory, "codexometer", "usage-history.json")}, nil
}

func (s *HistoryStore) Reconcile(remote AccountUsage, recovered []RecoveredUsageDay) (AccountUsage, error) {
	if s == nil || remote.AccountFingerprint == "" {
		return remote, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result AccountUsage
	err := withHistoryFileLock(s.Path+".lock", func() error {
		file, err := s.load()
		if err != nil {
			return err
		}
		account := file.account(remote.AccountFingerprint)
		mergeSummary(&account.Summary, remote.Summary)
		if !remote.FetchedAt.IsZero() {
			account.FetchedAt = remote.FetchedAt
		}
		if remote.DailyUsageBuckets != nil {
			account.BucketsKnown = true
		}
		for _, day := range remote.DailyUsageBuckets {
			if !validUsageDate(day.StartDate) || day.Tokens < 0 {
				continue
			}
			value := day.Tokens
			stored := account.Days[day.StartDate]
			stored.OpenAITokens = &value
			account.Days[day.StartDate] = stored
		}
		// Recovery is a complete bounded rescan, so replace local aggregates in
		// that range instead of incrementing them and risking duplicate imports.
		if recovered != nil {
			if len(recovered) > 0 {
				account.BucketsKnown = true
			}
			for date, day := range account.Days {
				day.Local = RecoveredUsageDay{}
				account.Days[date] = day
			}
			for _, day := range recovered {
				if !validUsageDate(day.StartDate) || day.TotalTokens < 0 {
					continue
				}
				stored := account.Days[day.StartDate]
				stored.Local = day
				account.Days[day.StartDate] = stored
			}
		}
		pruneHistory(account, time.Now().UTC())
		if err := s.save(file); err != nil {
			return err
		}
		result = renderAccountUsage(remote.AccountFingerprint, account, false)
		s.activeAccount = remote.AccountFingerprint
		return nil
	})
	if err != nil {
		return remote, fmt.Errorf("persist usage history: %w", err)
	}
	return result, nil
}

// RecordQuota stores current snapshots for later quota-history visualisations.
// Consecutive identical observations are collapsed.
func (s *HistoryStore) RecordQuota(snapshot Snapshot) error {
	if s == nil || snapshot.AccountFingerprint == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return withHistoryFileLock(s.Path+".lock", func() error {
		file, err := s.load()
		if err != nil {
			return err
		}
		account := file.account(snapshot.AccountFingerprint)
		at := snapshot.FetchedAt
		if at.IsZero() {
			at = time.Now()
		}
		windows := map[string]int{}
		for _, meter := range snapshot.Meters() {
			if meter.Kind != MeterQuotaWindow {
				continue
			}
			window := windows[meter.LimitID]
			bucket := snapshot.RateLimits
			if len(snapshot.RateLimitsByLimitID) > 0 {
				bucket = snapshot.RateLimitsByLimitID[meter.LimitID]
			}
			if bucket.Primary == nil {
				window++
			}
			windows[meter.LimitID]++
			observation := historyQuotaObservation{At: at.UTC(), LimitID: meter.LimitID, Window: window, Used: meter.Window.UsedPercent, Duration: meter.Window.WindowDurationMins, Reset: meter.Window.ResetsAt}
			latest := -1
			for i := len(account.Quota) - 1; i >= 0; i-- {
				if account.Quota[i].LimitID == observation.LimitID && account.Quota[i].Window == observation.Window {
					latest = i
					break
				}
			}
			if latest >= 0 && sameQuotaObservation(account.Quota[latest], observation) {
				account.Quota[latest].At = observation.At
			} else {
				account.Quota = append(account.Quota, observation)
			}
		}
		if len(account.Quota) > maxQuotaObservations {
			account.Quota = append([]historyQuotaObservation(nil), account.Quota[len(account.Quota)-maxQuotaObservations:]...)
		}
		if err := s.save(file); err != nil {
			return err
		}
		s.activeAccount = snapshot.AccountFingerprint
		return nil
	})
}

func (s *HistoryStore) Latest() (AccountUsage, error) {
	if s == nil {
		return AccountUsage{}, os.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result AccountUsage
	err := withHistoryFileLock(s.Path+".lock", func() error {
		if s.activeAccount == "" {
			return os.ErrNotExist
		}
		file, err := s.load()
		if err != nil {
			return err
		}
		account := file.Accounts[s.activeAccount]
		if account == nil {
			return os.ErrNotExist
		}
		result = renderAccountUsage(s.activeAccount, account, true)
		return nil
	})
	return result, err
}

func (f *historyFile) account(id string) *historyAccount {
	if f.Accounts == nil {
		f.Accounts = map[string]*historyAccount{}
	}
	account := f.Accounts[id]
	if account == nil {
		account = &historyAccount{Days: map[string]historyDay{}}
		f.Accounts[id] = account
	}
	if account.Days == nil {
		account.Days = map[string]historyDay{}
	}
	return account
}

func (s *HistoryStore) load() (*historyFile, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &historyFile{Version: historySchemaVersion, Accounts: map[string]*historyAccount{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var file historyFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Version != historySchemaVersion {
		return nil, fmt.Errorf("unsupported usage history schema %d", file.Version)
	}
	if file.Accounts == nil {
		file.Accounts = map[string]*historyAccount{}
	}
	return &file, nil
}

func (s *HistoryStore) save(file *historyFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".usage-history-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceHistoryFile(name, s.Path); err != nil {
		return err
	}
	return syncHistoryDirectory(directory)
}

func renderAccountUsage(id string, account *historyAccount, stale bool) AccountUsage {
	dates := make([]string, 0, len(account.Days))
	for date, day := range account.Days {
		if day.OpenAITokens != nil || day.Local.TotalTokens > 0 {
			dates = append(dates, date)
		}
	}
	sort.Strings(dates)
	result := AccountUsage{Summary: account.Summary, AccountFingerprint: id, FetchedAt: account.FetchedAt, Persisted: true, Stale: stale, DailyUsageBuckets: []AccountUsageDay{}}
	if !account.BucketsKnown {
		result.DailyUsageBuckets = nil
	}
	var matchedLocal int64
	for _, date := range dates {
		stored := account.Days[date]
		day := AccountUsageDay{StartDate: date, LocalTokens: stored.Local.TotalTokens, InputTokens: stored.Local.InputTokens, CachedInputTokens: stored.Local.CachedInputTokens, OutputTokens: stored.Local.OutputTokens, ReasoningTokens: stored.Local.ReasoningTokens}
		if stored.OpenAITokens != nil {
			day.Tokens = *stored.OpenAITokens
			day.Provenance = "OPENAI"
			result.Coverage.OpenAITokens = saturatingAdd(result.Coverage.OpenAITokens, day.Tokens)
			result.Coverage.OpenAIDays++
			matchedLocal = saturatingAdd(matchedLocal, day.LocalTokens)
		} else {
			day.Tokens = stored.Local.TotalTokens
			day.Provenance = "RECOVERED"
			result.Coverage.RecoveredDays++
		}
		result.Coverage.LocalTokens = saturatingAdd(result.Coverage.LocalTokens, day.LocalTokens)
		result.DailyUsageBuckets = append(result.DailyUsageBuckets, day)
	}
	switch {
	case result.Coverage.OpenAIDays > 0 && result.Coverage.RecoveredDays > 0:
		result.Coverage.Status = "PARTIAL"
	case result.Coverage.OpenAIDays > 0:
		result.Coverage.Status = "OPENAI"
	case result.Coverage.LocalTokens > 0:
		result.Coverage.Status = "RECOVERED"
	default:
		result.Coverage.Status = "EMPTY"
	}
	if result.Coverage.OpenAITokens > 0 {
		result.Coverage.AttributedPct = min(100, int(math.Round(100*float64(matchedLocal)/float64(result.Coverage.OpenAITokens))))
	}
	return result
}

func mergeSummary(current *AccountUsageSummary, incoming AccountUsageSummary) {
	if incoming.LifetimeTokens != nil {
		current.LifetimeTokens = incoming.LifetimeTokens
	}
	if incoming.PeakDailyTokens != nil {
		current.PeakDailyTokens = incoming.PeakDailyTokens
	}
	if incoming.LongestRunningTurnSec != nil {
		current.LongestRunningTurnSec = incoming.LongestRunningTurnSec
	}
	if incoming.CurrentStreakDays != nil {
		current.CurrentStreakDays = incoming.CurrentStreakDays
	}
	if incoming.LongestStreakDays != nil {
		current.LongestStreakDays = incoming.LongestStreakDays
	}
}

func validUsageDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func pruneHistory(account *historyAccount, now time.Time) {
	cutoff := now.AddDate(0, 0, -historyRetentionDays).Format("2006-01-02")
	for date := range account.Days {
		if date < cutoff {
			delete(account.Days, date)
		}
	}
}

func sameQuotaObservation(a, b historyQuotaObservation) bool {
	return a.LimitID == b.LimitID && a.Window == b.Window && a.Used == b.Used && equalInt64Ptr(a.Duration, b.Duration) && equalInt64Ptr(a.Reset, b.Reset)
}

func equalInt64Ptr(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func saturatingAdd(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
