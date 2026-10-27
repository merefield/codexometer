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
			// The opt-in Thresholds view has separate coverage. Preserve the
			// existing snapshot for launches without a threshold policy.
			if view == viewThresholds {
				continue
			}
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
	// Baseline includes Pace graph controls, explicit panel/border
	// backgrounds, Sessions Zero/Close All, ACTIVE/ERRORS categories and responsive
	// summary filters; every theme, view and size
	// remains covered. The displayed pricing review date is now 2026-10-27.
	const want = "927550ee230c89085468c3aea6a7c9930beaa236d3e07ffc5a479690775fe2e1"
	if got != want {
		t.Fatalf("English presentation changed: got %s, want %s", got, want)
	}
}
