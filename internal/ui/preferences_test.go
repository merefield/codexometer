package ui

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
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
	if last.Theme != "hacker" || last.QuotaView != "resets" || last.BenchmarkFilter != "pass" || last.BenchmarkRank != "speed" {
		t.Fatalf("persisted preferences = %#v", last)
	}
}

func TestInvalidOrUnreadablePreferencesKeepSafeDefaults(t *testing.T) {
	for _, store := range []*memoryPreferenceStore{
		{preferences: Preferences{MainTab: "missing", Theme: "missing", QuotaView: "rotary", BenchmarkFilter: "maybe", BenchmarkRank: "tokens"}},
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
	want := Preferences{MainTab: "monitor", Theme: "rust", QuotaView: "pie", BenchmarkFilter: "pass", BenchmarkRank: "balanced"}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("preference round trip = %#v, %v; want %#v", got, err, want)
	}
}

func TestPreferencesRememberMainTabSeparatelyFromQuotaView(t *testing.T) {
	for tab, name := range mainTabPreferenceNames {
		t.Run(name, func(t *testing.T) {
			store := &memoryPreferenceStore{preferences: Preferences{QuotaView: "fuel-tank"}}
			m := NewWithPreferences(nil, time.Minute, store)
			next, _ := m.pressMainTab(tab)
			m = next.(Model)
			if store.preferences.MainTab != name || store.preferences.QuotaView != "fuel-tank" {
				t.Fatalf("saved preferences = %+v", store.preferences)
			}
			restarted := NewWithPreferences(nil, time.Minute, store)
			if restarted.currentMainTab() != tab || restarted.selectedQuotaView() != viewFuel {
				t.Fatal("restart lost tab or quota view")
			}
			if restarted.monitorContextDetail != "" || restarted.monitorApprovalConfirm != "" || restarted.benchmarkState != benchmarkIdle {
				t.Fatal("restart restored operational state")
			}
			next, _ = restarted.pressMainTab(mainTabQuota)
			if next.(Model).meterView != viewFuel {
				t.Fatal("return to quota lost remembered view")
			}
		})
	}
	for _, tab := range []string{"", "unknown"} {
		m := NewWithPreferences(nil, time.Minute, &memoryPreferenceStore{preferences: Preferences{MainTab: tab, QuotaView: "pie"}})
		if m.meterView != viewPie {
			t.Fatal("legacy/invalid tab did not fall back to saved quota view")
		}
	}
}

func TestRestoredTabsLoadDataWithoutStartingBenchmark(t *testing.T) {
	tasks := make(chan []codex.BenchmarkTaskID, 1)
	m := NewWithPreferences(benchmarkCaptureFetcher{tasks: tasks}, time.Minute, &memoryPreferenceStore{preferences: Preferences{MainTab: "benchmark"}})
	next, cmd := m.Update(initialViewMsg{})
	m = next.(Model)
	if cmd == nil || !m.benchmarkPlanning || m.benchmarkState != benchmarkIdle {
		t.Fatal("restored benchmark did not load its plan safely")
	}
	if _, ok := cmd().(benchmarkPlanMsg); !ok || len(tasks) != 0 {
		t.Fatal("restoring benchmark ran a task rather than loading its plan")
	}
	if _, duplicate := m.Update(initialViewMsg{}); duplicate != nil {
		t.Fatal("duplicated benchmark planning")
	}
	m = NewWithPreferences(historyStub{}, time.Minute, &memoryPreferenceStore{preferences: Preferences{MainTab: "usage"}})
	next, cmd = m.Update(initialViewMsg{})
	if cmd == nil || !next.(Model).history.loading {
		t.Fatal("restored usage tab did not request history")
	}
	if _, ok := cmd().(accountHistoryMsg); !ok {
		t.Fatal("wrong usage startup command")
	}
}
