// Package tui is the interactive Bubble Tea front end: it runs a scan with a
// live progress screen, then lets the user browse the largest files, an
// ncdu-style folder tree, clutter categories and unreadable folders, and
// reveal anything in Finder. It never modifies the filesystem.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"inventur/internal/format"
	"inventur/internal/scan"
)

// Config configures the TUI.
type Config struct {
	Scan    scan.Options
	Stale   time.Duration // ages beyond this are highlighted
	Actions Actions
	Now     func() time.Time // for tests; defaults to time.Now
}

type keyMap struct {
	Up, Down, PgUp, PgDown, Home, End         key.Binding
	Enter, Back, Reveal, Goto, Copy           key.Binding
	Filter, Sort, Tab, ShiftTab, Rescan, Help key.Binding
	Quit                                      key.Binding
}

func newKeyMap() keyMap {
	b := func(keys []string, k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
	}
	return keyMap{
		Up:       b([]string{"up", "k"}, "↑/k", "up"),
		Down:     b([]string{"down", "j"}, "↓/j", "down"),
		PgUp:     b([]string{"pgup", "ctrl+u"}, "pgup", "page up"),
		PgDown:   b([]string{"pgdown", "ctrl+d"}, "pgdn", "page down"),
		Home:     b([]string{"home"}, "home", "top"),
		End:      b([]string{"end"}, "end", "bottom"),
		Enter:    b([]string{"enter", "right", "l"}, "enter", "open"),
		Back:     b([]string{"backspace", "left", "h"}, "←", "back"),
		Reveal:   b([]string{"o"}, "o", "reveal in Finder"),
		Goto:     b([]string{"g"}, "g", "show in folders"),
		Copy:     b([]string{"y"}, "y", "copy path"),
		Filter:   b([]string{"/"}, "/", "filter"),
		Sort:     b([]string{"s"}, "s", "sort size/age"),
		Tab:      b([]string{"tab"}, "tab", "next tab"),
		ShiftTab: b([]string{"shift+tab"}, "1-4", "jump to tab"),
		Rescan:   b([]string{"r"}, "r", "rescan"),
		Help:     b([]string{"?"}, "?", "more keys"),
		Quit:     b([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Reveal, k.Goto, k.Back, k.Filter, k.Tab, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PgUp, k.PgDown, k.Home, k.End},
		{k.Enter, k.Back, k.Reveal, k.Goto, k.Copy},
		{k.Filter, k.Sort, k.Tab, k.ShiftTab, k.Rescan, k.Help, k.Quit},
	}
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg   Config
	now   func() time.Time
	stale time.Duration
	st    styles
	keys  keyMap
	help  help.Model
	spin  spinner.Model
	input textinput.Model

	width, height int
	listH         int

	scanning  bool
	scanGen   int
	scanner   *scan.Scanner
	cancel    context.CancelFunc
	progress  scan.Progress
	scanStart time.Time
	err       error

	res       *scan.Result
	tab       tabID
	cwd       *scan.Node
	catSel    *scan.CategoryStat
	sortByAge bool
	filtering bool

	files, folders, cats, inst, skipped rowList

	flash    string
	flashBad bool
	flashSeq int
}

type (
	scanDoneMsg struct {
		gen int
		res *scan.Result
		err error
	}
	progressTickMsg struct{ gen int }
	actionDoneMsg   struct {
		verb, path string
		err        error
	}
	clearFlashMsg struct{ seq int }
)

// New creates the model; the scan starts in Init.
func New(cfg Config) *Model {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Actions == nil {
		cfg.Actions = System{}
	}
	m := &Model{
		cfg:   cfg,
		now:   cfg.Now,
		stale: cfg.Stale,
		keys:  newKeyMap(),
		help:  help.New(),
		spin:  spinner.New(spinner.WithSpinner(spinner.Dot)),
		input: textinput.New(),
		listH: 10,
	}
	m.input.Prompt = "/ "
	m.input.Placeholder = "filter by path"
	m.setTheme(true)
	return m
}

func (m *Model) setTheme(dark bool) {
	m.st = newStyles(dark)
	m.help.Styles = help.DefaultStyles(dark)
	m.input.SetStyles(textinput.DefaultStyles(dark))
	m.spin.Style = m.st.accent
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.startScan(), tea.RequestBackgroundColor)
}

func (m *Model) startScan() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.scanGen++
	gen := m.scanGen
	sc := scan.New(m.cfg.Scan)
	m.scanner, m.cancel, m.scanning, m.scanStart, m.progress = sc, cancel, true, time.Now(), scan.Progress{}
	run := func() tea.Msg {
		res, err := sc.Run(ctx)
		return scanDoneMsg{gen: gen, res: res, err: err}
	}
	return tea.Batch(run, m.spin.Tick, m.progressTick())
}

func (m *Model) progressTick() tea.Cmd {
	gen := m.scanGen
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return progressTickMsg{gen} })
}

func (m *Model) stopScan() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Model) finishScan(msg scanDoneMsg) {
	m.scanning, m.scanner, m.cancel = false, nil, nil
	if msg.err != nil {
		m.err = msg.err
		return
	}
	// After a rescan, stay where the user was.
	var prevPath string
	if m.cwd != nil {
		prevPath = m.cwd.Path()
	}
	prevCat := m.catSel
	m.res, m.cwd, m.catSel = msg.res, msg.res.Root, nil
	if n := m.res.Root.Find(prevPath); n != nil {
		m.cwd = n
	}
	for _, c := range m.res.Categories {
		if prevCat != nil && c.Category == prevCat.Category {
			m.catSel = c
		}
	}
	if m.tab == tabSkipped && len(m.res.Skipped) == 0 {
		m.tab = tabFiles
	}
	m.resize()
	m.rebuild()
}

// rebuild refreshes all lists from the result, keeping selections.
func (m *Model) rebuild() {
	m.files.set(m.buildFiles())
	m.folders.set(m.buildFolders())
	m.cats.set(m.buildCategories())
	if m.catSel != nil {
		m.inst.set(m.buildInstances())
	}
	m.skipped.set(m.buildSkipped())
}

func (m *Model) resize() {
	if m.res == nil {
		return
	}
	m.help.SetWidth(m.width)
	m.input.SetWidth(max(10, m.width-4))
	m.listH = m.layout().listH
	for _, l := range []*rowList{&m.files, &m.folders, &m.cats, &m.inst, &m.skipped} {
		l.setHeight(m.listH)
	}
}

// list returns the list shown in the current tab.
func (m *Model) list() *rowList {
	switch m.tab {
	case tabFolders:
		return &m.folders
	case tabCategories:
		if m.catSel != nil {
			return &m.inst
		}
		return &m.cats
	case tabSkipped:
		return &m.skipped
	}
	return &m.files
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
		return m, nil
	case spinner.TickMsg:
		if !m.scanning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case progressTickMsg:
		if msg.gen != m.scanGen || !m.scanning {
			return m, nil
		}
		m.progress = m.scanner.Progress()
		return m, m.progressTick()
	case scanDoneMsg:
		if msg.gen == m.scanGen {
			m.finishScan(msg)
		}
		return m, nil
	case actionDoneMsg:
		if msg.err != nil {
			return m, m.setFlash(fmt.Sprintf("%s: %v", m.short(msg.path), msg.err), true)
		}
		return m, m.setFlash(msg.verb+": "+m.short(msg.path), false)
	case clearFlashMsg:
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg.Mouse())
	case tea.MouseWheelMsg:
		if m.res != nil && !m.scanning {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.list().move(-3)
			case tea.MouseWheelDown:
				m.list().move(3)
			}
		}
		return m, nil
	}
	if m.filtering {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		m.stopScan()
		return m, tea.Quit
	}
	if m.scanning || m.err != nil || m.res == nil {
		if k == "q" || k == "esc" {
			m.stopScan()
			return m, tea.Quit
		}
		return m, nil
	}
	l := m.list()

	if m.filtering {
		switch k {
		case "enter":
			m.filtering = false
			m.input.Blur()
			return m, nil
		case "esc":
			m.filtering = false
			m.input.Blur()
			m.input.SetValue("")
			l.setFilter("")
			return m, nil
		case "up", "down", "pgup", "pgdown":
			l.move(map[string]int{"up": -1, "down": 1, "pgup": -l.height, "pgdown": l.height}[k])
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		l.setFilter(m.input.Value())
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.stopScan()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.resize()
	case key.Matches(msg, m.keys.Up):
		l.move(-1)
	case key.Matches(msg, m.keys.Down):
		l.move(1)
	case key.Matches(msg, m.keys.PgUp):
		l.move(-l.height)
	case key.Matches(msg, m.keys.PgDown):
		l.move(l.height)
	case key.Matches(msg, m.keys.Home):
		l.moveTo(0)
	case key.Matches(msg, m.keys.End):
		l.moveTo(len(l.rows) - 1)
	case key.Matches(msg, m.keys.Tab):
		m.cycleTab(1)
	case k == "shift+tab":
		m.cycleTab(-1)
	case len(k) == 1 && k >= "1" && k <= "4":
		m.switchTab(tabID(k[0] - '1'))
	case key.Matches(msg, m.keys.Enter):
		return m, m.activate()
	case k == "esc":
		if l.filter != "" {
			l.setFilter("")
			m.input.SetValue("")
		} else {
			m.back()
		}
	case key.Matches(msg, m.keys.Back):
		m.back()
	case key.Matches(msg, m.keys.Reveal):
		if r, ok := l.selected(); ok && r.path() != "" {
			return m, m.action("Revealed in Finder", r.path(), m.cfg.Actions.Reveal)
		}
	case key.Matches(msg, m.keys.Copy):
		if r, ok := l.selected(); ok && r.path() != "" {
			return m, m.action("Copied", r.path(), m.cfg.Actions.Copy)
		}
	case key.Matches(msg, m.keys.Goto):
		m.gotoSelected()
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.input.SetValue(l.filter)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case key.Matches(msg, m.keys.Sort):
		m.sortByAge = !m.sortByAge
		m.rebuild()
	case key.Matches(msg, m.keys.Rescan):
		return m, m.startScan()
	}
	return m, nil
}

func (m *Model) handleClick(ms tea.Mouse) (tea.Model, tea.Cmd) {
	if m.res == nil || m.scanning || ms.Button != tea.MouseLeft {
		return m, nil
	}
	lay := m.layout()
	switch {
	case ms.Y == lay.tabsY:
		for t, sp := range lay.tabs {
			if sp.contains(ms.X) {
				m.switchTab(t)
			}
		}
	case ms.Y == lay.contextY:
		for _, c := range lay.crumbs {
			if !c.contains(ms.X) {
				continue
			}
			if c.back {
				m.back()
			} else if c.node != m.cwd {
				m.cdUp(c.node)
			}
		}
	case ms.Y >= lay.listY && ms.Y < lay.listY+lay.listH:
		if ok, double := m.list().click(ms.Y-lay.listY, m.now()); ok && double {
			return m, m.activate()
		}
	}
	return m, nil
}

// activate opens the selected row: directories are entered, files are
// revealed in Finder, categories show their instances.
func (m *Model) activate() tea.Cmd {
	r, ok := m.list().selected()
	if !ok {
		return nil
	}
	switch r.kind {
	case rowUp:
		m.back()
	case rowDir:
		m.switchTab(tabFolders)
		m.cd(r.dir, nil)
	case rowCategory:
		m.catSel = r.cat
		m.inst = rowList{height: m.listH}
		m.inst.set(m.buildInstances())
	case rowFile, rowSmall, rowSkip:
		return m.action("Revealed in Finder", r.path(), m.cfg.Actions.Reveal)
	}
	return nil
}

func (m *Model) back() {
	switch m.tab {
	case tabFolders:
		if m.cwd.Parent != nil {
			m.cdUp(m.cwd.Parent)
		}
	case tabCategories:
		m.catSel = nil
	}
}

// cd shows dir in the folder browser and selects the first row matching
// sel, or the first entry.
func (m *Model) cd(dir *scan.Node, sel func(row) bool) {
	m.cwd = dir
	m.folders = rowList{height: m.listH}
	m.folders.set(m.buildFolders())
	if sel != nil && m.folders.selectWhere(sel) {
		return
	}
	if len(m.folders.rows) > 1 && m.folders.rows[0].kind == rowUp {
		m.folders.moveTo(1)
	}
}

// cdUp goes to an ancestor and selects the child we came from.
func (m *Model) cdUp(ancestor *scan.Node) {
	child := m.cwd
	for child.Parent != nil && child.Parent != ancestor {
		child = child.Parent
	}
	m.cd(ancestor, func(r row) bool { return r.kind == rowDir && r.dir == child })
}

// gotoSelected shows the selected item in its folder.
func (m *Model) gotoSelected() {
	r, ok := m.list().selected()
	if !ok {
		return
	}
	switch r.kind {
	case rowFile:
		m.switchTab(tabFolders)
		m.cd(r.file.Parent, func(x row) bool { return x.kind == rowFile && x.file == r.file })
	case rowDir:
		m.switchTab(tabFolders)
		if r.dir.Parent == nil {
			m.cd(r.dir, nil)
			return
		}
		m.cd(r.dir.Parent, func(x row) bool { return x.kind == rowDir && x.dir == r.dir })
	case rowSkip:
		if parent := m.res.Root.Find(filepath.Dir(r.skip.Path)); parent != nil {
			name := filepath.Base(r.skip.Path)
			m.switchTab(tabFolders)
			m.cd(parent, func(x row) bool { return x.kind == rowDir && x.dir.Name == name })
		}
	}
}

func (m *Model) switchTab(t tabID) {
	for _, have := range m.tabs() {
		if have == t {
			m.tab = t
			m.filtering = false
			m.input.Blur()
			return
		}
	}
}

func (m *Model) cycleTab(d int) {
	tabs := m.tabs()
	for i, t := range tabs {
		if t == m.tab {
			m.switchTab(tabs[(i+d+len(tabs))%len(tabs)])
			return
		}
	}
}

// action runs a side effect (reveal / copy) off the UI goroutine.
func (m *Model) action(verb, path string, fn func(string) error) tea.Cmd {
	return func() tea.Msg { return actionDoneMsg{verb: verb, path: path, err: fn(path)} }
}

func (m *Model) setFlash(s string, bad bool) tea.Cmd {
	m.flashSeq++
	seq := m.flashSeq
	m.flash, m.flashBad = s, bad
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearFlashMsg{seq} })
}

func (m *Model) short(p string) string { return format.ShortPath(p, m.cfg.Scan.Home) }

// ---- view ----

func (m *Model) View() tea.View {
	var s string
	switch {
	case m.width == 0:
	case m.err != nil:
		s = m.errorView()
	case m.scanning || m.res == nil:
		s = m.scanView()
	default:
		s = m.resultsView()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "inventur"
	return v
}

func (m *Model) scanView() string {
	p := m.progress
	el := time.Since(m.scanStart)
	rate := float64(p.Files) / max(el.Seconds(), 0.001)
	lines := []string{
		"",
		"  " + m.st.title.Render("inventur") + "  " + m.spin.View() + " Scanning " + m.st.accent.Render(m.short(m.cfg.Scan.Root)),
		"",
		fmt.Sprintf("  %s files · %s folders · %s", m.st.size.Render(format.Count(p.Files)),
			m.st.size.Render(format.Count(p.Dirs)), m.st.size.Render(format.Size(p.Bytes))),
		m.st.dim.Render(fmt.Sprintf("  %s files/s · %s elapsed", format.Count(int64(rate)), el.Round(time.Second))),
		"",
		"  " + m.st.dim.Render(format.TruncLeft(m.short(p.Current), max(10, m.width-4))),
		"",
		m.st.dim.Render("  Read-only: nothing is modified or deleted.   q to cancel"),
	}
	return strings.Join(lines, "\n")
}

func (m *Model) errorView() string {
	return "\n  " + m.st.bad.Render("Cannot scan: "+m.err.Error()) + "\n\n  " + m.st.dim.Render("q to quit")
}

func (m *Model) resultsView() string {
	lay := m.layout()
	fit := func(s string) string { return ansi.Truncate(s, m.width, "…") }
	tabs, _ := m.tabBar()
	ctx, _ := m.contextLine()
	lines := []string{fit(m.header()), fit(tabs), fit(ctx), fit(m.columnHeader())}

	l := m.list()
	start, end := l.visible()
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(l.rows[i], m.width, i == l.cursor))
	}
	if len(l.rows) == 0 {
		msg := "  nothing here"
		if l.filter != "" {
			msg = fmt.Sprintf("  no matches for %q", l.filter)
		}
		lines = append(lines, m.st.dim.Render(msg))
	}
	for len(lines) < lay.statusY {
		lines = append(lines, "")
	}
	lines = append(lines, fit(m.statusLine()), m.help.View(m.keys))
	return strings.Join(lines, "\n")
}

func (m *Model) statusLine() string {
	if m.filtering {
		return m.input.View()
	}
	if m.flash != "" {
		if m.flashBad {
			return m.st.bad.Render("✗ " + m.flash)
		}
		return m.st.ok.Render("✓ " + m.flash)
	}
	l := m.list()
	left := ""
	if l.filter != "" {
		left = m.st.accent.Render("filter: "+l.filter) + m.st.dim.Render("  (esc to clear)")
	}
	right := m.st.dim.Render(fmt.Sprintf("%d/%d", min(l.cursor+1, len(l.rows)), len(l.rows)))
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}
