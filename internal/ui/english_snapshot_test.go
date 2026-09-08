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
	// Captured from the released v0.12.0 renderer, before localisation: all
	// themes, all views, and three terminal sizes (105 complete screens).
	const want = "ea99652c4980b370759d19139e4e9eb52a554d95b11ccbb67c5f8e7a053cca41"
	if got != want {
		t.Fatalf("English presentation changed: got %s, want %s", got, want)
	}
}
