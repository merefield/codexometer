package ui

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type memoryPreferenceStore struct {
	preferences Preferences
	loadErr     error
	saves       []Preferences
}

func (s *memoryPreferenceStore) Load() (Preferences, error) {
	return s.preferences, s.loadErr
}

func (s *memoryPreferenceStore) Save(preferences Preferences) error {
	s.preferences = preferences
	s.saves = append(s.saves, preferences)
	return nil
}

func TestPreferencesRestoreAndPersistPresentationChoices(t *testing.T) {
	store := &memoryPreferenceStore{preferences: Preferences{
		Theme: "nightshade", QuotaView: "fuel-tank", BenchmarkFilter: "fail", BenchmarkRank: "cost",
	}}
	model := NewWithPreferences(nil, time.Minute, store)
	if model.theme != themeNightshade || model.meterView != viewFuel || model.quotaMeterView != viewFuel ||
		model.benchmarkFilter != benchmarkFilterFail || model.benchmarkRankMode != benchmarkRankCost {
		t.Fatalf("preferences were not restored: %#v", model)
	}

	updated, _ := model.Update(key('t'))
	model = updated.(Model)
	updated, _ = model.Update(key('v'))
	model = updated.(Model)
	model.setBenchmarkFilter(benchmarkFilterPass)
	model.setBenchmarkRankMode(benchmarkRankSpeed)
	if len(store.saves) != 4 {
		t.Fatalf("preference saves = %d, want one for each presentation change", len(store.saves))
	}
	last := store.saves[len(store.saves)-1]
	if last.Theme != "hacker" || last.QuotaView != "bars" || last.BenchmarkFilter != "pass" || last.BenchmarkRank != "speed" {
		t.Fatalf("persisted preferences = %#v", last)
	}
}

func TestInvalidOrUnreadablePreferencesKeepSafeDefaults(t *testing.T) {
	for _, store := range []*memoryPreferenceStore{
		{preferences: Preferences{Theme: "missing", QuotaView: "rotary", BenchmarkFilter: "maybe", BenchmarkRank: "tokens"}},
		{loadErr: errors.New("broken config")},
	} {
		model := NewWithPreferences(nil, time.Minute, store)
		if model.theme != themeHacker || model.meterView != viewBars || model.benchmarkFilter != benchmarkFilterAll ||
			model.benchmarkRankMode != benchmarkRankBalanced {
			t.Fatalf("invalid preferences changed defaults: %#v", model)
		}
	}
}

func TestFilePreferenceStoreRoundTripAndMissingFile(t *testing.T) {
	store := FilePreferenceStore{Path: filepath.Join(t.TempDir(), "nested", "preferences.json")}
	if preferences, err := store.Load(); err != nil || !reflect.DeepEqual(preferences, Preferences{}) {
		t.Fatalf("missing preference file = %#v, %v", preferences, err)
	}
	want := Preferences{Theme: "rust", QuotaView: "pie", BenchmarkFilter: "pass", BenchmarkRank: "balanced"}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("preference round trip = %#v, %v; want %#v", got, err, want)
	}
}
