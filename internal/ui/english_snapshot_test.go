package ui

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
)

func TestEnglishPresentationSnapshot(t *testing.T) {
	hash := sha256.New()
	const snapshotVersion = "1.2.3"
	stripVersionLink := regexp.MustCompile(regexp.QuoteMeta(ansi.SetHyperlink(versionHighlightsURL(snapshotVersion))) + `(.*?)` + regexp.QuoteMeta(ansi.ResetHyperlink()))
	for theme := themeHacker; theme < themeCount; theme++ {
		for view := viewBars; view < viewCount; view++ {
			for _, size := range []struct{ w, h int }{{40, 16}, {80, 24}, {120, 40}} {
				snapshot := codex.DemoSnapshot()
				snapshot.RateLimits.Primary.ResetsAt = nil
				snapshot.RateLimits.Secondary.ResetsAt = nil
				snapshot.FetchedAt = time.Time{}
				m := Model{snapshot: snapshot, width: size.w, height: size.h, meterView: view, theme: theme, appVersion: snapshotVersion}
				// Hyperlink metadata is intentionally new; retain the original
				// byte-for-byte check of all text, colour and layout sequences.
				rendered := stripVersionLink.ReplaceAllString(m.render(), "$1")
				fmt.Fprintf(hash, "%d/%d/%d/%d\n%s\n", theme, view, size.w, size.h, rendered)
			}
		}
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	// Baseline intentionally updated when removing Monitor's Pause button and
	// widening its readout; all themes, views and three terminal sizes are covered.
	const want = "4ae2271f6559dc94bde5b965c13a398ba7a41d4be990c6d5dc9727cdcf9b9f1a"
	if got != want {
		t.Fatalf("English presentation changed: got %s, want %s", got, want)
	}
}
