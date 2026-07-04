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

// ─── styles ───────────────────────────────────────────────────────────────────

var (
	accent    = lipgloss.Color("#7D56F4")
	dimColor  = lipgloss.Color("#626262")
	alertColor = lipgloss.Color("#FF5F87")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	badgeStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(dimColor)
	fuzzyStyle    = lipgloss.NewStyle().Foreground(alertColor).Italic(true)
	confirmStyle  = lipgloss.NewStyle().Foreground(alertColor).Bold(true)
)

// ─── model ────────────────────────────────────────────────────────────────────

// Model is the root Bubble Tea model for the ion-mem TUI dashboard.
type Model struct {
	store *store.Store
	width int
	height int

	view viewState

	// Projects view.
	projects      []store.ProjectSummary
	projectCursor int

	// Observations view.
	selectedProject string
	observations    []store.Observation
	obsCursor       int

	// Search.
	searching   bool
	searchQuery string
	fuzzyResults bool
	searchInput textinput.Model

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
	}
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
		m.vp.Width = msg.Width
		m.vp.Height = msg.Height - 4
		return m, nil

	case projectsLoadedMsg:
		m.projects = msg.summaries
		if m.projectCursor >= len(m.projects) {
			m.projectCursor = 0
		}
		return m, nil

	case observationsLoadedMsg:
		m.observations = msg.observations
		if msg.project != "" {
			m.selectedProject = msg.project
		}
		if m.obsCursor >= len(m.observations) {
			m.obsCursor = 0
		}
		m.confirmDelete = false
		return m, nil

	case searchResultMsg:
		m.observations = msg.results
		m.fuzzyResults = msg.fuzzy
		m.searching = false
		m.obsCursor = 0
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

	// Delegate to sub-components when searching.
	if m.searching {
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
	// When in search input mode, handle Enter/Esc specially.
	if m.searching {
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
		}
	case key.Matches(msg, keys.Down):
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
		}
	case key.Matches(msg, keys.Enter):
		if len(m.projects) == 0 {
			return m, nil
		}
		m.selectedProject = m.projects[m.projectCursor].Project
		m.obsCursor = 0
		m.fuzzyResults = false
		m.view = viewObservations
		return m, m.fetchObservations(m.selectedProject)
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
		}
	case key.Matches(msg, keys.Down):
		if m.obsCursor < len(m.observations)-1 {
			m.obsCursor++
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
		m.searchInput.Focus()
		return m, textinput.Blink
	case key.Matches(msg, keys.Delete):
		if len(m.observations) > 0 {
			m.confirmDelete = true
		}
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
	}
	return ""
}

func (m Model) viewProjects() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("ion-mem — Projects") + "\n\n")

	if len(m.projects) == 0 {
		sb.WriteString(dimStyle.Render("No projects found. Save observations from an agent first.") + "\n")
	}

	for i, p := range m.projects {
		cursor := "  "
		line := fmt.Sprintf("%-30s  %3d obs  %2d sessions  %s",
			p.Project,
			p.ObservationCount,
			p.SessionCount,
			humanizeTime(p.LastActivity),
		)
		if i == m.projectCursor {
			cursor = "> "
			sb.WriteString(selectedStyle.Render(cursor+line) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
		_ = cursor
	}

	sb.WriteString("\n")
	sb.WriteString(statusBarStyle.Render(m.statusLine()) + "\n")
	sb.WriteString(dimStyle.Render(m.help.ShortHelpView(keys.ShortHelp())) + "\n")
	return sb.String()
}

func (m Model) viewObservations() string {
	var sb strings.Builder

	header := fmt.Sprintf("Project: %s", m.selectedProject)
	if m.searchQuery != "" {
		header += fmt.Sprintf("  [search: %q", m.searchQuery)
		if m.fuzzyResults {
			header += "  " + fuzzyStyle.Render("~fuzzy")
		}
		header += "]"
	}
	sb.WriteString(titleStyle.Render(header) + "\n\n")

	if m.searching {
		sb.WriteString("Search: " + m.searchInput.View() + "\n\n")
	}

	if len(m.observations) == 0 {
		sb.WriteString(dimStyle.Render("No observations.") + "\n")
	}

	for i, obs := range m.observations {
		badge := badgeStyle.Render("[" + truncStr(obs.Type, 10) + "]")
		age := dimStyle.Render(humanizeTime(parseCreatedAt(obs.CreatedAt)))
		title := truncStr(obs.Title, 50)
		line := fmt.Sprintf("%s %-50s %s", badge, title, age)

		if i == m.obsCursor {
			sb.WriteString(selectedStyle.Render("> "+line) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}

	if m.confirmDelete {
		sb.WriteString("\n" + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(statusBarStyle.Render(m.statusLine()) + "\n")
	sb.WriteString(dimStyle.Render(m.help.ShortHelpView(keys.ShortHelp())) + "\n")
	return sb.String()
}

func (m Model) viewDetail() string {
	if m.selectedObs == nil {
		return ""
	}
	obs := m.selectedObs
	var sb strings.Builder

	sb.WriteString(titleStyle.Render(obs.Title) + "\n")
	sb.WriteString(dimStyle.Render(fmt.Sprintf("type: %s  project: %s  scope: %s", obs.Type, obs.Project, obs.Scope)) + "\n")
	if obs.TopicKey != nil && *obs.TopicKey != "" {
		sb.WriteString(dimStyle.Render("topic_key: "+*obs.TopicKey) + "\n")
	}
	if obs.SyncID != "" {
		sb.WriteString(dimStyle.Render("sync_id: "+obs.SyncID) + "\n")
	}
	sb.WriteString(dimStyle.Render(fmt.Sprintf("created: %s  updated: %s", obs.CreatedAt, obs.UpdatedAt)) + "\n")
	sb.WriteString("\n")
	sb.WriteString(m.vp.View())

	if m.confirmDelete {
		sb.WriteString("\n" + confirmStyle.Render("Delete this observation? y/n") + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(statusBarStyle.Render(m.statusLine()) + "\n")
	sb.WriteString(dimStyle.Render(m.help.ShortHelpView(keys.ShortHelp())) + "\n")
	return sb.String()
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

func (m Model) statusLine() string {
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	switch m.view {
	case viewProjects:
		return fmt.Sprintf("%d project(s)", len(m.projects))
	case viewObservations:
		s := fmt.Sprintf("%s — %d observation(s)", m.selectedProject, len(m.observations))
		if m.fuzzyResults {
			s += "  " + fuzzyStyle.Render("~fuzzy")
		}
		return s
	case viewDetail:
		if m.selectedObs != nil {
			return fmt.Sprintf("observation #%d — scroll ↑↓", m.selectedObs.ID)
		}
	}
	return ""
}

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
