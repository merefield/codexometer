package ui

// These scenes call the real TUI renderer, but never initialise the application,
// read credentials/session files, start a server or execute a model/tool request.
// Keeping them in a test file also keeps screenshot tooling out of the binary.
import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

type documentationScene struct {
	name, caption string
	model         Model
	contains      []string
}

type documentationCell struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Text      string `json:"text"`
	Width     int    `json:"width"`
	FG        string `json:"fg"`
	BG        string `json:"bg"`
	Bold      bool   `json:"bold,omitempty"`
	Faint     bool   `json:"faint,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
}

type documentationFrame struct {
	Name    string              `json:"name"`
	Caption string              `json:"caption"`
	Version string              `json:"version"`
	Columns int                 `json:"columns"`
	Rows    int                 `json:"rows"`
	Cells   []documentationCell `json:"cells"`
}

// Only synthetic, display-only capabilities exist in these fixtures. A future
// accidental command dispatch must fail instead of performing any real work.
type documentationClient struct{}

func (documentationClient) Fetch(context.Context) (codex.Snapshot, error) {
	panic("documentation scenes must not fetch")
}
func (documentationClient) SessionApprovalPending(token string) bool {
	return token == "screenshot-approval"
}
func (documentationClient) RespondSessionApproval(context.Context, string, string) error {
	panic("documentation scenes must not approve")
}
func (documentationClient) SessionPrompt(id string) codex.SessionPromptOffer {
	return codex.SessionPromptOffer{Token: "screenshot-prompt", ThreadID: id}
}
func (documentationClient) SendSessionPrompt(context.Context, string, []string) error {
	panic("documentation scenes must not send")
}

func documentationScenes(version string) []documentationScene {
	now := time.Now()
	base := func(view meterViewID, theme themeID, height int) Model {
		return Model{
			width: 136, height: height, meterView: view, theme: theme,
			appVersion: version, snapshot: codex.DemoSnapshot(),
			fetcher: documentationClient{}, refreshEvery: time.Minute,
			lastRefresh: now, nextRefresh: now.Add(time.Minute), phase: 3,
			resetThreshold: 80, resetWarningHours: 72,
		}
	}
	sessions := base(viewMonitor, themeHacker, 45)
	sessions.monitorState = monitorRunning
	sessions.monitorStartedAt = now.Add(-18 * time.Minute)
	budget := int64(500_000)
	for i, entry := range []struct{ id, name, directory, model, effort string }{
		{"demo-session-7c41a", "Make the dashboard shine", "/projects/codexometer", "gpt-6.1-sol", "high"},
		{"demo-session-a92bd", "Ship the regression tests", "/projects/starship", "gpt-6-astra", "medium"},
		{"demo-session-f83ce", "Polish the launch notes", "/projects/flight-manual", "gpt-6-luna", "high"},
	} {
		s := monitorSession{
			id: entry.id, name: entry.name, workingDirectory: entry.directory,
			active: true, displayed: true, startedAt: sessions.monitorStartedAt,
			lastActivity: now.Add(-8 * time.Second), latest: int64(168_400 - i*47_200),
			modelSettings: codex.SessionModelSettings{Model: entry.model, ReasoningEffort: entry.effort, ServiceTier: "default"},
			modelCalls:    22 - i*6, lastCallAt: now.Add(-8 * time.Second), latestOutput: 840, peakOutput: 2_400, latestOutputOK: true, peakOutputOK: true,
			preview: codex.SessionContext{ThreadID: entry.id, Source: "LIVE", At: now.Add(-8 * time.Second)},
		}
		for j := 0; j < 36; j++ {
			value := int64(400 + (j*791+i*1217)%5400)
			if (j+i*3)%11 < 3 {
				value /= 5
			}
			s.samples = append(s.samples, monitorSample{At: now.Add(time.Duration(j-35) * 30 * time.Second), Duration: 30 * time.Second, Tokens: value})
		}
		sessions.monitorLatest += s.latest
		sessions.monitorSessionData = append(sessions.monitorSessionData, s)
	}
	sessions.monitorSessionData[0].working = true
	sessions.monitorSessionData[0].agentCount = 2
	sessions.monitorSessionData[0].modelSettings.ServiceTier = "fast"
	sessions.monitorSessionData[0].goal = &codex.SessionGoal{Status: "active", Objective: "Build a beautiful, responsive dashboard", TokensUsed: 168_400, TokenBudget: &budget}
	sessions.monitorSessionData[0].preview.Kind = codex.SessionContextActivity
	sessions.monitorSessionData[0].preview.Text = "The layout is responsive. I’m checking the narrow-terminal behaviour next."
	sessions.monitorSessionData[0].preview.CurrentTask = "Give the dashboard a little more rocket fuel."
	sessions.monitorSessionData[1].attention = codex.SessionAttentionApproval
	sessions.monitorSessionData[1].preview.Kind = codex.SessionContextApproval
	sessions.monitorSessionData[1].preview.Text = "Run the targeted regression tests before shipping?"
	sessions.monitorSessionData[1].preview.ApprovalContext = "The new layout is ready. I’d like to verify that every button still clicks where it belongs."
	sessions.monitorSessionData[1].preview.ApprovalToken = "screenshot-approval"
	sessions.monitorSessionData[1].preview.PendingApprovals = 1
	sessions.monitorSessionData[1].preview.CommandDetails = codex.ApprovalCommandDetails{
		Justification: "Run the dashboard regression suite. This only executes the project's tests.",
		Command:       "go test ./internal/ui -run TestResponsiveLayout", Directory: "/projects/starship",
	}
	sessions.monitorSessionData[1].preview.ApprovalOptions = [8]codex.ApprovalOption{
		{Kind: "accept", Value: "accept"}, {Kind: "decline", Value: "decline"}, {Kind: "cancel", Value: "cancel"},
	}
	sessions.monitorSessionData[2].attention = codex.SessionAttentionComplete
	sessions.monitorSessionData[2].preview.Kind = codex.SessionContextReply
	sessions.monitorSessionData[2].preview.Text = "Launch notes polished.\n\n• Installation steps checked\n• New features explained\n• Ready for lift-off!"
	sessions.monitorSessionData[2].preview.CurrentTask = "Make the release notes useful, concise and just a little retro."
	sessions.monitorSelectedID = sessions.monitorSessionData[1].id
	sessions.refreshMonitorRates(now, true)

	approval := sessions
	approval.height, approval.theme = 35, themeBlueSteel
	approval.openMonitorContext(sessions.monitorSelectedID)

	scheduler := sessions
	scheduler.height, scheduler.theme = 33, themeUltraviolet
	scheduler.openMonitorContext(sessions.monitorSessionData[2].id)
	scheduler.openSchedule() // Returns a command; intentionally never dispatched.
	scheduler.scheduleUI.input.SetValue("Review the launch checklist and report anything we missed.")
	scheduler.scheduleUI.hours, scheduler.scheduleUI.minutes = 2, 30
	scheduler.scheduleUI.mode = 1
	scheduler.scheduleUI.focus = 0

	usage := base(viewUsage, themeHacker, 26)
	usage.history.data = codex.AccountUsage{FetchedAt: now, Reports: codex.DemoUsageReports(now)}
	lifetime, peak, streak, longest, turn := int64(142_500_000), int64(780_000), int64(12), int64(28), int64(1480)
	usage.history.data.Summary = codex.AccountUsageSummary{LifetimeTokens: &lifetime, PeakDailyTokens: &peak, CurrentStreakDays: &streak, LongestStreakDays: &longest, LongestRunningTurnSec: &turn}
	for i := 364; i >= 0; i-- {
		tokens := int64((i*137+31)%39) * 19_000
		if i == 12 || i > 12 && (i%7 == 0 || i%17 < 3) {
			tokens = 0
		}
		usage.history.data.DailyUsageBuckets = append(usage.history.data.DailyUsageBuckets, codex.AccountUsageDay{StartDate: now.AddDate(0, 0, -i).Format(time.DateOnly), Tokens: tokens})
	}
	benchmark := base(viewBenchmark, themeRust, 27)
	benchmark.benchmarkState = benchmarkFinished
	benchmark.benchmarkPlan = codex.BenchmarkPlan{Efforts: []string{"high"}, Models: []codex.BenchmarkModelOption{
		{Model: "gpt-6.1-sol", DisplayName: "GPT-6.1 Sol", Efforts: []string{"high"}},
		{Model: "gpt-6-astra", DisplayName: "GPT-6 Astra", Efforts: []string{"high"}},
		{Model: "gpt-6-luna", DisplayName: "GPT-6 Luna", Efforts: []string{"high"}},
	}}
	benchmark.benchmarkScope = benchmark.benchmarkPlan.AllScope()
	benchmark.benchmarkScopeTasks = map[codex.BenchmarkSuiteID][]codex.BenchmarkTaskID{
		codex.BenchmarkSuiteCore: {codex.BenchmarkMergeRanges, codex.BenchmarkLRUCache},
	}
	benchmark.benchmarkCombinations = 3
	for i, task := range []struct{ id, name string }{{"merge-ranges", "MERGE RANGES"}, {"lru-cache", "LRU CACHE"}} {
		for j, model := range []struct{ id, name string }{{"gpt-6.1-sol", "GPT-6.1 Sol"}, {"gpt-6-astra", "GPT-6 Astra"}, {"gpt-6-luna", "GPT-6 Luna"}} {
			benchmark.benchmarkResults = append(benchmark.benchmarkResults, codex.BenchmarkResult{
				TaskID: codex.BenchmarkTaskID(task.id), TaskName: task.name, Model: model.id, DisplayName: model.name, Effort: "high",
				Correct: i+j != 3, Duration: time.Duration(12+i*7+j*9) * time.Second,
				Usage: codex.BenchmarkUsage{TotalTokens: int64(7400 + j*1600 + i*2300)}, UsageKnown: true,
				CostKnown: true, CostUSD: []float64{.018, .072, .002}[j] * float64(i+1),
			})
		}
	}
	benchmark.benchmarkTotal, benchmark.benchmarkCompleted = 6, 6

	return []documentationScene{
		{"sessions", "Sessions command centre: three synthetic sessions, live activity and an approval awaiting review.", sessions, []string{"SESSION TOTALS", "WORKING", "APPROVAL", "TURN COMPLETE", "Make the dashboard"}},
		{"quota", "Quota Bars: synthetic short-term and weekly allowances, with directly comparable reset-cycle gauges.", base(viewBars, themeHacker, 32), []string{"BARS", "RESET CYCLE"}},
		{"pace", "Quota Pace: synthetic consumption plotted against time elapsed.", base(viewPace, themeNightshade, 32), []string{"PACE"}},
		{"approval", "Session detail: synthetic commentary, the exact command to review, and explicit approval controls.", approval, []string{"APPROVAL", "go test", "starship"}},
		{"usage", "Usage Daily: a year of synthetic account history, with brighter cells indicating more activity.", usage, []string{"DAILY", "LIFETIME", "2026"}},
		{"benchmark", "Benchmark: illustrative results only, not measured model performance or price comparisons.", benchmark, []string{"RESULT MATRIX", "PASS", "FAIL"}},
		{"scheduler", "Schedule a synthetic follow-up without leaving session detail; no job is submitted.", scheduler, []string{"SCHEDULE FOLLOW-UP", "Hours", "Minutes"}},
	}
}

func documentationColour(c, fallback color.Color) string {
	if c == nil {
		c = fallback
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func documentationCells(content string, width, height int, palette palette) []documentationCell {
	screen := uv.NewScreenBuffer(width, height)
	uv.NewStyledString(content).Draw(&screen, image.Rect(0, 0, width, height))
	cells := make([]documentationCell, 0, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; {
			cell := screen.CellAt(x, y)
			w := max(cell.Width, 1)
			fg, bg := cell.Style.Fg, cell.Style.Bg
			if fg == nil {
				fg = palette.primary
			}
			if bg == nil {
				bg = palette.background
			}
			if cell.Style.Attrs&uv.AttrReverse != 0 {
				fg, bg = bg, fg
			}
			text := cell.Content
			if cell.Style.Attrs&uv.AttrConceal != 0 {
				text = ""
			}
			cells = append(cells, documentationCell{
				X: x, Y: y, Text: text, Width: w,
				FG: documentationColour(fg, palette.primary), BG: documentationColour(bg, palette.background),
				Bold: cell.Style.Attrs&uv.AttrBold != 0, Faint: cell.Style.Attrs&uv.AttrFaint != 0,
				Italic: cell.Style.Attrs&uv.AttrItalic != 0, Underline: cell.Style.Underline != 0,
			})
			x += w
		}
	}
	return cells
}

func TestDocumentationCellsPreserveTerminalGeometryAndStyles(t *testing.T) {
	palette := paletteFor(themeHacker)
	content := "\x1b[1;38;2;255;16;32;48;2;1;2;3mA\x1b[0m界" +
		ansi.SetHyperlink("https://example.com/private", "") + "B" + ansi.ResetHyperlink()
	cells := documentationCells(content, 5, 1, palette)
	if len(cells) != 4 || cells[0].Text != "A" || cells[0].FG != "#ff1020" || cells[0].BG != "#010203" || !cells[0].Bold {
		t.Fatal("ANSI colour or bold styling was lost", cells)
	}
	if cells[1].Text != "界" || cells[1].X != 1 || cells[1].Width != 2 || cells[2].Text != "B" || cells[2].X != 3 {
		t.Fatal("wide character shifted following cells", cells)
	}
	encoded, err := json.Marshal(cells)
	if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "example.com") {
		t.Fatal("hyperlink destination leaked into exported cells", err)
	}
	reversed := documentationCells("\x1b[7mX", 1, 1, palette)[0]
	if reversed.FG != documentationColour(palette.background, nil) || reversed.BG != documentationColour(palette.primary, nil) {
		t.Fatal("default reverse colours were lost", reversed)
	}
}

func TestDocumentationScreenshots(t *testing.T) {
	if i18n.Code() != "en-GB" {
		t.Skip("documentation scenes deliberately use the default English presentation")
	}
	versionBytes, err := os.ReadFile("../version/VERSION")
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	synctest.Test(t, func(t *testing.T) {
		// Freeze every time.Now call, including countdowns and command timers.
		instant := time.Date(2026, 11, 5, 14, 30, 0, 0, time.UTC)
		time.Sleep(instant.Sub(time.Now()))
		for _, scene := range documentationScenes(version) {
			func() {
				m := scene.model
				content := m.View().Content
				if lipgloss.Width(content) != m.width || lipgloss.Height(content) != m.height {
					t.Fatalf("scene does not fit %dx%d", m.width, m.height)
				}
				for _, label := range scene.contains {
					if !strings.Contains(ansi.Strip(content), label) {
						t.Fatalf("missing %q in scene:\n%s", label, ansi.Strip(content))
					}
				}
				if content != m.View().Content {
					t.Fatal("screenshot is not deterministic")
				}
				frame := documentationFrame{scene.name, scene.caption, version, m.width, m.height, documentationCells(content, m.width, m.height, paletteFor(m.theme))}
				data, err := json.Marshal(frame)
				if err != nil {
					t.Fatal(err)
				}
				// Export only on explicit request. Normal tests never write images.
				if dir := os.Getenv("CODEXOMETER_SCREENSHOT_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, scene.name+".json"), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}()
		}
	})
}
