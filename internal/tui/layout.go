package tui

import "charm.land/lipgloss/v2"

// layout describes where everything sits on screen. View renders with it
// and mouse handling hit-tests against it, so the two can never disagree.
//
//	y=0  header
//	y=1  tabs
//	y=2  context line (breadcrumb / summary)
//	y=3  column header
//	y=4… list rows
//	     status line (filter input, flash message, position)
//	     help
type layout struct {
	tabsY, contextY, columnsY int
	listY, listH              int
	statusY, helpY            int
	tabs                      map[tabID]span
	crumbs                    []crumb
}

func (m *Model) layout() layout {
	_, tabs := m.tabBar()
	_, crumbs := m.contextLine()
	helpH := lipgloss.Height(m.help.View(m.keys))
	l := layout{tabsY: 1, contextY: 2, columnsY: 3, listY: 4, tabs: tabs, crumbs: crumbs}
	l.listH = max(1, m.height-l.listY-1-helpH)
	l.statusY = l.listY + l.listH
	l.helpY = l.statusY + 1
	return l
}

func (s span) contains(x int) bool { return x >= s.x0 && x < s.x1 }
