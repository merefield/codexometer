package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/merefield/codexometer/internal/codex"
	"github.com/merefield/codexometer/internal/i18n"
)

func TestDetailMetadataSharesTimelineOnlyWhenItFits(t *testing.T) {
	m := historyTestModel()
	c := &m.monitorSessionData[0].preview
	c.Source, c.ThreadID, c.At = "LIVE", "界-session", time.Time{}
	c.CurrentTask, c.Text = "", "Ready."
	metadata := "LIVE // 界-session // --"
	colors := paletteFor(m.theme)
	buttons := m.historyNavigationButtons(120, 30)
	minWidth := 4 + lipgloss.Width(m.renderMonitorNavigationButtons(buttons, colors)+" "+metadata)
	for _, width := range []int{minWidth - 1, minWidth, minWidth + 10} {
		frame := strings.Split(m.renderMonitorContextDetail(width, 30, colors), "\n")
		lines := stripLinkLines(frame)
		row := 1
		if width < minWidth {
			row++
			if !strings.HasPrefix(lines[row], "│ "+metadata) {
				t.Fatalf("width %d: fallback should start at the left content edge: %q", width, lines[row])
			}
		} else if !strings.HasSuffix(lines[row], metadata+" │") {
			t.Fatalf("width %d: inline metadata should end at the right content edge: %q", width, lines[row])
		}
		if strings.Count(strings.Join(lines, "\n"), metadata) != 1 ||
			strings.Trim(lines[row+1], " │") != "" || !strings.Contains(lines[row+2], "LAST REPLY ─") {
			t.Fatalf("width %d, incorrect timeline / spacer / heading: %q", width, lines[:6])
		}
		for _, b := range buttons {
			if ansi.Cut(lines[1], b.rect.x, b.rect.x+b.rect.width) != b.label {
				t.Fatalf("width %d, displaced %s", width, b.label)
			}
		}
		for _, cell := range backgroundTestLines(frame[row])[0] {
			if cell.Content == "界" && cell.Style.Attrs&uv.AttrBold != 0 {
				t.Fatal("metadata should be subdued")
			}
		}
	}
	// Leave exactly two rows for LAST REPLY and its content.
	short := m.detailPage(minWidth, 5, 0)
	if len(short.buttons) == 0 || len(short.document) != 2 || short.document[0].kind != "heading" || short.document[1].text != "Ready." {
		t.Fatalf("spacer displaced content: %#v", short)
	}
	m.monitorHistory = nil
	alone := strings.Split(ansi.Strip(m.renderMonitorContextDetail(120, 30, colors)), "\n")
	if !strings.HasPrefix(alone[1], "│ "+metadata) || !strings.Contains(alone[3], "LAST REPLY ─") {
		t.Fatalf("no-timeline fallback: %q", alone[:4])
	}
}

func TestDetailTimelineDescribesSelectedObservation(t *testing.T) {
	m := historyTestModel()
	c := historyContext(10)
	c.Source, c.At = "LOCAL", time.Now().Add(-time.Minute)
	m.monitorSessionData[0].preview = c
	m.observeMonitorHistory()
	m.monitorSessionData[0].preview = historyContext(11)
	m.monitorSessionData[0].preview.Source = "LIVE"
	m.observeMonitorHistory()
	m.moveMonitorHistory(-1)
	page := m.detailPage(128, 40, 0)
	if !strings.Contains(page.right, "LOCAL // root-one // ") || !strings.HasSuffix(page.right, " ago") || strings.Contains(page.right, "LIVE") {
		t.Fatalf("timeline showed the wrong observation: %q", page.right)
	}
}

func TestMonitorDetailStructuredSections(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.CommandDetails = codex.ApprovalCommandDetails{
		Justification: "Check the repository.\nCommand: this is just explanatory prose.",
		Command:       "printf 'Directory: not metadata'\nprintf ok",
		Directory:     "/work/project",
	}
	c.ApprovalOptions[4].Detail = `["printf"]`
	c.ApprovalDecisions = "accept, acceptForSession, cancel"
	before := *c
	for _, width := range []int{1, 20, 80, 120} {
		doc := m.contextDetailDocument(width)
		for _, line := range doc {
			if lipgloss.Width(line.text) > width {
				t.Fatalf("line overflow at %d: %q", width, line.text)
			}
		}
		if width < 80 {
			continue
		}
		var command []string
		for _, line := range doc {
			if line.kind == "command" {
				command = append(command, strings.TrimPrefix(line.text, "│ "))
			}
		}
		if strings.Join(command, "\n") != c.CommandDetails.Command {
			t.Fatal("command text/line breaks changed", command)
		}
		plain := strings.Join(m.contextDetailLines(width), "\n")
		for _, label := range []string{i18n.Text("JUSTIFICATION"), i18n.Text("COMMAND"), i18n.Text("WORKING DIRECTORY"), i18n.Text("PERSISTENT PERMISSION RULE"), i18n.Text("OFFERED DECISIONS")} {
			if !strings.Contains(plain, "\n\n"+label) {
				t.Fatal("section lacks separation", label, plain)
			}
		}
		if !strings.Contains(plain, c.CommandDetails.Justification) {
			t.Fatal("justification split by untrusted label")
		}
	}
	if *c != before {
		t.Fatal("formatting changed request or capability")
	}
	for theme := themeHacker; theme < themeCount; theme++ {
		colors := paletteFor(theme)
		meta := (detailLine{"same", "metadata"}).render(colors)
		head := (detailLine{"same", "heading"}).render(colors)
		if meta == head || ansi.Strip(meta) != "same" || ansi.Strip(head) != "same" {
			t.Fatal("section styles indistinguishable")
		}
	}
}

func TestApprovalCommentaryFullDetailOnly(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.ApprovalContext = "The preceding explanation"
	c.CommandDetails = codex.ApprovalCommandDetails{Justification: "Allow the check?", Command: "git status", Directory: "/work"}
	before := *c
	for _, width := range []int{1, 20, 80} {
		for _, line := range m.contextDetailDocument(width) {
			if lipgloss.Width(line.text) > width {
				t.Fatal("context overflow", line)
			}
		}
	}
	text := strings.Join(m.contextDetailLines(100), "\n")
	contextAt, reasonAt := strings.Index(text, c.ApprovalContext), strings.Index(text, c.CommandDetails.Justification)
	if contextAt < 0 || reasonAt <= contextAt || !strings.Contains(text, "\n\n"+i18n.Text("CONTEXT")) {
		t.Fatal(text)
	}
	if *c != before {
		t.Fatal("rendering changed approval")
	}
	colors := paletteFor(m.theme)
	if text := ansi.Strip(m.renderExpandedContext(100, 30, m.monitorSessionData[0], colors)); strings.Contains(text, c.ApprovalContext) {
		t.Fatal("context leaked into row")
	}
}

func TestApprovalDetailKeepsCommandWhitespaceAndControls(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.CommandDetails = codex.ApprovalCommandDetails{Command: "\n  printf 'a\tb'  \r\n\n", Directory: "/work"}
	before := *c
	var command []string
	for _, line := range m.contextDetailDocument(120) {
		if line.kind == "command" {
			command = append(command, strings.TrimPrefix(line.text, "│ "))
		}
	}
	if got := strings.Join(command, "\n"); got != "\n  printf 'a    b'  \n\n" {
		t.Fatalf("command formatting lost: %q", got)
	}
	g := m.monitorDashboardLayout()
	if !m.monitorApprovalControls(g.contentWidth, g.meterHeight) {
		t.Fatal("whitespace disabled approval controls")
	}
	if *c != before {
		t.Fatal("display formatting changed the original approval")
	}
}

func TestHumanApprovalUsesExistingReviewWithClearPolicyLabel(t *testing.T) {
	m := approvalTestModel()
	c := &m.monitorSessionData[0].preview
	c.RequiredHumanApproval = true
	c.CommandDetails = codex.ApprovalCommandDetails{Command: "git push", Directory: "/work"}
	text := strings.Join(m.contextDetailLines(120), "\n")
	if !strings.Contains(text, "HUMAN APPROVAL REQUIRED") || !strings.Contains(text, "Managed policy requires approval each time.") {
		t.Fatal("missing human checkpoint explanation", text)
	}
}
