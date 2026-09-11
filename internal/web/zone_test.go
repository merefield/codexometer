package web

import (
	"errors"
	"testing"
	"time"

	"github.com/merefield/codexometer/internal/codex"
)

func TestZoneTrailBoundariesAndBound(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	duration, reset := int64(100), now.Add(time.Hour).Unix()
	m := meter{Name: "weekly", Used: 10, Duration: &duration, Reset: &reset}
	m.Trail = observeZone(m, nil, now, false)
	if len(m.Trail) != 1 || m.Trail[0].Elapsed != 40 {
		t.Fatalf("initial point: %+v", m.Trail)
	}
	first := m.Trail[0]
	if observeZone(m, nil, now.Add(-2*time.Hour), false)[0].Elapsed != 0 || observeZone(m, nil, now.Add(2*time.Hour), false)[0].Elapsed != 100 {
		t.Fatal("elapsed position not bounded")
	}
	for i := 1; i < maxZonePoints+10; i++ {
		m.Trail = observeZone(m, []meter{m}, now.Add(time.Duration(i)*time.Second), false)
	}
	if len(m.Trail) != maxZonePoints || m.Trail[0] != first || !m.Trail[1].Break {
		t.Fatal("trail lost start, exceeded bound or bridged removed points")
	}
	for name, edit := range map[string]func(*meter){
		"reset":            func(m *meter) { r := reset + 100; m.Reset = &r },
		"duration":         func(m *meter) { d := duration + 1; m.Duration = &d },
		"counter decrease": func(m *meter) { m.Used = 9 },
		"different window": func(m *meter) { m.Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			next := m
			edit(&next)
			if len(observeZone(next, []meter{m}, now.Add(time.Hour), false)) != 1 {
				t.Fatal("old cycle retained")
			}
		})
	}
	if len(observeZone(m, []meter{m}, now, false)) != 1 {
		t.Fatal("backwards time retained")
	}
	for _, d := range []*int64{nil, new(int64)} {
		missing := m
		missing.Duration = d
		if observeZone(missing, nil, now, false) != nil {
			t.Fatal("guessed missing duration")
		}
	}
	missing := m
	missing.Reset = nil
	if observeZone(missing, nil, now, false) != nil {
		t.Fatal("guessed reset")
	}
	points := observeZone(m, []meter{m}, now.Add(time.Hour), true)
	if !points[len(points)-1].Break {
		t.Fatal("bridged failed observation")
	}
}

func TestZoneTrailAccountAndRefreshIsolation(t *testing.T) {
	s := newStore()
	now := time.Now()
	q := codex.DemoSnapshot()
	q.AccountFingerprint = "A"
	s.quota(q, nil, now)
	s.quota(q, nil, now.Add(time.Minute))
	if len(s.state.Meters[0].Trail) != 2 {
		t.Fatal("not collecting between browser visits")
	}
	s.quota(q, errors.New("failed"), now.Add(2*time.Minute))
	if len(s.state.Meters[0].Trail) != 2 {
		t.Fatal("failure added fabricated point")
	}
	s.quota(q, nil, now.Add(3*time.Minute))
	if len(s.state.Meters[0].Trail) != 3 || !s.state.Meters[0].Trail[2].Break {
		t.Fatal("failure gap lost")
	}
	q.AccountFingerprint = "B"
	s.quota(q, nil, now.Add(4*time.Minute))
	if len(s.state.Meters[0].Trail) != 1 {
		t.Fatal("account trails mixed")
	}
	s.quota(codex.Snapshot{AccountFingerprint: "B"}, nil, now.Add(5*time.Minute))
	s.quota(q, nil, now.Add(6*time.Minute))
	if len(s.state.Meters[0].Trail) != 1 {
		t.Fatal("removed window retained")
	}
	if len(newStore().state.Meters) != 0 {
		t.Fatal("trail persisted across process")
	}
}
