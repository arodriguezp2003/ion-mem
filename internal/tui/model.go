package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── view states ──────────────────────────────────────────────────────────────

type viewState int

const (
	viewProjects viewState = iota
	viewObservations
	viewDetail
	viewGlobalSearch // cross-project search results — observations list with project column
)

// ─── messages ─────────────────────────────────────────────────────────────────

// projectsLoadedMsg is sent when project summaries are fetched from the store.
type projectsLoadedMsg struct {
	summaries []store.ProjectSummary
}

// observationsLoadedMsg is sent when observations are fetched for a project.
type observationsLoadedMsg struct {
	observations []store.Observation
	project      string
}

// searchResultMsg is sent when a SearchWithFallback completes.
type searchResultMsg struct {
	results []store.Observation
	fuzzy   bool
}

// searchSubmitMsg is sent when the user presses Enter in the search input.
type searchSubmitMsg struct {
	query string
}

// deleteResultMsg is sent after a soft delete succeeds.
type deleteResultMsg struct {
	project string
}

// errMsg wraps an error for display in the status bar.
type errMsg struct{ err error }

// ─── key bindings ──────────────────────────────────────────────────────────────

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Quit   key.Binding
	Search key.Binding
	Delete key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Search: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Search, k.Delete, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// ─── theme ────────────────────────────────────────────────────────────────────

// theme is the single source of truth for all visual styling. Colors use
// AdaptiveColor so the palette works on both light and dark terminals.
type theme struct {
	accent  lipgloss.AdaptiveColor
	dim     lipgloss.AdaptiveColor
	danger  lipgloss.AdaptiveColor
	muted   lipgloss.AdaptiveColor
	surface lipgloss.AdaptiveColor
}

var defaultTheme = theme{
	accent:  lipgloss.AdaptiveColor{Dark: "#7D56F4", Light: "#5B33D9"},
	dim:     lipgloss.AdaptiveColor{Dark: "#626262", Light: "#888888"},
	danger:  lipgloss.AdaptiveColor{Dark: "#FF5F87", Light: "#D00050"},
	muted:   lipgloss.AdaptiveColor{Dark: "#444444", Light: "#BBBBBB"},
	surface: lipgloss.AdaptiveColor{Dark: "#1C1C1C", Light: "#F5F5F5"},
}

// badgeTints maps observation types to a subtle tinted background color.
// All tints share the same purple-blue hue family — only brightness varies.
// Text uses a near-white foreground so it reads on all tint levels.
var badgeTints = map[string]string{
	"decision":     "#3D2A7A", // deepest — most important type
	"architecture": "#3A2E8A",
	"bugfix":       "#2E3580",
	"discovery":    "#2A3D7A",
	"config":       "#253A72",
	"preference":   "#22376B",
	"manual":       "#1F3464",
	// default (unknown types): falls through to badgeDefaultTint
}

const badgeDefaultTint = "#1C2E58"

// styles derived from defaultTheme. Built once at package init.
var (
	// Text styles.
	dimStyle   = lipgloss.NewStyle().Foreground(defaultTheme.dim)
	mutedStyle = lipgloss.NewStyle().Foreground(defaultTheme.muted)
	boldStyle  = lipgloss.NewStyle().Bold(true)

	// Header bar: full-width, accent left brand, dim right breadcrumb.
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(defaultTheme.accent)

	// Selected row: ▌ indicator + accent bold, no full-block inverse.
	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(defaultTheme.accent)

	// Status bar.
	statusBarStyle = lipgloss.NewStyle().Foreground(defaultTheme.dim)

	// Fuzzy chip: accent background, dark text.
	fuzzyChipStyle = lipgloss.NewStyle().
			Background(defaultTheme.accent).
			Foreground(lipgloss.AdaptiveColor{Dark: "#F0ECFF", Light: "#FFFFFF"}).
			Bold(true).
			Padding(0, 1)

	// Search bar border styles.
	searchBarActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(defaultTheme.accent).
				Padding(0, 1)

	searchBarIdleStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(defaultTheme.muted).
				Padding(0, 1)

	// Delete confirm.
	confirmStyle = lipgloss.NewStyle().Foreground(defaultTheme.danger).Bold(true)
)

// renderBadge renders a fixed-width type badge with a per-type tinted background
// and near-white text. The badge is padded/truncated to badgeInnerWidth characters.
const badgeInnerWidth = 12 // visible characters inside the badge (excludes border spacing)

func renderBadge(typeName string) string {
	bg, ok := badgeTints[typeName]
	if !ok {
		bg = badgeDefaultTint
	}
	label := truncStr(typeName, badgeInnerWidth)
	label = fmt.Sprintf("%-*s", badgeInnerWidth, label)
	return lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.AdaptiveColor{Dark: "#DDD6FE", Light: "#F5F3FF"}).
		Bold(true).
		Render(label)
}

// ─── layout constants ─────────────────────────────────────────────────────────

const (
	// headerRows is the number of lines consumed by the header bar.
	// 1 brand line + 1 separator line.
	headerRows = 2
	// statusRows is the number of lines consumed by status bar + footer.
	// 1 status line + 1 footer line — no leading blank; padding is injected
	// into the content area to pin chrome to the terminal bottom.
	statusRows = 2
	// searchBarRows is the number of content-area lines consumed by the
	// persistent search bar (border top + content + border bottom).
	searchBarRows = 3
	// minVisibleRows is the minimum number of list rows to show.
	minVisibleRows = 1
	// scrollMargin is the look-ahead margin kept when scrolling.
	scrollMargin = 2
	// leftPad is the consistent horizontal left padding for all content rows.
	leftPad = 2
	// rightPad is the right-side padding kept between right-aligned content and
	// the terminal edge.
	rightPad = 2
	// contentMaxWidth is the maximum column width for the content block.
	// On terminals wider than this, the content is horizontally centred so
	// columns don't scatter across an excessively wide screen.
	contentMaxWidth = 100
)

// contentOffset returns the number of extra leading spaces to prepend to
// every content row when the terminal is wider than contentMaxWidth.
// On narrow terminals (width ≤ contentMaxWidth) it returns 0, preserving the
// existing behaviour.
func contentOffset(termWidth int) int {
	if termWidth <= contentMaxWidth {
		return 0
	}
	offset := (termWidth - contentMaxWidth) / 2
	return offset
}

// effectiveWidth returns the column budget that content rows should fill.
// It is the minimum of the terminal width and contentMaxWidth, so layout
// calculations cap at contentMaxWidth on wide screens.
func effectiveWidth(termWidth int) int {
	if termWidth > contentMaxWidth {
		return contentMaxWidth
	}
	return termWidth
}

// ─── model ────────────────────────────────────────────────────────────────────

// Options configures optional TUI startup parameters.
type Options struct {
	// Version is displayed in the header bar. Defaults to "dev" when empty.
	Version string
}

// Model is the root Bubble Tea model for the ion-mem TUI dashboard.
type Model struct {
	store   *store.Store
	version string
	width   int
	height  int

	view viewState

	// Projects view.
	projects      []store.ProjectSummary
	projectCursor int
	projOffset    int // first visible index in the windowed projects list

	// Observations view.
	selectedProject string
	observations    []store.Observation
	obsCursor       int
	obsOffset       int // first visible index in the windowed observations list

	// Search (per-project observations view).
	searching    bool
	searchQuery  string
	fuzzyResults bool
	searchInput  textinput.Model

	// Global search (cross-project from projects view).
	globalSearching bool   // true while search input is focused in projects view
	globalQuery     string // last submitted global query

	// Detail view.
	selectedObs *store.Observation
	vp          viewport.Model

	// Delete confirm.
	confirmDelete bool

	// UI components.
	help   help.Model
	status string
	err    error
}

// newModel returns a zero-value Model ready to use without a real store
// (for unit tests). The store field is nil; production code uses New().
func newModel() Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 128

	return Model{
		searchInput: ti,
		help:        help.New(),
		view:        viewProjects,
		version:     "dev",
	}
}

// newModelWithOptions returns a model initialised with the given Options.
func newModelWithOptions(opts Options) Model {
	m := newModel()
	if opts.Version != "" {
		m.version = opts.Version
	}
	return m
}

// ─── windowing helpers ────────────────────────────────────────────────────────

// listVisibleHeight returns the number of list rows that fit in the current
// terminal height for the given view configuration.
//
// Two rows are always reserved for overflow markers (↑ more / ↓ more) so the
// worst-case layout (both markers visible) never pushes the output over the
// terminal height.
//
// Parameters:
//   - withSearchBar: subtract searchBarRows for the persistent search bar (observations view)
//   - withLogo: subtract logoHeight for the hero logo (projects view on tall terminals)
func (m Model) listVisibleHeight(withSearchBar, withLogo bool) int {
	const markerRows = 2 // reserve for ↑ more + ↓ more
	extra := 0
	if withSearchBar {
		extra += searchBarRows
	}
	if withLogo {
		extra += logoHeight
	}
	h := m.height - headerRows - statusRows - markerRows - extra
	if h < minVisibleRows {
		return minVisibleRows
	}
	return h
}

// showLogo returns true when the terminal is tall enough to display the hero logo.
func (m Model) showLogo() bool {
	return m.height >= logoMinTermHeight
}

// clampWindow adjusts offset so the invariant holds:
//
//	offset <= cursor < offset+visible
//
// A scrollMargin is applied: the window scrolls before the cursor actually
// reaches the edge (when the list is long enough to accommodate the margin).
func clampWindow(cursor, offset, visible, total int) int {
	if total == 0 || visible <= 0 {
		return 0
	}
	// Scroll down: cursor is too close to (or past) the bottom edge.
	if cursor >= offset+visible-scrollMargin && offset+visible < total {
		offset = cursor - visible + scrollMargin + 1
	}
	// Scroll up: cursor is too close to (or past) the top edge.
	if cursor < offset+scrollMargin && offset > 0 {
		offset = cursor - scrollMargin
	}
	// Hard clamps.
	if offset < 0 {
		offset = 0
	}
	if offset+visible > total {
		offset = total - visible
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// ─── Init ────────────────────────────────────────────────────────────────────

// Init starts the initial data fetch.
func (m Model) Init() tea.Cmd {
	if m.store == nil {
		return nil
	}
	return m.fetchProjects()
}

// ─── Update ──────────────────────────────────────────────────────────────────

// Update is the pure state-transition function for the Bubble Tea runtime.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Recompute viewport height for detail view.
		// Content area = height - headerRows - statusRows.
		// Inside the content area the metadata block uses at minimum 4 lines
		// (title, type/project/scope, created/updated, horizontal rule).
		// The viewport gets the remainder.
		const detailMetaLines = 4
		vpHeight := msg.Height - headerRows - statusRows - detailMetaLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.vp.Width = msg.Width
		m.vp.Height = vpHeight
		// Reclaim window after resize so cursor stays visible.
		m.projOffset = clampWindow(m.projectCursor, m.projOffset, m.listVisibleHeight(false, m.showLogo()), len(m.projects))
		m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(true, false), len(m.observations))
		return m, nil

	case projectsLoadedMsg:
		m.projects = msg.summaries
		if m.projectCursor >= len(m.projects) {
			m.projectCursor = 0
		}
		m.projOffset = 0
		return m, nil

	case observationsLoadedMsg:
		m.observations = msg.observations
		if msg.project != "" {
			m.selectedProject = msg.project
		}
		if m.obsCursor >= len(m.observations) {
			m.obsCursor = 0
		}
		m.obsOffset = 0
		m.confirmDelete = false
		return m, nil

	case searchResultMsg:
		m.observations = msg.results
		m.fuzzyResults = msg.fuzzy
		m.searching = false
		m.obsCursor = 0
		m.obsOffset = 0
		// Global search lands in viewGlobalSearch — preserve that state.
		// Per-project search stays in viewObservations (unchanged).
		return m, nil

	case searchSubmitMsg:
		m.searching = false
		return m, m.runSearch(msg.query)

	case deleteResultMsg:
		return m, m.fetchObservations(msg.project)

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Delegate to sub-components when searching (per-project or global).
	if m.searching || m.globalSearching {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	// Delegate scroll events to viewport in detail view.
	if m.view == viewDetail {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When in search input mode (observations view), handle Enter/Esc specially.
	if m.searching && m.view == viewObservations {
		switch {
		case key.Matches(msg, keys.Back):
			m.searching = false
			m.searchQuery = ""
			m.fuzzyResults = false
			return m, m.fetchObservations(m.selectedProject)
		case msg.Type == tea.KeyEnter:
			q := strings.TrimSpace(m.searchInput.Value())
			m.searchQuery = q
			m.searching = false
			if q == "" {
				return m, m.fetchObservations(m.selectedProject)
			}
			return m, m.runSearch(q)
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
	}

	// Global search input mode (projects view '/' → cross-project search).
	if m.globalSearching {
		switch {
		case key.Matches(msg, keys.Back):
			m.globalSearching = false
			m.globalQuery = ""
			m.searchInput.Reset()
			return m, nil
		case msg.Type == tea.KeyEnter:
			q := strings.TrimSpace(m.searchInput.Value())
			m.globalQuery = q
			m.globalSearching = false
			if q == "" {
				return m, nil
			}
			m.view = viewGlobalSearch
			m.obsCursor = 0
			m.obsOffset = 0
			m.fuzzyResults = false
			return m, m.runGlobalSearch(q)
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
	}

	// Delete confirm prompt.
	if m.confirmDelete {
		switch {
		case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'y':
			m.confirmDelete = false
			return m, m.doDelete()
		case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'n':
			m.confirmDelete = false
			return m, nil
		case key.Matches(msg, keys.Back):
			m.confirmDelete = false
			return m, nil
		}
		return m, nil
	}

	switch m.view {
	case viewProjects:
		return m.handleKeyProjects(msg)
	case viewObservations:
		return m.handleKeyObservations(msg)
	case viewDetail:
		return m.handleKeyDetail(msg)
	case viewGlobalSearch:
		return m.handleKeyGlobalSearch(msg)
	}
	return m, nil
}

func (m Model) handleKeyProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Up):
		if m.projectCursor > 0 {
			m.projectCursor--
			m.projOffset = clampWindow(m.projectCursor, m.projOffset, m.listVisibleHeight(false, m.showLogo()), len(m.projects))
		}
	case key.Matches(msg, keys.Down):
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			m.projOffset = clampWindow(m.projectCursor, m.projOffset, m.listVisibleHeight(false, m.showLogo()), len(m.projects))
		}
	case key.Matches(msg, keys.Enter):
		if len(m.projects) == 0 {
			return m, nil
		}
		m.selectedProject = m.projects[m.projectCursor].Project
		m.obsCursor = 0
		m.obsOffset = 0
		m.fuzzyResults = false
		m.view = viewObservations
		return m, m.fetchObservations(m.selectedProject)
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == '/':
		// Open global search (cross-project).
		m.globalSearching = true
		m.searchInput.Reset()
		m.searchInput.Placeholder = "Search all projects…"
		m.searchInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) handleKeyObservations(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.view = viewProjects
		m.searching = false
		m.searchQuery = ""
		m.fuzzyResults = false
	case key.Matches(msg, keys.Up):
		if m.obsCursor > 0 {
			m.obsCursor--
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(true, false), len(m.observations))
		}
	case key.Matches(msg, keys.Down):
		if m.obsCursor < len(m.observations)-1 {
			m.obsCursor++
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(true, false), len(m.observations))
		}
	case key.Matches(msg, keys.Enter):
		if len(m.observations) == 0 {
			return m, nil
		}
		obs := m.observations[m.obsCursor]
		m.selectedObs = &obs
		m.vp.SetContent(renderObservationDetail(obs))
		m.vp.GotoTop()
		m.view = viewDetail
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == '/':
		m.searching = true
		m.searchInput.Reset()
		m.searchInput.Placeholder = "Search memories… (press /)"
		m.searchInput.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.Delete):
		if len(m.observations) > 0 {
			m.confirmDelete = true
		}
	}
	return m, nil
}

func (m Model) handleKeyGlobalSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.view = viewProjects
		m.globalQuery = ""
		m.fuzzyResults = false
		m.observations = nil
		m.obsCursor = 0
		m.obsOffset = 0
	case key.Matches(msg, keys.Up):
		if m.obsCursor > 0 {
			m.obsCursor--
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(false, false), len(m.observations))
		}
	case key.Matches(msg, keys.Down):
		if m.obsCursor < len(m.observations)-1 {
			m.obsCursor++
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(false, false), len(m.observations))
		}
	case key.Matches(msg, keys.Enter):
		if len(m.observations) == 0 {
			return m, nil
		}
		obs := m.observations[m.obsCursor]
		m.selectedObs = &obs
		m.selectedProject = obs.Project
		m.vp.SetContent(renderObservationDetail(obs))
		m.vp.GotoTop()
		m.view = viewDetail
	}
	return m, nil
}

func (m Model) handleKeyDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.view = viewObservations
		m.selectedObs = nil
	case key.Matches(msg, keys.Delete):
		if m.selectedObs != nil {
			m.confirmDelete = true
		}
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ─── View ────────────────────────────────────────────────────────────────────

// View renders the current model state as a string.
func (m Model) View() string {
	switch m.view {
	case viewProjects:
		return m.viewProjects()
	case viewObservations:
		return m.viewObservations()
	case viewDetail:
		return m.viewDetail()
	case viewGlobalSearch:
		return m.viewGlobalSearchResults()
	}
	return ""
}

// renderHeader renders the branded top bar. Left side: "ion-mem vX.Y.Z",
// right side: breadcrumb with rightPad columns of right margin.
// Width-aware when m.width > 0.
func (m Model) renderHeader(breadcrumb string) string {
	brand := headerStyle.Render("ion-mem") + " " + dimStyle.Render(m.version)
	right := dimStyle.Render(breadcrumb)

	if m.width > 0 {
		brandLen := lipgloss.Width(brand)
		rightLen := lipgloss.Width(right)
		gap := m.width - brandLen - rightLen - rightPad
		if gap < 1 {
			gap = 1
		}
		return brand + strings.Repeat(" ", gap) + right + strings.Repeat(" ", rightPad)
	}
	return brand + "  " + right
}

// renderLogoHero renders the ION MEM ASCII logo with a vertical gradient and
// a dim tagline. Returns a string with exactly logoHeight lines (each
// terminated by "\n"). Callers must subtract logoHeight from the content budget.
func (m Model) renderLogoHero(version string) string {
	var sb strings.Builder

	// Use the effective width for centering so the logo aligns with the rest of
	// the content block on wide terminals.
	w := effectiveWidth(m.width)
	if w < 1 {
		w = 80
	}
	// The centering offset shifts the whole block right on extra-wide terminals.
	offset := contentOffset(m.width)

	// Render each art row with its gradient color.
	for i, row := range logoRows {
		// Empty art rows are rendered as plain blank lines — no ANSI codes —
		// to avoid styled-blank artifacts in real terminals where some emulators
		// paint the rest of the line in the foreground colour.
		if row == "" {
			sb.WriteString("\n")
			continue
		}
		colorIdx := i
		if colorIdx >= len(logoGradient) {
			colorIdx = len(logoGradient) - 1
		}
		col := lipgloss.Color(logoGradient[colorIdx])
		styled := lipgloss.NewStyle().Foreground(col).Render(row)
		// Center within the effective content block.
		rawWidth := lipgloss.Width(styled)
		pad := (w - rawWidth) / 2
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(strings.Repeat(" ", offset+pad) + styled + "\n")
	}

	// Tagline line (1 line) — centred within the content block.
	tagline := dimStyle.Render("Persistent memory for AI coding agents — " + version)
	tagRaw := lipgloss.Width(tagline)
	tagPad := (w - tagRaw) / 2
	if tagPad < 0 {
		tagPad = 0
	}
	sb.WriteString(strings.Repeat(" ", offset+tagPad) + tagline + "\n")

	// Blank spacer between tagline and the first list row (Bug 3 fix).
	sb.WriteString("\n")

	return sb.String()
}

// renderSearchBar renders the persistent styled search bar for the observations view.
// It returns a string with exactly searchBarRows lines (each terminated by "\n").
// When focused (searching=true), the border is accent-colored and shows live input.
// When idle, the border is muted and shows a dim placeholder prompt.
func (m Model) renderSearchBar() string {
	w := m.width
	if w < 20 {
		w = 20
	}
	// Inner width: full width minus border (2 cols) minus padding (2 each side = 4).
	innerW := w - 6
	if innerW < 5 {
		innerW = 5
	}

	var inner string
	if m.searching {
		// Live input — show "/" glyph + textinput.
		prompt := headerStyle.Render("/") + " " + m.searchInput.View()
		inner = prompt
	} else if m.searchQuery != "" {
		// Show submitted query in dim style.
		inner = dimStyle.Render("/") + " " + dimStyle.Render(m.searchQuery)
	} else {
		// Idle placeholder.
		inner = dimStyle.Render("/ Search memories… (press /)")
	}

	style := searchBarIdleStyle
	if m.searching {
		style = searchBarActiveStyle
	}
	// Force the box to span full width.
	rendered := style.Width(innerW).Render(inner)
	return rendered + "\n"
}

// renderSeparator renders a full-width horizontal rule in muted style.
// This acts as the visual divider below the header bar.
func (m Model) renderSeparator() string {
	w := m.width
	if w < 1 {
		w = 40
	}
	return mutedStyle.Render(strings.Repeat("─", w))
}

// padContentArea returns a string that renders as exactly targetRows lines when
// split on "\n". The input content may end with a newline or not.
//
// The caller composes the final view as:
//
//	padContentArea(content, rows) + "\n" + statusLine + "\n" + footerLine + "\n"
//
// so the return value must NOT carry a trailing newline — the caller's "\n"
// after padContentArea becomes the last newline of the padded content region.
//
// Padding is computed as:
//   - count lines already in content (each "\n" terminates one line)
//   - if content does not end in "\n", the partial last chunk counts as one line
//   - append blank lines until line count == targetRows
//   - strip exactly one trailing "\n" so the caller's join "\n" re-terminates it
func padContentArea(content string, targetRows int) string {
	if targetRows <= 0 {
		return strings.TrimSuffix(content, "\n")
	}
	// Each "\n" terminates a line.
	n := strings.Count(content, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		// Last partial line has no terminating newline; it still occupies one row.
		n++
		// Normalise: give it a newline so the padding loop is uniform.
		content += "\n"
	}
	// Append blank lines until we have targetRows terminated lines.
	for n < targetRows {
		content += "\n"
		n++
	}
	// The caller appends "\n" after padContentArea (as the join character
	// between padded content and statusLine), so strip the last "\n" here to
	// avoid an off-by-one blank line.
	return strings.TrimSuffix(content, "\n")
}

// renderFooter renders the key-hint footer in dim style.
func (m Model) renderFooter() string {
	return mutedStyle.Render(m.help.ShortHelpView(keys.ShortHelp()))
}

// positionIndicator returns "cursor+1/total" or empty when the list is empty.
func positionIndicator(cursor, total int) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", cursor+1, total)
}

// overflowMarkers returns (showUp, showDown) based on whether there is content
// above or below the current window.
func overflowMarkers(offset, visible, total int) (bool, bool) {
	showUp := offset > 0
	showDown := offset+visible < total
	return showUp, showDown
}

func (m Model) viewProjects() string {
	// ── chrome ──────────────────────────────────────────────────────────────
	header := m.renderHeader("Projects")
	separator := m.renderSeparator()

	// Wide-terminal layout helpers: content is capped at contentMaxWidth and
	// centred horizontally when the terminal is wider.
	cOffset := contentOffset(m.width) // extra left indent for centering
	cWidth := effectiveWidth(m.width) // column budget for content rows
	if cWidth < 40 {
		cWidth = 40
	}

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Hero logo: shown on tall terminals only.
	logo := m.showLogo()
	if logo {
		content.WriteString(m.renderLogoHero(m.version))
	}

	// Global search overlay: when '/' is pressed in projects view, render an
	// accent search bar at the top of the content area (above the project list).
	if m.globalSearching {
		// Render input in the same styled box as the observations search bar.
		innerW := cWidth - 6
		if innerW < 5 {
			innerW = 5
		}
		prompt := headerStyle.Render("/") + " " + m.searchInput.View()
		box := searchBarActiveStyle.Width(innerW).Render(prompt)
		content.WriteString(strings.Repeat(" ", cOffset) + box + "\n")
	}

	rowIndent := strings.Repeat(" ", cOffset+leftPad) // indent for ordinary content rows

	if len(m.projects) == 0 {
		msg := dimStyle.Render("No projects yet — memories will appear as agents save them.")
		if m.width > 0 {
			msg = lipgloss.NewStyle().Width(cWidth).Align(lipgloss.Center).Foreground(defaultTheme.dim).Render(
				"No projects yet — memories will appear as agents save them.",
			)
		}
		content.WriteString(strings.Repeat(" ", cOffset) + msg + "\n")
	} else {
		visible := m.listVisibleHeight(false, logo)
		offset := m.projOffset
		total := len(m.projects)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(rowIndent + mutedStyle.Render("↑ more") + "\n")
		}

		end := offset + visible
		if end > total {
			end = total
		}

		// Compute right-aligned activity column width.
		activityWidth := 10 // "26d ago" max ~10 chars
		nameWidth := 28

		for i := offset; i < end; i++ {
			p := m.projects[i]
			name := truncStr(p.Project, nameWidth)
			counts := fmt.Sprintf("%4d obs  %3d sessions", p.ObservationCount, p.SessionCount)
			activityStr := humanizeTime(p.LastActivity)

			// Right-align the activity column within the effective content width.
			var row string
			if m.width > 0 {
				left := strings.Repeat(" ", cOffset+leftPad)
				if i == m.projectCursor {
					left = strings.Repeat(" ", cOffset) + selectedRowStyle.Render("▌") + " "
				}
				nameFmt := fmt.Sprintf("%-*s", nameWidth, name)
				countsFmt := counts
				// Gap fills the space between counts and the right-aligned activity
				// column, calculated against the effective content width.
				leftRaw := leftPad + nameWidth + 2 + len(counts) + 2
				gap := cWidth - leftRaw - activityWidth - rightPad
				if gap < 1 {
					gap = 1
				}
				if i == m.projectCursor {
					nameStr := selectedRowStyle.Render(nameFmt)
					countsStr := boldStyle.Render(countsFmt)
					actStr := dimStyle.Render(fmt.Sprintf("%-*s", activityWidth, activityStr))
					row = left + nameStr + "  " + countsStr + strings.Repeat(" ", gap) + actStr
				} else {
					actStr := dimStyle.Render(fmt.Sprintf("%-*s", activityWidth, activityStr))
					row = left + nameFmt + "  " + countsFmt + strings.Repeat(" ", gap) + actStr
				}
			} else {
				if i == m.projectCursor {
					indicator := selectedRowStyle.Render("▌ ")
					nameStr := selectedRowStyle.Render(fmt.Sprintf("%-*s", nameWidth, name))
					countsStr := boldStyle.Render(counts)
					actStr := dimStyle.Render(activityStr)
					row = indicator + nameStr + "  " + countsStr + "  " + actStr
				} else {
					row = strings.Repeat(" ", leftPad) + fmt.Sprintf("%-*s", nameWidth, name) + "  " + counts + "  " + dimStyle.Render(activityStr)
				}
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(rowIndent + mutedStyle.Render("↓ more") + "\n")
		}
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.projectCursor, len(m.projects))
	statusLeft := fmt.Sprintf("%d project(s)", len(m.projects))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "error: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft)

	var footerHints []key.Binding
	if m.globalSearching {
		footerHints = []key.Binding{keys.Back, keys.Enter}
	} else {
		footerHints = keys.ShortHelp()
	}
	footerLine := strings.Repeat(" ", cOffset+leftPad) + mutedStyle.Render(m.help.ShortHelpView(footerHints))

	// ── compose full-height layout ───────────────────────────────────────────
	// Content area height = terminal height − header − separator − status − footer.
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

func (m Model) viewObservations() string {
	// ── chrome ──────────────────────────────────────────────────────────────
	breadcrumb := "Projects › " + m.selectedProject
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Persistent search bar — always visible between header and list.
	content.WriteString(m.renderSearchBar())

	if len(m.observations) == 0 {
		var emptyMsg string
		if m.searchQuery != "" {
			emptyMsg = fmt.Sprintf("No results for %q.", m.searchQuery)
		} else {
			emptyMsg = "No observations yet."
		}
		content.WriteString(strings.Repeat(" ", leftPad) + dimStyle.Render(emptyMsg) + "\n")
	} else {
		visible := m.listVisibleHeight(true, false)
		offset := m.obsOffset
		total := len(m.observations)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(strings.Repeat(" ", leftPad) + mutedStyle.Render("↑ more") + "\n")
		}

		// Badge: badgeInnerWidth chars of rendered content.
		// The lipgloss badge adds background/foreground escape codes, so we use
		// lipgloss.Width() for layout math rather than raw string length.
		badgeRenderedWidth := badgeInnerWidth // visual columns consumed by badge (no border)
		ageWidth := 10
		titleWidth := 50
		if m.width > 40 {
			// leftPad + badgeRenderedWidth + 1 (space) + titleWidth + gap + ageWidth + rightPad = width
			titleWidth = m.width - leftPad - badgeRenderedWidth - 1 - ageWidth - rightPad
			if titleWidth < 10 {
				titleWidth = 10
			}
		}

		end := offset + visible
		if end > total {
			end = total
		}
		for i := offset; i < end; i++ {
			obs := m.observations[i]
			badge := renderBadge(obs.Type)
			ageStr := humanizeTime(parseCreatedAt(obs.CreatedAt))
			title := truncStr(obs.Title, titleWidth)

			// Right-align age: pad so age ends at width-rightPad.
			var row string
			if m.width > 0 {
				titleFmt := fmt.Sprintf("%-*s", titleWidth, title)
				// Compute gap between title and age to right-align age.
				// raw chars used so far: leftPad + badgeRenderedWidth + 1 (space) + titleWidth
				usedLeft := leftPad + badgeRenderedWidth + 1 + titleWidth
				gap := m.width - usedLeft - len(ageStr) - rightPad
				if gap < 1 {
					gap = 1
				}
				ageRendered := dimStyle.Render(ageStr)
				if i == m.obsCursor {
					indicator := selectedRowStyle.Render("▌") + " "
					titleStr := lipgloss.NewStyle().Bold(true).Foreground(defaultTheme.accent).Render(titleFmt)
					// Indicator occupies leftPad chars (▌ + space).
					row = indicator + badge + " " + titleStr + strings.Repeat(" ", gap) + ageRendered
				} else {
					row = strings.Repeat(" ", leftPad) + badge + " " + titleFmt + strings.Repeat(" ", gap) + ageRendered
				}
			} else {
				age := dimStyle.Render(ageStr)
				if i == m.obsCursor {
					indicator := selectedRowStyle.Render("▌ ")
					titleStr := lipgloss.NewStyle().Bold(true).Foreground(defaultTheme.accent).Render(fmt.Sprintf("%-*s", titleWidth, title))
					row = indicator + badge + " " + titleStr + " " + age
				} else {
					row = strings.Repeat(" ", leftPad) + badge + " " + fmt.Sprintf("%-*s", titleWidth, title) + " " + age
				}
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(strings.Repeat(" ", leftPad) + mutedStyle.Render("↓ more") + "\n")
		}
	}

	if m.confirmDelete {
		content.WriteString("\n" + strings.Repeat(" ", leftPad) + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.obsCursor, len(m.observations))
	statusLeft := m.selectedProject + " — " + fmt.Sprintf("%d observation(s)", len(m.observations))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "error: " + m.err.Error()
	}

	// Build status line: left context + optional fuzzy chip right-aligned.
	var statusLine string
	if m.searchQuery != "" && m.fuzzyResults {
		chip := fuzzyChipStyle.Render("~fuzzy")
		leftPart := strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusLeft+fmt.Sprintf("  %q", m.searchQuery))
		if m.width > 0 {
			leftW := lipgloss.Width(leftPart)
			chipW := lipgloss.Width(chip)
			gap := m.width - leftW - chipW - rightPad
			if gap < 1 {
				gap = 1
			}
			statusLine = leftPart + strings.Repeat(" ", gap) + chip
		} else {
			statusLine = leftPart + "  " + chip
		}
	} else if m.searchQuery != "" {
		statusLine = strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusLeft+fmt.Sprintf("  %q", m.searchQuery))
	} else {
		statusLine = strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusLeft)
	}

	footerLine := strings.Repeat(" ", leftPad) + m.renderFooter()

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

// viewGlobalSearchResults renders search results across all projects.
// It reuses the observations list layout but adds a dim "project" column.
func (m Model) viewGlobalSearchResults() string {
	query := m.globalQuery
	// ── chrome ──────────────────────────────────────────────────────────────
	breadcrumb := fmt.Sprintf(`Projects › search %q`, query)
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	if len(m.observations) == 0 {
		var emptyMsg string
		if query != "" {
			emptyMsg = fmt.Sprintf("No results for %q.", query)
		} else {
			emptyMsg = "No results."
		}
		content.WriteString(strings.Repeat(" ", leftPad) + dimStyle.Render(emptyMsg) + "\n")
	} else {
		visible := m.listVisibleHeight(false, false)
		offset := m.obsOffset
		total := len(m.observations)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(strings.Repeat(" ", leftPad) + mutedStyle.Render("↑ more") + "\n")
		}

		// Column layout: badge | title | project (dim) | age
		badgeW := badgeInnerWidth
		projColW := 14
		ageWidth := 10
		titleWidth := 30
		if m.width > 60 {
			titleWidth = m.width - leftPad - badgeW - 1 - projColW - 2 - ageWidth - rightPad
			if titleWidth < 10 {
				titleWidth = 10
			}
		}

		end := offset + visible
		if end > total {
			end = total
		}
		for i := offset; i < end; i++ {
			obs := m.observations[i]
			badge := renderBadge(obs.Type)
			ageStr := humanizeTime(parseCreatedAt(obs.CreatedAt))
			title := truncStr(obs.Title, titleWidth)
			proj := dimStyle.Render(truncStr(obs.Project, projColW))

			titleFmt := fmt.Sprintf("%-*s", titleWidth, title)

			usedLeft := leftPad + badgeW + 1 + titleWidth + 2 + projColW
			gap := m.width - usedLeft - len(ageStr) - rightPad
			if gap < 1 {
				gap = 1
			}
			ageRendered := dimStyle.Render(ageStr)
			var row string
			if i == m.obsCursor {
				indicator := selectedRowStyle.Render("▌") + " "
				titleStr := lipgloss.NewStyle().Bold(true).Foreground(defaultTheme.accent).Render(titleFmt)
				row = indicator + badge + " " + titleStr + "  " + proj + strings.Repeat(" ", gap) + ageRendered
			} else {
				row = strings.Repeat(" ", leftPad) + badge + " " + titleFmt + "  " + proj + strings.Repeat(" ", gap) + ageRendered
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(strings.Repeat(" ", leftPad) + mutedStyle.Render("↓ more") + "\n")
		}
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.obsCursor, len(m.observations))
	statusLeft := fmt.Sprintf("all projects — %d result(s)", len(m.observations))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "error: " + m.err.Error()
	}

	var statusLine string
	if m.fuzzyResults {
		chip := fuzzyChipStyle.Render("~fuzzy")
		leftPart := strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusLeft)
		if m.width > 0 {
			leftW := lipgloss.Width(leftPart)
			chipW := lipgloss.Width(chip)
			gap := m.width - leftW - chipW - rightPad
			if gap < 1 {
				gap = 1
			}
			statusLine = leftPart + strings.Repeat(" ", gap) + chip
		} else {
			statusLine = leftPart + "  " + chip
		}
	} else {
		statusLine = strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusLeft)
	}

	footerLine := strings.Repeat(" ", leftPad) + m.renderFooter()

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

func (m Model) viewDetail() string {
	if m.selectedObs == nil {
		return ""
	}
	obs := m.selectedObs

	// ── chrome ──────────────────────────────────────────────────────────────
	breadcrumb := "Projects › " + m.selectedProject + " › detail"
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Metadata block — label:value grid with dim labels.
	// Count lines precisely; viewport height is set in Update's WindowSizeMsg.
	labelStyle := lipgloss.NewStyle().Foreground(defaultTheme.dim).Width(10)
	metaLines := 0

	// Title row (full accent bold).
	content.WriteString(strings.Repeat(" ", leftPad) + selectedRowStyle.Render(obs.Title) + "\n")
	metaLines++

	// Type + badge inline.
	content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("type") + renderBadge(obs.Type) + "\n")
	metaLines++

	// project / scope on same line.
	content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("project") + dimStyle.Render(obs.Project) + "   " + labelStyle.Render("scope") + dimStyle.Render(obs.Scope) + "\n")
	metaLines++

	if obs.TopicKey != nil && *obs.TopicKey != "" {
		content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("topic_key") + dimStyle.Render(*obs.TopicKey) + "\n")
		metaLines++
	}
	if obs.SyncID != "" {
		content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("sync_id") + mutedStyle.Render(obs.SyncID) + "\n")
		metaLines++
	}
	content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("created") + dimStyle.Render(obs.CreatedAt) + "\n")
	metaLines++
	content.WriteString(strings.Repeat(" ", leftPad) + labelStyle.Render("updated") + dimStyle.Render(obs.UpdatedAt) + "\n")
	metaLines++

	// Horizontal rule inside the content area.
	ruleWidth := m.width
	if ruleWidth < 1 {
		ruleWidth = 40
	}
	content.WriteString(mutedStyle.Render(strings.Repeat("─", ruleWidth)) + "\n")
	metaLines++ // rule counts as one content line

	_ = metaLines // used only for documentation; viewport height is set in Update

	content.WriteString(m.vp.View())

	if m.confirmDelete {
		content.WriteString("\n" + strings.Repeat(" ", leftPad) + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	statusText := fmt.Sprintf("observation #%d", obs.ID)
	if m.vp.TotalLineCount() > 0 {
		statusText += fmt.Sprintf("  %.0f%%", m.vp.ScrollPercent()*100)
	} else {
		statusText += "  scroll ↑↓"
	}
	if m.err != nil {
		statusText = "error: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", leftPad) + statusBarStyle.Render(statusText)
	footerLine := strings.Repeat(" ", leftPad) + m.renderFooter()

	// ── compose full-height layout ───────────────────────────────────────────
	contentRows := m.height - headerRows - statusRows
	if contentRows < 1 {
		contentRows = 1
	}
	paddedContent := padContentArea(content.String(), contentRows)

	return header + "\n" +
		separator + "\n" +
		paddedContent + "\n" +
		statusLine + "\n" +
		footerLine + "\n"
}

// ─── commands ────────────────────────────────────────────────────────────────

func (m Model) fetchProjects() tea.Cmd {
	st := m.store
	return func() tea.Msg {
		summaries, err := st.ProjectSummaries(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return projectsLoadedMsg{summaries: summaries}
	}
}

func (m Model) fetchObservations(project string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	return func() tea.Msg {
		obs, err := st.RecentObservations(context.Background(), store.RecentObservationsParams{
			Project: project,
			Limit:   50,
		})
		if err != nil {
			return errMsg{err}
		}
		return observationsLoadedMsg{observations: obs, project: project}
	}
}

func (m Model) runSearch(query string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	project := m.selectedProject
	return func() tea.Msg {
		results, fuzzy, err := st.SearchWithFallback(context.Background(), store.SearchParams{
			Q:       query,
			Project: project,
			Limit:   50,
		})
		if err != nil {
			return errMsg{err}
		}
		obs := make([]store.Observation, 0, len(results))
		for _, r := range results {
			obs = append(obs, r.Observation)
		}
		return searchResultMsg{results: obs, fuzzy: fuzzy}
	}
}

// runGlobalSearch performs a cross-project search (Project field is empty,
// which means all projects per store.SearchParams semantics).
func (m Model) runGlobalSearch(query string) tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	return func() tea.Msg {
		results, fuzzy, err := st.SearchWithFallback(context.Background(), store.SearchParams{
			Q:     query,
			Limit: 50,
			// Project intentionally empty → all projects.
		})
		if err != nil {
			return errMsg{err}
		}
		obs := make([]store.Observation, 0, len(results))
		for _, r := range results {
			obs = append(obs, r.Observation)
		}
		return searchResultMsg{results: obs, fuzzy: fuzzy}
	}
}

func (m Model) doDelete() tea.Cmd {
	if m.store == nil || len(m.observations) == 0 {
		return nil
	}
	var id int64
	project := m.selectedProject
	if m.view == viewDetail && m.selectedObs != nil {
		id = m.selectedObs.ID
	} else if m.obsCursor < len(m.observations) {
		id = m.observations[m.obsCursor].ID
	} else {
		return nil
	}
	st := m.store
	return func() tea.Msg {
		if err := st.DeleteObservation(context.Background(), id, false); err != nil {
			return errMsg{err}
		}
		return deleteResultMsg{project: project}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func humanizeTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func parseCreatedAt(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}

func renderObservationDetail(obs store.Observation) string {
	var sb strings.Builder
	sb.WriteString(obs.Content)
	return sb.String()
}
