package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Preferences contains presentation choices and anonymous aggregate quota
// evidence that survive between runs. Raw account, session, and token-event
// data are never stored.
type Preferences struct {
	Theme            string           `json:"theme,omitempty"`
	QuotaView        string           `json:"quotaView,omitempty"`
	BenchmarkFilter  string           `json:"benchmarkFilter,omitempty"`
	BenchmarkRank    string           `json:"benchmarkRank,omitempty"`
	QuotaAPIEvidence []quotaAPISample `json:"quotaApiEvidence,omitempty"`
}

type PreferenceStore interface {
	Load() (Preferences, error)
	Save(Preferences) error
}

type FilePreferenceStore struct {
	Path string
}

func NewDefaultPreferenceStore() (FilePreferenceStore, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return FilePreferenceStore{}, err
	}
	return FilePreferenceStore{Path: filepath.Join(directory, "codexometer", "preferences.json")}, nil
}

func (s FilePreferenceStore) Load() (Preferences, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, err
	}
	var preferences Preferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return Preferences{}, err
	}
	return preferences, nil
}

func (s FilePreferenceStore) Save(preferences Preferences) error {
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(data, '\n'), 0o600)
}

func NewWithPreferences(fetcher Fetcher, refreshEvery time.Duration, store PreferenceStore) Model {
	model := New(fetcher, refreshEvery)
	model.preferenceStore = store
	if store == nil {
		return model
	}
	preferences, err := store.Load()
	if err == nil {
		model.applyPreferences(preferences)
	}
	return model
}

func (m *Model) applyPreferences(preferences Preferences) {
	if theme, ok := themePreferenceIDs[preferences.Theme]; ok {
		m.theme = theme
	}
	if style, ok := quotaViewPreferenceIDs[preferences.QuotaView]; ok {
		m.meterStyle = style
		m.quotaMeterStyle = style
	}
	if filter, ok := benchmarkFilterPreferenceIDs[preferences.BenchmarkFilter]; ok {
		m.benchmarkFilter = filter
	}
	if rank, ok := benchmarkRankPreferenceIDs[preferences.BenchmarkRank]; ok {
		m.benchmarkRankMode = rank
	}
	m.quotaAPIEvidence = validQuotaAPISamples(preferences.QuotaAPIEvidence, time.Now())
}

func (m Model) persistPreferences() {
	if m.preferenceStore == nil {
		return
	}
	_ = m.preferenceStore.Save(Preferences{
		Theme:            themePreferenceNames[m.theme],
		QuotaView:        quotaViewPreferenceNames[m.selectedQuotaStyle()],
		BenchmarkFilter:  benchmarkFilterPreferenceNames[m.benchmarkFilter],
		BenchmarkRank:    benchmarkRankPreferenceNames[m.benchmarkRankMode],
		QuotaAPIEvidence: append([]quotaAPISample(nil), m.quotaAPIEvidence...),
	})
}

var themePreferenceNames = map[themeID]string{
	themeHacker: "hacker", themeRust: "rust", themeBlueSteel: "blue-steel",
	themeUltraviolet: "ultraviolet", themeNightshade: "nightshade",
}

var themePreferenceIDs = reverseThemePreferences(themePreferenceNames)

func reverseThemePreferences(values map[themeID]string) map[string]themeID {
	reversed := make(map[string]themeID, len(values))
	for id, value := range values {
		reversed[value] = id
	}
	return reversed
}

var quotaViewPreferenceNames = map[meterStyleID]string{
	styleBars: "bars", stylePie: "pie", styleConsumptionPace: "consumption-pace", styleFuel: "fuel-tank",
}

var quotaViewPreferenceIDs = reverseStylePreferences(quotaViewPreferenceNames)

func reverseStylePreferences(values map[meterStyleID]string) map[string]meterStyleID {
	reversed := make(map[string]meterStyleID, len(values))
	for id, value := range values {
		reversed[value] = id
	}
	return reversed
}

var benchmarkFilterPreferenceNames = map[benchmarkResultFilter]string{
	benchmarkFilterAll: "all", benchmarkFilterPass: "pass", benchmarkFilterFail: "fail",
}

var benchmarkFilterPreferenceIDs = reverseFilterPreferences(benchmarkFilterPreferenceNames)

func reverseFilterPreferences(values map[benchmarkResultFilter]string) map[string]benchmarkResultFilter {
	reversed := make(map[string]benchmarkResultFilter, len(values))
	for id, value := range values {
		reversed[value] = id
	}
	return reversed
}

var benchmarkRankPreferenceNames = map[benchmarkRankMode]string{
	benchmarkRankBalanced: "balanced", benchmarkRankCost: "cost", benchmarkRankSpeed: "speed",
}

var benchmarkRankPreferenceIDs = reverseRankPreferences(benchmarkRankPreferenceNames)

func reverseRankPreferences(values map[benchmarkRankMode]string) map[string]benchmarkRankMode {
	reversed := make(map[string]benchmarkRankMode, len(values))
	for id, value := range values {
		reversed[value] = id
	}
	return reversed
}
