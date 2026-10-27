package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCompressedRolloutActivityUsesOwnedEventTimeAndRestorationIsNotGrowth(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-72 * time.Hour)
	home := t.TempDir()
	path := testRolloutPath(t, home, old, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+
		attentionEventLine(now, "task_started", nil)+"\n"+responseItemLine(now, "reasoning")+"\n")
	plain := compressTestRollout(t, path, true)
	freezeRolloutTime(t, path+".zst", old)
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	u, err := r.FetchTokenUsage(context.Background())
	if err != nil || u.TotalTokens != 0 || u.SessionCount != 1 || !r.files[path].lastEventAt.Equal(now) {
		t.Fatal("compressed startup missed fresh events or counted history", u, err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	now = now.Add(fallbackAttentionAfter + time.Second)
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	freezeRolloutTime(t, path, old)
	u, err = r.FetchTokenUsageFresh(context.Background())
	if err != nil || u.TotalTokens != 0 || r.files[path].compressed || !r.files[path].lastGrowthAt.IsZero() {
		t.Fatal("identical restoration invented activity or tokens", u, err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionCheck, true)
	appendRollout(t, path, responseItemLine(now, "reasoning")+"\n")
	freezeRolloutTime(t, path, old)
	u, err = r.FetchTokenUsage(context.Background())
	if err != nil || u.TotalTokens != 0 {
		t.Fatal("fresh append failed or counted tokens", u, err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
}

func TestRolloutActivityCompressionPreservesGrowthAndPartialBaseline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-time.Hour)
	home := t.TempDir()
	path := testRolloutPath(t, home, now, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n"+attentionEventLine(old, "task_started", nil)+"\n")
	freezeRolloutTime(t, path, old)
	r, err := NewLiveUsageReader(home)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	r.statusProvider = stubSessionStatusProvider{unavailable: true}
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path, `{"timestamp":`)
	freezeRolloutTime(t, path, old)
	if _, err := r.FetchTokenUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	plain := compressTestRollout(t, path, true)
	freezeRolloutTime(t, path+".zst", old)
	if _, err := r.FetchTokenUsageFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.files[path].lastGrowthAt.Equal(now) || r.files[path].lastSize != int64(len(plain)) {
		t.Fatal("compression lost observed growth or partial bytes", r.files[path])
	}
	assertRolloutAttention(t, r, now, SessionAttentionNone, true)
	now = now.Add(fallbackAttentionAfter + time.Second)
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	freezeRolloutTime(t, path, old)
	if _, err := r.FetchTokenUsageFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolloutAttention(t, r, now, SessionAttentionCheck, true)
}

func TestActivityBootstrapBoundsOverlapAndStillReadsLaterEvents(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, suffix := range []string{"\n", "\n" + responseItemLine(now, "reasoning") + "\n"} {
		path := testRolloutPath(t, t.TempDir(), now, "huge")
		// The giant record advertises a timestamp but is too large to validate.
		// It must not establish freshness; a subsequent small valid record can.
		data := `{"timestamp":"` + now.Format(time.RFC3339) + `","type":"response_item","payload":"` +
			strings.Repeat("x", rolloutBaselineLineLimit+rolloutActivityTailBytes) + `"}` + suffix
		writeRollout(t, path, data)
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		target := int64(len(data) - rolloutActivityTailBytes)
		start, partial, err := rolloutActivityRecordStart(context.Background(), f, target)
		f.Close()
		if err != nil || !partial || start != target-rolloutBaselineLineLimit {
			t.Fatal("overlap scan exceeded its bound", start, partial, err)
		}
		c := &rolloutCursor{}
		if err := c.bootstrapActivity(context.Background(), path, int64(len(data)), now); err != nil {
			t.Fatal(err)
		}
		if suffix == "\n" && !c.lastEventAt.IsZero() || suffix != "\n" && !c.lastEventAt.Equal(now) {
			t.Fatal("oversized record was trusted or hid later event", c.lastEventAt)
		}
	}
}

func TestActivityBootstrapAndRestorationHonourCancellation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	path := testRolloutPath(t, t.TempDir(), now, "cancel")
	writeRollout(t, path, sessionMetaLine("s", `"cli"`, "/work", nil)+"\n"+responseItemLine(now, "reasoning")+"\n")
	plain := compressTestRollout(t, path, true)
	info, err := os.Stat(path + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	c, err := compressedRolloutBaseline(context.Background(), path+".zst", info, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.bootstrapActivity(ctx, path, info.Size(), now); !errors.Is(err, context.Canceled) {
		t.Fatal("bootstrap ignored cancellation", err)
	}
	r := &LiveUsageReader{}
	if err := r.restoreCompressedCursor(ctx, path, info, c); !errors.Is(err, context.Canceled) || !c.compressed {
		t.Fatal("restoration ignored cancellation or committed partial state", err)
	}
}

func TestCompressedRolloutActivityRejectsInheritedAndFutureEvents(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	boundary := uint64(10)
	for _, tc := range []struct {
		name    string
		ordinal uint64
		at      time.Time
		want    bool
	}{{"inherited", 9, now, false}, {"future", 10, now.Add(time.Hour), false}, {"owned", 10, now, true}} {
		t.Run(tc.name, func(t *testing.T) {
			path := testRolloutPath(t, t.TempDir(), now, "child")
			line := fmt.Sprintf(`{"timestamp":%q,"ordinal":%d,"type":"response_item","payload":{"type":"reasoning"}}`, tc.at.Format(time.RFC3339), tc.ordinal)
			writeRollout(t, path, sessionMetaLine("child", threadSpawnSource("parent"), "/work", &boundary)+"\n"+line+"\n")
			compressTestRollout(t, path, true)
			info, err := os.Stat(path + ".zst")
			if err != nil {
				t.Fatal(err)
			}
			c, err := compressedRolloutBaseline(context.Background(), path+".zst", info, now)
			if err != nil || tc.want && !c.lastEventAt.Equal(now) || !tc.want && !c.lastEventAt.IsZero() {
				t.Fatal("wrong compressed ownership/future-time decision", tc.name, c, err)
			}
		})
	}
}

func TestRebasedRolloutPreservesPreviouslyObservedGrowth(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	home := t.TempDir()
	path := testRolloutPath(t, home, now, "growing")
	writeRollout(t, path, sessionMetaLine("growing", `"cli"`, "/work", nil)+"\n")
	freezeRolloutTime(t, path, now.Add(-time.Hour))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	growth := now.Add(-time.Minute)
	r := &LiveUsageReader{initialized: true, startedAt: now.Add(-time.Hour), now: func() time.Time { return now }, files: map[string]*rolloutCursor{
		path: {threadID: "growing", lastGrowthAt: growth},
	}}
	if err := r.addFile(context.Background(), path, info); err != nil || !r.files[path].lastGrowthAt.Equal(growth) {
		t.Fatal("rebasing discarded real observed activity", r.files[path], err)
	}
}
