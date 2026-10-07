package ui

import (
	"github.com/merefield/codexometer/internal/i18n"
	"time"
)

func scheduleDate(t time.Time) string {
	if i18n.Code() == "en-GB" {
		return t.Format("02 Jan 2006 15:04 MST -07:00")
	}
	return t.Format("2006-01-02 15:04 MST -07:00")
}

// All coordinates are display cells, never byte offsets.
type scheduleControl struct {
	key                         string
	x, row, width, focus, value int
}

func (m Model) scheduleControlAt(x, y int) scheduleControl {
	g := m.monitorDashboardLayout()
	l := m.schedulePaneLayout(g.contentWidth, g.meterHeight)
	row := y - g.meterY - 1
	if row < 0 || row >= l.textRows || row+l.scroll >= len(l.indices) {
		return scheduleControl{}
	}
	m.width = g.contentWidth
	_, controls := m.scheduleFormDocument()
	for _, c := range controls {
		if c.row == l.indices[row+l.scroll] && x >= 4+c.x && x < 4+c.x+c.width {
			return c
		}
	}
	return scheduleControl{}
}

// Let the normal dashboard router handle navigation outside an embedded editor.
func (m Model) editorNavigationAt(x, y int) bool {
	_, tab := m.mainTabAt(x, y)
	return tab || m.headerActionAt(x, y) != "" || m.monitorAttentionAt(x, y) != ""
}
