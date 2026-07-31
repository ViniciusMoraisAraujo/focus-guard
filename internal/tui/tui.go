package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"focusguard/internal/ipc"
)

type viewState int

const (
	viewList viewState = iota
	viewForm
)

type model struct {
	client *ipc.Client
	state  viewState

	table  table.Model
	blocks []BlockInfo

	domainInput   textinput.Model
	durationInput textinput.Model
	formFocus     int

	statusMessage string
	err           error
	loading       bool

	expectedDoH     bool
	dohActive       bool
	firewallRules   int
	protectionError string

	width  int
	height int
}

type BlockInfo struct {
	Domain    string
	StartedAt time.Time
	ExpiresAt time.Time
	Remaining time.Duration
	IsActive  bool
}

type statusFetchedMsg struct {
	blocks          []BlockInfo
	expectedDoH     bool
	dohActive       bool
	firewallRules   int
	protectionError string
}

type fetchErrMsg struct {
	err error
}

type blockAppliedMsg struct {
	message string
}

type blockErrMsg struct {
	err error
}

var (
	primary   = lipgloss.AdaptiveColor{Light: "#0057B7", Dark: "#6EB5FF"}
	success   = lipgloss.AdaptiveColor{Light: "#008000", Dark: "#4ADE80"}
	warning   = lipgloss.AdaptiveColor{Light: "#B8860B", Dark: "#FBBF24"}
	errColor  = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#F87171"}
	muted     = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}
	highlight = lipgloss.AdaptiveColor{Light: "#0033AA", Dark: "#93C5FD"}

	appStyle = lipgloss.NewStyle().
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primary).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(muted).
			MarginBottom(1)

	footerStyle = lipgloss.NewStyle().
			MarginTop(1).
			Foreground(muted)

	keyStyle = lipgloss.NewStyle().
			Foreground(highlight).
			Bold(true)

	keyBindStyle = lipgloss.NewStyle().
			Foreground(muted)

	emptyStyle = lipgloss.NewStyle().
			Foreground(muted).
			Italic(true).
			MarginTop(1).
			MarginBottom(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(errColor).
			Bold(true).
			MarginTop(1)

	successStyle = lipgloss.NewStyle().
			Foreground(success).
			Bold(true).
			MarginTop(1)

	loadingStyle = lipgloss.NewStyle().
			Foreground(warning).
			Italic(true).
			MarginTop(1)

	formBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primary).
			Padding(1, 2).
			MarginTop(1).
			Width(55)

	labelStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true).
			MarginRight(1)

	tableBaseStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(muted).
			MarginTop(1).
			MarginBottom(1)
)

func New(client *ipc.Client) *model {
	cols := []table.Column{
		{Title: "Dom\u00ednio", Width: 30},
		{Title: "In\u00edcio", Width: 12},
		{Title: "Expira", Width: 12},
		{Title: "Restante", Width: 10},
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(muted).
		BorderBottom(true).
		Bold(true).
		Foreground(highlight).
		Padding(0, 1)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#E0E0E0")).
		Background(lipgloss.Color("#2D5F9A")).
		Bold(false)
	s.Cell = s.Cell.
		Foreground(lipgloss.AdaptiveColor{Light: "#222222", Dark: "#E0E0E0"}).
		Padding(0, 1)

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(8),
		table.WithStyles(s),
	)

	di := textinput.New()
	di.Placeholder = "ex: twitter.com"
	di.CharLimit = 80
	di.Width = 35

	du := textinput.New()
	du.Placeholder = "ex: 4h, 30m, 1h30m"
	du.CharLimit = 15
	du.Width = 15

	return &model{
		client:        client,
		state:         viewList,
		loading:       true,
		table:         t,
		domainInput:   di,
		durationInput: du,
	}
}

func (m *model) Init() tea.Cmd {
	return m.fetchStatusCmd()
}

func (m *model) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		req := ipc.Request{Action: "status"}
		resp, err := m.client.Send(req)
		if err != nil {
			return fetchErrMsg{err: err}
		}
		if !resp.Success {
			return fetchErrMsg{err: errors.New(resp.Message)}
		}

		blocks := make([]BlockInfo, 0, len(resp.Blocks))
		for _, b := range resp.Blocks {
			rem := time.Until(b.ExpiresAt).Round(time.Second)
			if rem < 0 {
				rem = 0
			}
			blocks = append(blocks, BlockInfo{
				Domain:    b.Domain,
				StartedAt: b.StartedAt,
				ExpiresAt: b.ExpiresAt,
				Remaining: rem,
				IsActive:  b.IsActive(),
			})
		}
		return statusFetchedMsg{
			blocks:          blocks,
			expectedDoH:     resp.ExpectedDoH,
			dohActive:       resp.DoHActive,
			firewallRules:   resp.FirewallRules,
			protectionError: resp.ProtectionError,
		}
	}
}

func (m *model) blockDomainCmd(domain, duration string) tea.Cmd {
	return func() tea.Msg {
		req := ipc.Request{
			Action:   "block",
			Domain:   domain,
			Duration: duration,
		}
		resp, err := m.client.Send(req)
		if err != nil {
			return blockErrMsg{err: err}
		}
		if !resp.Success {
			return blockErrMsg{err: errors.New(resp.Message)}
		}
		return blockAppliedMsg{message: resp.Message}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case statusFetchedMsg:
		m.loading = false
		m.blocks = msg.blocks
		m.err = nil
		m.expectedDoH = msg.expectedDoH
		m.dohActive = msg.dohActive
		m.firewallRules = msg.firewallRules
		m.protectionError = msg.protectionError
		m.updateTableRows()
		return m, nil

	case fetchErrMsg:
		m.loading = false
		m.err = msg.err
		m.statusMessage = ""
		m.expectedDoH = false
		m.dohActive = false
		m.firewallRules = 0
		m.protectionError = ""
		return m, nil

	case blockAppliedMsg:
		m.loading = false
		m.statusMessage = msg.message
		m.err = nil
		m.state = viewList
		m.domainInput.Reset()
		m.durationInput.Reset()
		m.formFocus = 0
		return m, m.fetchStatusCmd()

	case blockErrMsg:
		m.loading = false
		m.state = viewList
		m.domainInput.Reset()
		m.durationInput.Reset()
		m.formFocus = 0
		m.statusMessage = ""
		m.err = fmt.Errorf("não foi possível aplicar o bloqueio. Verifique se o daemon está em execução e tente novamente.")
		return m, m.fetchStatusCmd()
	}

	return m, nil
}

func (m *model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case viewList:
		return m.handleListKeys(msg)
	case viewForm:
		return m.handleFormKeys(msg)
	}
	return m, nil
}

func (m *model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "b":
		m.state = viewForm
		m.formFocus = 0
		m.domainInput.Focus()
		m.domainInput.SetValue("")
		m.durationInput.SetValue("")
		m.err = nil
		m.statusMessage = ""
		return m, nil

	case "r":
		if m.loading {
			return m, nil
		}
		m.loading = true
		m.err = nil
		return m, m.fetchStatusCmd()
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) handleFormKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.state = viewList
		m.err = nil
		m.statusMessage = ""
		return m, nil

	case "tab", "down":
		m.formFocus = (m.formFocus + 1) % 2
		m.updateFormFocus()
		return m, nil

	case "shift+tab", "up":
		m.formFocus = (m.formFocus - 1 + 2) % 2
		m.updateFormFocus()
		return m, nil

	case "enter":
		domain := strings.TrimSpace(m.domainInput.Value())
		duration := strings.TrimSpace(m.durationInput.Value())
		if domain == "" || duration == "" {
			m.err = fmt.Errorf("preencha o dom\u00ednio e a dura\u00e7\u00e3o")
			return m, nil
		}
		m.loading = true
		m.err = nil
		return m, m.blockDomainCmd(domain, duration)

	case "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	if m.formFocus == 0 {
		m.domainInput, cmd = m.domainInput.Update(msg)
	} else {
		m.durationInput, cmd = m.durationInput.Update(msg)
	}
	return m, cmd
}

func (m *model) updateFormFocus() {
	if m.formFocus == 0 {
		m.domainInput.Focus()
		m.durationInput.Blur()
	} else {
		m.domainInput.Blur()
		m.durationInput.Focus()
	}
}

func (m *model) updateTableRows() {
	rows := make([]table.Row, 0, len(m.blocks))
	for _, b := range m.blocks {
		if !b.IsActive {
			continue
		}
		rows = append(rows, table.Row{
			b.Domain,
			b.StartedAt.Local().Format("15:04 02/01"),
			b.ExpiresAt.Local().Format("15:04 02/01"),
			b.Remaining.String(),
		})
	}
	m.table.SetRows(rows)
}

func (m *model) View() string {
	switch m.state {
	case viewList:
		return m.listView()
	case viewForm:
		return m.formView()
	}
	return ""
}

func (m *model) listView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("\U0001f512 FocusGuard - Modo Interativo"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Gerencie seus bloqueios de forma f\u00e1cil"))
	b.WriteString("\n")

	if !m.loading {
		b.WriteString(m.protectionLine())
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("\u2716 %v", m.err)))
		b.WriteString("\n")
	}

	if m.statusMessage != "" {
		b.WriteString(successStyle.Render(fmt.Sprintf("\u2714 %s", m.statusMessage)))
		b.WriteString("\n")
	}

	if m.loading {
		b.WriteString(loadingStyle.Render("\u23f3 Carregando..."))
		b.WriteString("\n")
	}

	if len(m.blocks) == 0 && !m.loading && m.err == nil && m.statusMessage == "" {
		b.WriteString(emptyStyle.Render("Nenhum bloqueio ativo no momento."))
		b.WriteString("\n")
	} else if !m.loading {
		b.WriteString(tableBaseStyle.Render(m.table.View()))
	}

	b.WriteString("\n")

	b.WriteString(footerStyle.Render(fmt.Sprintf(
		"%s  %s  %s",
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[b]"),
			keyBindStyle.Render("Bloquear"),
		),
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[r]"),
			keyBindStyle.Render("Atualizar"),
		),
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[q]"),
			keyBindStyle.Render("Sair"),
		),
	)))

	return appStyle.Render(b.String())
}

func (m *model) protectionLine() string {
	if m.protectionError != "" {
		return "🛡 DoH/DoT: " + lipgloss.NewStyle().Foreground(warning).Bold(true).Render("erro ao consultar") +
			lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf(" · %s", m.protectionError))
	}

	state := "🛡 DoH/DoT: "

	if m.dohActive {
		state += lipgloss.NewStyle().Foreground(success).Bold(true).Render("ATIVA")
	} else {
		state += lipgloss.NewStyle().Foreground(muted).Render("inativa")
	}

	state += lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf(" · %d regra(s) de firewall", m.firewallRules))

	if m.expectedDoH && !m.dohActive {
		state += " " + lipgloss.NewStyle().Foreground(warning).Bold(true).Render("(esperada, não encontrada!)")
	} else if !m.expectedDoH && m.dohActive {
		state += " " + lipgloss.NewStyle().Foreground(muted).Render("(sem bloqueios ativos)")
	}

	return state
}

func (m *model) formView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("\U0001f512 FocusGuard - Novo Bloqueio"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Preencha os campos abaixo para bloquear um dom\u00ednio."))
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("\u2716 %v", m.err)))
		b.WriteString("\n")
	}

	formContent := strings.Builder{}

	domainLabel := labelStyle.Render("Dom\u00ednio:")
	domainField := m.domainInput.View()
	if m.formFocus == 0 {
		domainField = lipgloss.NewStyle().
			Foreground(highlight).
			Render(domainField)
	}
	formContent.WriteString(fmt.Sprintf("%s %s\n\n", domainLabel, domainField))

	durLabel := labelStyle.Render("Dura\u00e7\u00e3o:")
	durField := m.durationInput.View()
	if m.formFocus == 1 {
		durField = lipgloss.NewStyle().
			Foreground(highlight).
			Render(durField)
	}
	formContent.WriteString(fmt.Sprintf("%s %s\n", durLabel, durField))

	b.WriteString(formBoxStyle.Render(formContent.String()))
	b.WriteString("\n")

	if m.loading {
		b.WriteString(loadingStyle.Render("\u23f3 Aplicando bloqueio..."))
		b.WriteString("\n\n")
	}

	b.WriteString(footerStyle.Render(fmt.Sprintf(
		"%s  %s  %s",
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[enter]"),
			keyBindStyle.Render("Bloquear"),
		),
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[tab]"),
			keyBindStyle.Render("Pr\u00f3ximo"),
		),
		lipgloss.JoinHorizontal(lipgloss.Center,
			keyStyle.Render("[esc]"),
			keyBindStyle.Render("Cancelar"),
		),
	)))

	return appStyle.Render(b.String())
}

func Run(client *ipc.Client) error {
	m := New(client)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
