package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ionix/ion-mem/internal/hybrid"
	"github.com/ionix/ion-mem/internal/store"
)

// ─── view states ──────────────────────────────────────────────────────────────

type viewState int

const (
	viewProjects viewState = iota
	viewObservations
	viewDetail
	viewGlobalSearch // cross-project search results — observations list with project column
	viewConfig       // embeddings / Ollama settings
)

// ─── config row indices ───────────────────────────────────────────────────────

const (
	configRowEmbeddings   = 0
	configRowOllamaURL    = 1
	configRowModel        = 2
	configRowTestConn     = 3
	configRowEmbedMissing = 4 // EMBED MISSING — incremental backfill, skips already-embedded
	configRowRegen        = 5 // REGENERATE EMBEDDINGS — wipes then re-embeds all
	configRowCount        = 6
)

// ─── embed job message ────────────────────────────────────────────────────────

// embedJobBatchMsg is sent after each batch of the chained embed job (both
// EMBED MISSING and REGENERATE). The engine issues the next batch command on
// receipt unless finished or aborted is true.
type embedJobBatchMsg struct {
	done     int
	total    int
	lastErr  error
	finished bool // no more missing rows — job complete
	aborted  bool // zero-progress guard fired — stop loop
}

// jobKind distinguishes which operation is currently running, used to select
// the correct progress-bar label (EMBEDDING vs REGENERATING).
type jobKind int

const (
	jobKindEmbed jobKind = iota // EMBED MISSING
	jobKindRegen                // REGENERATE EMBEDDINGS
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

// searchResultMsg is sent when a search completes (BM25 or hybrid).
type searchResultMsg struct {
	results []store.Observation
	fuzzy   bool
	hybrid  bool
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

// configSettingsLoadedMsg carries the settings values read from the store on
// config view entry.
type configSettingsLoadedMsg struct {
	embeddingsEnabled bool
	ollamaURL         string
	model             string
}

// configSaveSettingMsg signals the result of a setting persistence attempt.
// err is non-nil when the store write failed.
type configSaveSettingMsg struct {
	err error
}

// configProbeResultMsg carries the result of a TEST CONNECTION probe.
type configProbeResultMsg struct {
	ok   bool
	info string // human-readable summary or error message
}

// configCoverageLoadedMsg carries the embedding coverage counts for the config
// view's idle COVERAGE row. Sent when the config view loads settings.
type configCoverageLoadedMsg struct {
	have  int
	total int
}

// configRegenResultMsg carries the result of a REGENERATE EMBEDDINGS operation.
type configRegenResultMsg struct {
	ok   bool
	info string // human-readable summary or error message
}

// ─── key bindings ──────────────────────────────────────────────────────────────

// bbsFooter renders the BBS-style key legend. It is context-sensitive:
// the observations and detail views include the Delete action.
const (
	bbsFooterBase   = "[↑↓] MOVE  [⏎] OPEN  [/] SEARCH  [C] CONFIG  [ESC] BACK  [Q] QUIT"
	bbsFooterDelete = "[↑↓] MOVE  [⏎] OPEN  [/] SEARCH  [D] DELETE  [ESC] BACK  [Q] QUIT"
	bbsFooterDetail = "[↑↓] SCROLL  [D] DELETE  [ESC] BACK  [Q] QUIT"
	bbsFooterSearch = "[⏎] SUBMIT  [ESC] CANCEL"
	bbsFooterConfig = "[↑↓] MOVE  [⏎] EDIT/RUN  [ESC] BACK  [Q] QUIT"
)

// ─── theme ────────────────────────────────────────────────────────────────────

// theme is the single source of truth for all visual styling. Colors use
// AdaptiveColor so the palette works on both light and dark terminals.
type theme struct {
	accent  lipgloss.AdaptiveColor
	dim     lipgloss.AdaptiveColor
	danger  lipgloss.AdaptiveColor
	muted   lipgloss.AdaptiveColor
	surface lipgloss.AdaptiveColor
	amber   lipgloss.AdaptiveColor // warm secondary for bugfix badge
}

var defaultTheme = theme{
	accent:  lipgloss.AdaptiveColor{Dark: "#D22B44", Light: "#8C1C2C"},
	dim:     lipgloss.AdaptiveColor{Dark: "#626262", Light: "#888888"},
	danger:  lipgloss.AdaptiveColor{Dark: "#FF6B3D", Light: "#C2410C"},
	muted:   lipgloss.AdaptiveColor{Dark: "#444444", Light: "#BBBBBB"},
	surface: lipgloss.AdaptiveColor{Dark: "#1C1C1C", Light: "#F5F5F5"},
	amber:   lipgloss.AdaptiveColor{Dark: "#E8A000", Light: "#C07000"},
}

// badgeForegrounds maps observation types to foreground colors within the
// burgundy-to-rose accent family. Text rendered over the terminal default
// background. [BUG  ] uses the warm amber secondary.
var badgeForegrounds = map[string]lipgloss.TerminalColor{
	"decision":        lipgloss.AdaptiveColor{Dark: "#F5A0AB", Light: "#8C1C2C"},
	"architecture":    lipgloss.AdaptiveColor{Dark: "#E86274", Light: "#771125"},
	"bugfix":          defaultTheme.amber,
	"discovery":       lipgloss.AdaptiveColor{Dark: "#D22B44", Light: "#8C1C2C"},
	"config":          lipgloss.AdaptiveColor{Dark: "#B01A33", Light: "#D22B44"},
	"preference":      lipgloss.AdaptiveColor{Dark: "#92152C", Light: "#E86274"},
	"pattern":         lipgloss.AdaptiveColor{Dark: "#DC4A5C", Light: "#5C0E1D"},
	"session_summary": lipgloss.AdaptiveColor{Dark: "#EE8C99", Light: "#5C0E1D"},
	"manual":          lipgloss.AdaptiveColor{Dark: "#D22B44", Light: "#8C1C2C"},
}

// badgeLabels maps type names to their 5-char uppercase BBS badge label.
// The badge format is [XXXXX] — bracket + 5 chars + bracket = 7 chars visible.
var badgeLabels = map[string]string{
	"decision":        "DECID",
	"architecture":    "ARCH ",
	"bugfix":          "BUG  ",
	"discovery":       "DISCO",
	"config":          "CONF ",
	"preference":      "PREF ",
	"pattern":         "PATRN",
	"session_summary": "SESSN",
	"manual":          "NOTE ",
}

// badgeVisibleWidth is the total visible width of a rendered badge in columns.
// Format: "[" + 5 label chars + "]" = 7 columns.
const badgeVisibleWidth = 7

// styles derived from defaultTheme. Built once at package init.
var (
	// Text styles.
	dimStyle   = lipgloss.NewStyle().Foreground(defaultTheme.dim)
	mutedStyle = lipgloss.NewStyle().Foreground(defaultTheme.muted)
	boldStyle  = lipgloss.NewStyle().Bold(true)

	// Header bar: brand left, accent bold uppercase.
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(defaultTheme.accent)

	// Selected row: full-row inverse video — accent background, dark foreground.
	// No ▌ glyph; the row background itself is the selection indicator.
	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Background(defaultTheme.accent).
				Foreground(lipgloss.AdaptiveColor{Dark: "#1A0407", Light: "#FFFFFF"})

	// Status bar: inverse-video strip across content width.
	statusBarStyle = lipgloss.NewStyle().
			Foreground(defaultTheme.dim)

	// Fuzzy chip: accent background, dark text — uppercase ~FUZZY.
	fuzzyChipStyle = lipgloss.NewStyle().
			Background(defaultTheme.accent).
			Foreground(lipgloss.AdaptiveColor{Dark: "#FFEDEE", Light: "#FFFFFF"}).
			Bold(true).
			Padding(0, 1)

	// Hybrid chip: same accent family as fuzzy — uppercase ~HYBRID.
	hybridChipStyle = lipgloss.NewStyle().
			Background(defaultTheme.accent).
			Foreground(lipgloss.AdaptiveColor{Dark: "#FFEDEE", Light: "#FFFFFF"}).
			Bold(true).
			Padding(0, 1)

	// Delete confirm.
	confirmStyle = lipgloss.NewStyle().Foreground(defaultTheme.danger).Bold(true)

	// Search bar double-border: accent when focused, muted when idle.
	searchBarActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(defaultTheme.accent).
				Padding(0, 1)

	searchBarIdleStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(defaultTheme.muted).
				Padding(0, 1)
)

// renderBadge renders a fixed-width type badge in the retro BBS format:
// "[XXXXX]" — bracket + 5-char uppercase label + bracket.
// Total visible width is always badgeVisibleWidth (7) characters.
// Color is foreground-only (no background block) within the accent family.
func renderBadge(typeName string) string {
	label, ok := badgeLabels[typeName]
	if !ok {
		// Unknown type: use first 5 chars of name uppercased, padded to 5.
		l := strings.ToUpper(typeName)
		if len(l) > 5 {
			l = l[:5]
		} else {
			l = fmt.Sprintf("%-5s", l)
		}
		label = l
	}
	fg, hasFG := badgeForegrounds[typeName]
	var inner string
	if hasFG {
		inner = lipgloss.NewStyle().Foreground(fg).Bold(true).Render(label)
	} else {
		inner = lipgloss.NewStyle().Foreground(defaultTheme.accent).Bold(true).Render(label)
	}
	return "[" + inner + "]"
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
	// persistent search bar (double-border top + content + border bottom).
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
	searching     bool
	searchQuery   string
	fuzzyResults  bool
	hybridResults bool // true when last search used hybrid RRF fusion
	searchInput   textinput.Model

	// Global search (cross-project from projects view).
	globalSearching bool   // true while search input is focused in projects view
	globalQuery     string // last submitted global query

	// Detail view.
	selectedObs *store.Observation
	vp          viewport.Model

	// Delete confirm.
	confirmDelete bool

	// Config view.
	configCursor            int    // which settings row is selected
	configEditing           bool   // true when inline text input is open
	configEmbeddingsEnabled bool   // current toggle value (not persisted until changed)
	configOllamaURL         string // current URL value
	configModel             string // current model name
	configTesting           bool   // true while TEST CONNECTION probe is in flight
	configTestResult        string // last probe result (empty = none yet)
	configTestOK            bool   // true if last probe succeeded
	configEditOrig          string // original value before editing (for Esc cancel)
	configInput             textinput.Model

	// configSaveErr holds the most recent error from saveConfigSetting.
	// Non-nil when the last setting persistence attempt failed. Cleared on
	// a subsequent successful save.
	configSaveErr error

	// Embedding coverage counts for the idle COVERAGE row in the config view.
	// Populated by configCoverageLoadedMsg on config view entry.
	configCoverageHave  int
	configCoverageTotal int

	// saveFn is the injectable setting-persistence function. In production it
	// is nil and saveConfigSetting falls back to st.SetSetting. In tests it can
	// be replaced with a stub that returns a controlled error.
	saveFn func(key, value string) error

	// probeFn is the function used to run the Ollama connection test. In
	// production it creates an embed.Client and calls Ping/HasModel/ProbeEmbed.
	// In unit tests it is replaced with a stub that returns a fake result
	// without needing a real server.
	probeFn func(url, model string) tea.Cmd

	// regenFn is the legacy single-shot REGENERATE function kept for backward
	// compatibility with unit tests that stub it directly. Production code uses
	// regenBatchFn / the chained engine instead.
	regenFn func(url, model string) tea.Cmd

	// configRegenerating is kept to prevent breaking existing tests that check
	// this field directly. It mirrors jobRunning when the active job is regen.
	configRegenerating bool
	// configRegenResult is kept for existing tests; mirrors jobResult for regen jobs.
	configRegenResult string
	// configRegenOK is kept for existing tests; mirrors jobResultOK for regen jobs.
	configRegenOK bool

	// ─── chained embed job state ─────────────────────────────────────────────

	// jobRunning is true while an EMBED MISSING or REGENERATE batch loop is in
	// flight. Both rows are blocked (no re-trigger) while true.
	jobRunning bool
	// jobDone is the cumulative count of successfully embedded observations in
	// the current job.
	jobDone int
	// jobTotal is the total number of observations (embeddings target) for the
	// current job.
	jobTotal int
	// jobKind distinguishes EMBED MISSING from REGENERATE for the progress bar label.
	jobKind jobKind
	// jobResult is the human-readable summary shown when the job finishes.
	jobResult string
	// jobResultOK is true when the job finished without errors.
	jobResultOK bool

	// embedBatchFn is the injectable batch function for EMBED MISSING. In
	// production it is nil and makeDefaultEmbedBatchFn() is used. In tests it is
	// replaced with a stub so no Ollama is required.
	//
	// Signature: func(url, modelName string, offset, batchSize int) tea.Cmd
	// The offset parameter is reserved for future pagination; the current
	// implementation passes 0 and relies on MissingEmbeddings to return the
	// next unembedded page.
	embedBatchFn func(url, modelName string, offset, batch int) tea.Cmd

	// regenBatchFn is the injectable batch function for REGENERATE EMBEDDINGS
	// when using the new chained engine. In production it is nil and
	// makeDefaultRegenBatchFn() is used.
	regenBatchFn func(url, modelName string, offset, batch int) tea.Cmd

	// UI components.
	status string
	err    error
}

// newModel returns a zero-value Model ready to use without a real store
// (for unit tests). The store field is nil; production code uses New().
func newModel() Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 128

	ci := textinput.New()
	ci.CharLimit = 256

	return Model{
		searchInput:     ti,
		configInput:     ci,
		configOllamaURL: "http://localhost:11434",
		configModel:     "nomic-embed-text",
		view:            viewProjects,
		version:         "dev",
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

// detailMetaLineCount returns the number of content-area lines consumed by the
// metadata block in viewDetail for the currently selected observation. This must
// match the lines written by viewDetail exactly so that the viewport height is
// computed correctly.
//
// Fixed lines: title(1) + type/badge(1) + project/scope(1) + created(1) + updated(1) + rule(1) = 6.
// Optional lines: +1 for topic_key if set, +1 for sync_id if set.
func (m Model) detailMetaLineCount() int {
	const fixed = 6
	if m.selectedObs == nil {
		return fixed
	}
	extra := 0
	if m.selectedObs.TopicKey != nil && *m.selectedObs.TopicKey != "" {
		extra++
	}
	if m.selectedObs.SyncID != "" {
		extra++
	}
	return fixed + extra
}

// detailVPHeight returns the viewport height for the detail view: the number of
// content-area rows available after subtracting chrome (header+separator) and
// the metadata block.
func (m Model) detailVPHeight() int {
	meta := m.detailMetaLineCount()
	h := m.height - headerRows - statusRows - meta
	if h < 1 {
		h = 1
	}
	return h
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
		// Recompute viewport for detail view.
		// Viewport width is capped at effectiveWidth so content does not scatter
		// across an excessively wide screen on wide terminals.
		m.vp.Width = effectiveWidth(msg.Width)
		m.vp.Height = m.detailVPHeight()
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
		m.hybridResults = msg.hybrid
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

	case configSettingsLoadedMsg:
		m.configEmbeddingsEnabled = msg.embeddingsEnabled
		m.configOllamaURL = msg.ollamaURL
		m.configModel = msg.model
		return m, nil

	case configCoverageLoadedMsg:
		m.configCoverageHave = msg.have
		m.configCoverageTotal = msg.total
		return m, nil

	case configSaveSettingMsg:
		// Store the error (nil on success, non-nil on failure) so the config
		// view can render a danger message when the write failed.
		m.configSaveErr = msg.err
		return m, nil

	case configProbeResultMsg:
		// Ignore stale results that land after the user left the config view;
		// re-entry resets the test state and a late message must not repopulate it.
		if m.view == viewConfig {
			m.configTesting = false
			m.configTestOK = msg.ok
			m.configTestResult = msg.info
		}
		return m, nil

	case configRegenResultMsg:
		// Ignore stale results that arrive after the user left the config view.
		// This path is kept for the legacy regenFn (single-shot stub in tests).
		if m.view == viewConfig {
			m.configRegenerating = false
			m.configRegenOK = msg.ok
			m.configRegenResult = msg.info
		}
		return m, nil

	case embedJobBatchMsg:
		// Process a batch result from the chained embed engine. The job keeps
		// running (issuing next batch cmd) until finished or aborted.
		//
		// View-guard: the job KEEPS PROCESSING even if the user navigates away
		// from the config view (batch cmds continue flowing). The progress fields
		// are updated regardless of view so state is consistent when the user
		// returns. Only the visual progress bar is suppressed in other views.
		if msg.aborted || msg.finished {
			m.jobRunning = false
			m.jobDone = msg.done
			m.jobTotal = msg.total
			// Sync legacy fields for REGENERATE so existing tests still pass.
			if m.jobKind == jobKindRegen {
				m.configRegenerating = false
			}
			// Build result string.
			modelName := m.configModel
			if msg.aborted {
				errStr := ""
				if msg.lastErr != nil {
					errStr = msg.lastErr.Error()
				}
				m.jobResult = fmt.Sprintf("ABORTED — no progress — %s", errStr)
				m.jobResultOK = false
			} else {
				// finished == true.
				if msg.lastErr != nil {
					m.jobResult = fmt.Sprintf("PARTIAL — %d/%d — %s", msg.done, msg.total, msg.lastErr.Error())
					m.jobResultOK = false
				} else if msg.done == msg.total && msg.total > 0 {
					label := "EMBEDDINGS UP TO DATE"
					if m.jobKind == jobKindRegen {
						label = "EMBEDDINGS REGENERATED"
					}
					m.jobResult = fmt.Sprintf("%s — %d/%d — model %s", label, msg.done, msg.total, modelName)
					m.jobResultOK = true
				} else if msg.total == 0 {
					m.jobResult = fmt.Sprintf("ALL EMBEDDED — coverage %d/%d", msg.done, msg.total)
					m.jobResultOK = true
				} else {
					m.jobResult = fmt.Sprintf("PARTIAL — %d/%d — some failures", msg.done, msg.total)
					m.jobResultOK = false
				}
			}
			// Mirror into legacy configRegenResult for REGENERATE jobs.
			if m.jobKind == jobKindRegen {
				m.configRegenResult = m.jobResult
				m.configRegenOK = m.jobResultOK
			}
			return m, nil
		}
		// Partial batch: update progress and issue the next batch cmd.
		m.jobDone = msg.done
		m.jobTotal = msg.total
		if m.jobKind == jobKindEmbed {
			batchFn := m.embedBatchFn
			if batchFn == nil {
				batchFn = m.makeDefaultEmbedBatchFn()
			}
			return m, batchFn(m.configOllamaURL, m.configModel, 0, embedBatchSize)
		}
		// jobKindRegen.
		batchFn := m.regenBatchFn
		if batchFn == nil {
			batchFn = m.makeDefaultRegenBatchFn()
		}
		return m, batchFn(m.configOllamaURL, m.configModel, 0, embedBatchSize)

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
		case msg.Type == tea.KeyEsc:
			m.searching = false
			m.searchQuery = ""
			m.fuzzyResults = false
			m.hybridResults = false
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
		case msg.Type == tea.KeyEsc:
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
			m.hybridResults = false
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
		case msg.Type == tea.KeyEsc:
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
	case viewConfig:
		return m.handleKeyConfig(msg)
	}
	return m, nil
}

func (m Model) handleKeyProjects(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && (msg.Runes[0] == 'q') || msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.projectCursor > 0 {
			m.projectCursor--
			m.projOffset = clampWindow(m.projectCursor, m.projOffset, m.listVisibleHeight(false, m.showLogo()), len(m.projects))
		}
	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			m.projOffset = clampWindow(m.projectCursor, m.projOffset, m.listVisibleHeight(false, m.showLogo()), len(m.projects))
		}
	case msg.Type == tea.KeyEnter:
		if len(m.projects) == 0 {
			return m, nil
		}
		m.selectedProject = m.projects[m.projectCursor].Project
		m.obsCursor = 0
		m.obsOffset = 0
		m.fuzzyResults = false
		m.hybridResults = false
		m.view = viewObservations
		return m, m.fetchObservations(m.selectedProject)
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == '/':
		// Open global search (cross-project).
		m.globalSearching = true
		m.searchInput.Reset()
		m.searchInput.Placeholder = "Search all projects…"
		m.searchInput.Focus()
		return m, textinput.Blink
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'c':
		// Open config view.
		m.view = viewConfig
		m.configCursor = 0
		m.configEditing = false
		m.configTesting = false
		m.configTestResult = ""
		return m, m.fetchConfigSettings()
	}
	return m, nil
}

func (m Model) handleKeyObservations(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.view = viewProjects
		m.searching = false
		m.searchQuery = ""
		m.fuzzyResults = false
		m.hybridResults = false
	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.obsCursor > 0 {
			m.obsCursor--
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(true, false), len(m.observations))
		}
	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.obsCursor < len(m.observations)-1 {
			m.obsCursor++
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(true, false), len(m.observations))
		}
	case msg.Type == tea.KeyEnter:
		if len(m.observations) == 0 {
			return m, nil
		}
		obs := m.observations[m.obsCursor]
		m.selectedObs = &obs
		// Refresh viewport dimensions for the newly selected observation.
		// Width is capped at effectiveWidth so content aligns with the centred block.
		m.vp.Width = effectiveWidth(m.width)
		m.vp.Height = m.detailVPHeight()
		m.vp.SetContent(renderObservationDetail(obs))
		m.vp.GotoTop()
		m.view = viewDetail
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == '/':
		m.searching = true
		m.searchInput.Reset()
		m.searchInput.Placeholder = "Search memories…"
		m.searchInput.Focus()
		return m, textinput.Blink
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'd':
		if len(m.observations) > 0 {
			m.confirmDelete = true
		}
	}
	return m, nil
}

func (m Model) handleKeyGlobalSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.view = viewProjects
		m.globalQuery = ""
		m.fuzzyResults = false
		m.hybridResults = false
		m.observations = nil
		m.obsCursor = 0
		m.obsOffset = 0
	case msg.Type == tea.KeyUp || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'k'):
		if m.obsCursor > 0 {
			m.obsCursor--
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(false, false), len(m.observations))
		}
	case msg.Type == tea.KeyDown || (msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'j'):
		if m.obsCursor < len(m.observations)-1 {
			m.obsCursor++
			m.obsOffset = clampWindow(m.obsCursor, m.obsOffset, m.listVisibleHeight(false, false), len(m.observations))
		}
	case msg.Type == tea.KeyEnter:
		if len(m.observations) == 0 {
			return m, nil
		}
		obs := m.observations[m.obsCursor]
		m.selectedObs = &obs
		m.selectedProject = obs.Project
		// Refresh viewport dimensions for the newly selected observation.
		m.vp.Width = effectiveWidth(m.width)
		m.vp.Height = m.detailVPHeight()
		m.vp.SetContent(renderObservationDetail(obs))
		m.vp.GotoTop()
		m.view = viewDetail
	}
	return m, nil
}

func (m Model) handleKeyDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.view = viewObservations
		m.selectedObs = nil
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] == 'd':
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
	case viewConfig:
		return m.viewConfigPage()
	}
	return ""
}

// renderHeader renders the retro-branded top bar.
// Left side: "ION//MEM vX.Y.Z" (accent bold).
// Right side: uppercase breadcrumb with rightPad columns of right margin.
// Width-aware when m.width > 0.
func (m Model) renderHeader(breadcrumb string) string {
	brand := headerStyle.Render("ION//MEM") + " " + dimStyle.Render(m.version)
	right := dimStyle.Render(strings.ToUpper(breadcrumb))

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

// renderSeparator renders a full-width double-rule separator in muted style.
// Uses ═ (double horizontal box-drawing) as the retro CRT divider.
func (m Model) renderSeparator() string {
	w := m.width
	if w < 1 {
		w = 40
	}
	return mutedStyle.Render(strings.Repeat("═", w))
}

// renderLogoHero renders the ION MEM ASCII logo with a vertical gradient and
// a retro-styled dim tagline. Returns a string with exactly logoHeight lines
// (each terminated by "\n"). Callers must subtract logoHeight from the content budget.
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

	// Tagline line (1 line) — retro style with ── decorators, centred within the content block.
	tagText := "── PERSISTENT MEMORY FOR AI CODING AGENTS ──"
	tagline := dimStyle.Render(tagText)
	tagRaw := lipgloss.Width(tagline)
	tagPad := (w - tagRaw) / 2
	if tagPad < 0 {
		tagPad = 0
	}
	sb.WriteString(strings.Repeat(" ", offset+tagPad) + tagline + "\n")

	// Blank spacer between tagline and the first list row.
	sb.WriteString("\n")

	return sb.String()
}

// renderSearchBar renders the retro double-border search bar for the observations view.
// It returns a string with exactly searchBarRows lines (each terminated by "\n").
//
// Design: double-border box (╔ ═ ╗ / ║ / ╚ ═ ╝) with the title "SEARCH" embedded
// in the top border. When focused, the border is accent-colored.
//
// cOffset is the centering left-indent (0 on narrow terminals); cWidth is the
// effective column budget (≤ contentMaxWidth).
//
// Bug fix: cOffset must be applied ONCE — to the rendered box — not inside
// the box width calculation. Previously the style's Width() accidentally
// double-counted the offset by using cWidth which already excluded the margin.
func (m Model) renderSearchBar(cOffset, cWidth int) string {
	if cWidth < 20 {
		cWidth = 20
	}
	// Inner width: effective content width minus double-border (2 cols) minus padding (1 each side = 2).
	// DoubleBorder adds 1 char each side for the border itself.
	// Padding(0, 1) adds 1 space each side inside the border.
	// Total overhead: 2 (border) + 2 (padding) = 4 cols.
	innerW := cWidth - 4
	if innerW < 5 {
		innerW = 5
	}

	var inner string
	if m.searching {
		// Live input — show "/" prompt + textinput.
		prompt := headerStyle.Render("/") + " " + m.searchInput.View()
		inner = prompt
	} else if m.searchQuery != "" {
		// Show submitted query in dim style.
		inner = dimStyle.Render("/") + " " + dimStyle.Render(m.searchQuery)
	} else {
		// Idle placeholder.
		inner = dimStyle.Render("/ Search memories…")
	}

	style := searchBarIdleStyle
	if m.searching {
		style = searchBarActiveStyle
	}

	// Render the box at exactly innerW wide. The border is added by lipgloss
	// around the inner area — so total box width = innerW + 4 (border + padding).
	// Apply cOffset once by prepending spaces to the rendered box string.
	rendered := style.Width(innerW).Render(inner)

	// The rendered box may be multiple lines (top border, content, bottom border).
	// Prepend cOffset spaces to each line.
	if cOffset > 0 {
		indent := strings.Repeat(" ", cOffset)
		lines := strings.Split(rendered, "\n")
		for i, l := range lines {
			lines[i] = indent + l
		}
		rendered = strings.Join(lines, "\n")
	}

	return rendered + "\n"
}

// renderFooter renders the BBS key-legend footer in dim style.
// The hint text is context-sensitive based on the current view.
func (m Model) renderFooter() string {
	var hint string
	switch m.view {
	case viewDetail:
		hint = bbsFooterDetail
	case viewObservations, viewGlobalSearch:
		hint = bbsFooterDelete
	case viewConfig:
		hint = bbsFooterConfig
	default:
		hint = bbsFooterBase
	}
	if m.searching || m.globalSearching {
		hint = bbsFooterSearch
	}
	return dimStyle.Render(hint)
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
		content.WriteString(m.renderSearchBar(cOffset, cWidth))
	}

	rowIndent := strings.Repeat(" ", cOffset+leftPad) // indent for ordinary content rows

	if len(m.projects) == 0 {
		emptyText := "░░ NO PROJECTS YET ░░"
		emptyStyled := dimStyle.Render(emptyText)
		if m.width > 0 {
			emptyStyled = lipgloss.NewStyle().Width(cWidth).Align(lipgloss.Center).Foreground(defaultTheme.dim).Render(emptyText)
		}
		content.WriteString(strings.Repeat(" ", cOffset) + emptyStyled + "\n")
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
				if i == m.projectCursor {
					// Selected row: full inverse-video using selectedRowStyle.
					// We render the entire row content with the selection style,
					// padded to fill cWidth so the background covers the full row.
					nameFmt := fmt.Sprintf("%-*s", nameWidth, name)
					leftRaw := leftPad + nameWidth + 2 + len(counts) + 2
					gap := cWidth - leftRaw - activityWidth - rightPad
					if gap < 1 {
						gap = 1
					}
					actStr := fmt.Sprintf("%-*s", activityWidth, activityStr)
					rowContent := strings.Repeat(" ", leftPad) + nameFmt + "  " + counts + strings.Repeat(" ", gap) + actStr
					row = strings.Repeat(" ", cOffset) + selectedRowStyle.Render(rowContent)
				} else {
					left := strings.Repeat(" ", cOffset+leftPad)
					nameFmt := fmt.Sprintf("%-*s", nameWidth, name)
					countsFmt := counts
					leftRaw := leftPad + nameWidth + 2 + len(counts) + 2
					gap := cWidth - leftRaw - activityWidth - rightPad
					if gap < 1 {
						gap = 1
					}
					actStr := dimStyle.Render(fmt.Sprintf("%-*s", activityWidth, activityStr))
					row = left + nameFmt + "  " + countsFmt + strings.Repeat(" ", gap) + actStr
				}
			} else {
				if i == m.projectCursor {
					rowContent := strings.Repeat(" ", leftPad) + fmt.Sprintf("%-*s", nameWidth, name) + "  " + counts + "  " + activityStr
					row = selectedRowStyle.Render(rowContent)
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
	statusLeft := fmt.Sprintf("ION-MEMORY // %d PROJECT(S)", len(m.projects))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "ERROR: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft)

	footerLine := strings.Repeat(" ", cOffset+leftPad) + m.renderFooter()

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
	breadcrumb := "Projects // " + m.selectedProject
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// Wide-terminal layout helpers: content is capped at contentMaxWidth and
	// centred horizontally when the terminal is wider.
	cOffset := contentOffset(m.width) // extra left indent for centering
	cWidth := effectiveWidth(m.width) // column budget for content rows
	if cWidth < 40 {
		cWidth = 40
	}
	rowIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Persistent search bar — always visible between header and list.
	content.WriteString(m.renderSearchBar(cOffset, cWidth))

	if len(m.observations) == 0 {
		var emptyMsg string
		if m.searchQuery != "" {
			emptyMsg = fmt.Sprintf("░░ NO RESULTS FOR %q ░░", strings.ToUpper(m.searchQuery))
		} else {
			emptyMsg = "░░ NO MEMORIES YET ░░"
		}
		content.WriteString(rowIndent + dimStyle.Render(emptyMsg) + "\n")
	} else {
		visible := m.listVisibleHeight(true, false)
		offset := m.obsOffset
		total := len(m.observations)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(rowIndent + mutedStyle.Render("↑ more") + "\n")
		}

		// Badge: badgeVisibleWidth chars of rendered content.
		// The lipgloss badge adds foreground escape codes only, so we use
		// badgeVisibleWidth for layout math (visual columns consumed by badge).
		badgeRenderedWidth := badgeVisibleWidth
		ageWidth := 10
		titleWidth := 50
		if cWidth > 40 {
			// leftPad + badgeRenderedWidth + 1 (space) + titleWidth + gap + ageWidth + rightPad = cWidth
			titleWidth = cWidth - leftPad - badgeRenderedWidth - 1 - ageWidth - rightPad
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

			// Right-align age: pad so age ends at cWidth-rightPad (within the centred block).
			var row string
			if m.width > 0 {
				titleFmt := fmt.Sprintf("%-*s", titleWidth, title)
				// Compute gap between title and age to right-align age within effective width.
				// raw chars used so far: leftPad + badgeRenderedWidth + 1 (space) + titleWidth
				usedLeft := leftPad + badgeRenderedWidth + 1 + titleWidth
				gap := cWidth - usedLeft - len(ageStr) - rightPad
				if gap < 1 {
					gap = 1
				}
				ageRendered := dimStyle.Render(ageStr)
				if i == m.obsCursor {
					// Selected row: full inverse-video, no ▌ glyph.
					rowContent := strings.Repeat(" ", leftPad) + stripAnsiCodes(badge) + " " + titleFmt + strings.Repeat(" ", gap) + ageStr
					row = strings.Repeat(" ", cOffset) + selectedRowStyle.Render(rowContent)
				} else {
					row = rowIndent + badge + " " + titleFmt + strings.Repeat(" ", gap) + ageRendered
				}
			} else {
				age := dimStyle.Render(ageStr)
				if i == m.obsCursor {
					rowContent := strings.Repeat(" ", leftPad) + stripAnsiCodes(badge) + " " + fmt.Sprintf("%-*s", titleWidth, title) + " " + ageStr
					row = selectedRowStyle.Render(rowContent)
				} else {
					row = strings.Repeat(" ", leftPad) + badge + " " + fmt.Sprintf("%-*s", titleWidth, title) + " " + age
				}
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(rowIndent + mutedStyle.Render("↓ more") + "\n")
		}
	}

	if m.confirmDelete {
		content.WriteString("\n" + rowIndent + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.obsCursor, len(m.observations))
	statusLeft := strings.ToUpper(m.selectedProject) + " // " + fmt.Sprintf("%d OBSERVATION(S)", len(m.observations))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "ERROR: " + m.err.Error()
	}

	// Build status line: left context + optional fuzzy/hybrid chip right-aligned.
	var statusLine string
	if m.searchQuery != "" && (m.fuzzyResults || m.hybridResults) {
		var chip string
		if m.hybridResults {
			chip = hybridChipStyle.Render("~HYBRID")
		} else {
			chip = fuzzyChipStyle.Render("~FUZZY")
		}
		leftPart := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft+fmt.Sprintf("  %q", m.searchQuery))
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
		statusLine = strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft+fmt.Sprintf("  %q", m.searchQuery))
	} else {
		statusLine = strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft)
	}

	footerLine := strings.Repeat(" ", cOffset+leftPad) + m.renderFooter()

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
	breadcrumb := fmt.Sprintf(`Projects // Search "%s"`, query)
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// Wide-terminal layout helpers.
	cOffset := contentOffset(m.width)
	cWidth := effectiveWidth(m.width)
	if cWidth < 40 {
		cWidth = 40
	}
	rowIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	if len(m.observations) == 0 {
		var emptyMsg string
		if query != "" {
			emptyMsg = fmt.Sprintf("░░ NO RESULTS FOR %q ░░", strings.ToUpper(query))
		} else {
			emptyMsg = "░░ NO RESULTS ░░"
		}
		content.WriteString(rowIndent + dimStyle.Render(emptyMsg) + "\n")
	} else {
		visible := m.listVisibleHeight(false, false)
		offset := m.obsOffset
		total := len(m.observations)

		showUp, showDown := overflowMarkers(offset, visible, total)
		if showUp {
			content.WriteString(rowIndent + mutedStyle.Render("↑ more") + "\n")
		}

		// Column layout: badge | title | project (dim) | age
		badgeW := badgeVisibleWidth
		projColW := 14
		ageWidth := 10
		titleWidth := 30
		if cWidth > 60 {
			// leftPad + badgeW + 1 + titleWidth + 2 + projColW + gap + ageWidth + rightPad = cWidth
			titleWidth = cWidth - leftPad - badgeW - 1 - projColW - 2 - ageWidth - rightPad
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
			gap := cWidth - usedLeft - len(ageStr) - rightPad
			if gap < 1 {
				gap = 1
			}
			ageRendered := dimStyle.Render(ageStr)
			var row string
			if i == m.obsCursor {
				// Selected row: full inverse-video, no ▌ glyph.
				rowContent := strings.Repeat(" ", leftPad) + stripAnsiCodes(badge) + " " + titleFmt + "  " + truncStr(obs.Project, projColW) + strings.Repeat(" ", gap) + ageStr
				row = strings.Repeat(" ", cOffset) + selectedRowStyle.Render(rowContent)
			} else {
				row = rowIndent + badge + " " + titleFmt + "  " + proj + strings.Repeat(" ", gap) + ageRendered
			}
			content.WriteString(row + "\n")
		}

		if showDown {
			content.WriteString(rowIndent + mutedStyle.Render("↓ more") + "\n")
		}
	}

	// ── status and footer ────────────────────────────────────────────────────
	pos := positionIndicator(m.obsCursor, len(m.observations))
	statusLeft := fmt.Sprintf("ALL PROJECTS // %d RESULT(S)", len(m.observations))
	if pos != "" {
		statusLeft += "  " + pos
	}
	if m.err != nil {
		statusLeft = "ERROR: " + m.err.Error()
	}

	var statusLine string
	if m.hybridResults || m.fuzzyResults {
		var chip string
		if m.hybridResults {
			chip = hybridChipStyle.Render("~HYBRID")
		} else {
			chip = fuzzyChipStyle.Render("~FUZZY")
		}
		leftPart := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft)
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
		statusLine = strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusLeft)
	}

	footerLine := strings.Repeat(" ", cOffset+leftPad) + m.renderFooter()

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
	breadcrumb := "Projects // " + m.selectedProject + " // Detail"
	header := m.renderHeader(breadcrumb)
	separator := m.renderSeparator()

	// Wide-terminal layout helpers.
	cOffset := contentOffset(m.width)
	cWidth := effectiveWidth(m.width)
	if cWidth < 40 {
		cWidth = 40
	}
	metaIndent := strings.Repeat(" ", cOffset+leftPad)

	// ── content rows ────────────────────────────────────────────────────────
	var content strings.Builder

	// Metadata block — uppercase label grid with dim labels.
	// Count lines precisely; viewport height is set in detailVPHeight().
	labelStyle := lipgloss.NewStyle().Foreground(defaultTheme.dim).Width(10)

	// Title row (full accent bold).
	content.WriteString(metaIndent + selectedRowStyle.Render(obs.Title) + "\n")

	// Type + badge inline.
	content.WriteString(metaIndent + labelStyle.Render("TYPE") + renderBadge(obs.Type) + "\n")

	// project / scope on same line.
	content.WriteString(metaIndent + labelStyle.Render("PROJECT") + dimStyle.Render(obs.Project) + "   " + labelStyle.Render("SCOPE") + dimStyle.Render(obs.Scope) + "\n")

	if obs.TopicKey != nil && *obs.TopicKey != "" {
		content.WriteString(metaIndent + labelStyle.Render("TOPIC") + dimStyle.Render(*obs.TopicKey) + "\n")
	}
	if obs.SyncID != "" {
		content.WriteString(metaIndent + labelStyle.Render("SYNC_ID") + mutedStyle.Render(obs.SyncID) + "\n")
	}
	content.WriteString(metaIndent + labelStyle.Render("CREATED") + dimStyle.Render(obs.CreatedAt) + "\n")
	content.WriteString(metaIndent + labelStyle.Render("UPDATED") + dimStyle.Render(obs.UpdatedAt) + "\n")

	// Double-rule separator between meta and body (═), capped at effective content width.
	ruleWidth := cWidth
	if ruleWidth < 1 {
		ruleWidth = 40
	}
	content.WriteString(strings.Repeat(" ", cOffset) + mutedStyle.Render(strings.Repeat("═", ruleWidth)) + "\n")

	// Viewport body: the viewport renders content at vp.Width. On wide terminals
	// we prepend cOffset spaces to each body line so the body aligns with the
	// meta block (which uses cOffset+leftPad for labels).
	vpContent := m.vp.View()
	if cOffset > 0 && vpContent != "" {
		indent := strings.Repeat(" ", cOffset)
		bodyLines := strings.Split(vpContent, "\n")
		for i, l := range bodyLines {
			if l != "" {
				bodyLines[i] = indent + l
			}
		}
		vpContent = strings.Join(bodyLines, "\n")
	}
	content.WriteString(vpContent)

	if m.confirmDelete {
		content.WriteString("\n" + metaIndent + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	// ── status and footer ────────────────────────────────────────────────────
	statusText := fmt.Sprintf("OBSERVATION #%d", obs.ID)
	if m.vp.TotalLineCount() > 0 {
		statusText += fmt.Sprintf("  %.0f%%", m.vp.ScrollPercent()*100)
	} else {
		statusText += "  SCROLL ↑↓"
	}
	if m.err != nil {
		statusText = "ERROR: " + m.err.Error()
	}
	statusLine := strings.Repeat(" ", cOffset+leftPad) + statusBarStyle.Render(statusText)
	footerLine := strings.Repeat(" ", cOffset+leftPad) + m.renderFooter()

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
		ctx := context.Background()
		searcher := hybrid.NewSearcherFromSettings(ctx, st)
		results, meta, err := searcher.Search(ctx, store.SearchParams{
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
		return searchResultMsg{results: obs, fuzzy: meta.Fuzzy, hybrid: meta.Hybrid}
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
		ctx := context.Background()
		searcher := hybrid.NewSearcherFromSettings(ctx, st)
		results, meta, err := searcher.Search(ctx, store.SearchParams{
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
		return searchResultMsg{results: obs, fuzzy: meta.Fuzzy, hybrid: meta.Hybrid}
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

// stripAnsiCodes removes ANSI escape sequences from s, returning the plain text.
// This is a package-level helper used by both render functions and tests.
func stripAnsiCodes(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip 'm'
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

func renderObservationDetail(obs store.Observation) string {
	var sb strings.Builder
	sb.WriteString(obs.Content)
	return sb.String()
}
