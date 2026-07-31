package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	return ok
}

func newTestModel(blocks []BlockInfo) *model {
	m := New(nil)
	m.loading = false
	m.blocks = blocks
	m.updateTableRows()
	return m
}

func TestModelInit(t *testing.T) {
	m := New(nil)
	if m.state != viewList {
		t.Errorf("expected state viewList, got %v", m.state)
	}
	if !m.loading {
		t.Error("expected loading true")
	}
	if m.client != nil {
		t.Error("expected client to be nil in test")
	}
	cmd := m.Init()
	if cmd == nil {
		t.Error("expected Init to return a command")
	}
}

func TestView_LoadingState(t *testing.T) {
	m := New(nil)
	m.loading = true
	view := m.View()
	if !strings.Contains(view, "Carregando") {
		t.Error("loading view should contain 'Carregando'")
	}
	if strings.Contains(view, "Nenhum bloqueio") {
		t.Error("loading view should not show empty message")
	}
}

func TestView_EmptyList(t *testing.T) {
	m := newTestModel(nil)
	view := m.View()
	if !strings.Contains(view, "Nenhum bloqueio ativo no momento") {
		t.Error("empty list view should show empty message")
	}
	if !strings.Contains(view, "[b]") {
		t.Error("view should show block keybind")
	}
	if !strings.Contains(view, "[q]") {
		t.Error("view should show quit keybind")
	}
}

func TestView_WithBlocks(t *testing.T) {
	now := time.Now()
	blocks := []BlockInfo{
		{
			Domain:    "twitter.com",
			StartedAt: now,
			ExpiresAt: now.Add(1 * time.Hour),
			Remaining: 1 * time.Hour,
			IsActive:  true,
		},
	}
	m := newTestModel(blocks)
	view := m.View()
	if !strings.Contains(view, "twitter.com") {
		t.Error("view should show blocked domain")
	}
	if strings.Contains(view, "Nenhum bloqueio ativo") {
		t.Error("view should not show empty message when blocks exist")
	}
}

func TestView_WithInactiveBlock(t *testing.T) {
	now := time.Now()
	blocks := []BlockInfo{
		{
			Domain:    "expired.com",
			StartedAt: now.Add(-2 * time.Hour),
			ExpiresAt: now.Add(-1 * time.Hour),
			Remaining: 0,
			IsActive:  false,
		},
	}
	m := newTestModel(blocks)
	m.updateTableRows()
	view := m.View()
	if strings.Contains(view, "expired.com") {
		t.Error("view should not show inactive blocks")
	}
}

func TestView_FormState(t *testing.T) {
	m := New(nil)
	m.loading = false
	m.state = viewForm
	m.domainInput.Focus()
	view := m.View()
	if !strings.Contains(view, "Novo Bloqueio") {
		t.Error("form view should show 'Novo Bloqueio' header")
	}
	if !strings.Contains(view, "Dom") {
		t.Error("form view should show domain label")
	}
	if !strings.Contains(view, "Dur") {
		t.Error("form view should show duration label")
	}
	if !strings.Contains(view, "[esc]") {
		t.Error("form view should show cancel keybind")
	}
}

func TestKey_Q_Quits(t *testing.T) {
	m := New(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !isQuitCmd(cmd) {
		t.Error("pressing 'q' should return a quit command")
	}
}

func TestKey_CtrlC_Quits(t *testing.T) {
	m := New(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuitCmd(cmd) {
		t.Error("ctrl+c should return a quit command")
	}
}

func TestKey_B_OpensForm(t *testing.T) {
	m := newTestModel(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd != nil {
		t.Error("pressing 'b' should return nil command")
	}
	if m.state != viewForm {
		t.Error("pressing 'b' should switch to form state")
	}
	if !m.domainInput.Focused() {
		t.Error("domain input should be focused when form opens")
	}
}

func TestKey_Esc_ClosesForm(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if m.state != viewList {
		t.Error("pressing esc in form should return to list")
	}
}

func TestKey_FormTab_NavigatesFields(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if m.formFocus != 0 {
		t.Errorf("expected formFocus 0 after opening form, got %d", m.formFocus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.formFocus != 1 {
		t.Errorf("expected formFocus 1 after tab, got %d", m.formFocus)
	}
	if !m.durationInput.Focused() {
		t.Error("duration input should be focused after tab")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.formFocus != 0 {
		t.Errorf("expected formFocus 0 after shift+tab, got %d", m.formFocus)
	}
	if !m.domainInput.Focused() {
		t.Error("domain input should be focused after shift+tab")
	}
}

func TestKey_FormEmptyEnterShowsError(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("empty form submit should return nil command")
	}
	if m.err == nil {
		t.Error("empty form submit should set error")
	}
}

func TestKey_FormEnterDispatchesBlock(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.domainInput.SetValue("twitter.com")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.durationInput.SetValue("4h")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("submit with valid fields should return a command")
	}
	if !m.loading {
		t.Error("should set loading true when blocking")
	}
}

func TestKey_FormCtrlC_BlockedDuringLoading(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.loading = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Error("ctrl+c while loading should not return an error; it should be silently ignored per current behavior")
	}
	if m.state != viewForm {
		t.Error("should remain in form state")
	}
}

func TestMessage_StatusFetched(t *testing.T) {
	m := New(nil)
	m.loading = true
	now := time.Now()
	msg := statusFetchedMsg{
		blocks: []BlockInfo{
			{
				Domain:    "test.com",
				StartedAt: now,
				ExpiresAt: now.Add(1 * time.Hour),
				Remaining: 1 * time.Hour,
				IsActive:  true,
			},
		},
	}
	result, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("statusFetchedMsg should return nil command")
	}
	if result != m {
		t.Error("should return same model")
	}
	if m.loading {
		t.Error("loading should be false after status fetched")
	}
	if m.err != nil {
		t.Error("error should be nil after success")
	}
	if len(m.blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(m.blocks))
	}
}

func TestMessage_FetchErr(t *testing.T) {
	m := New(nil)
	m.loading = true
	m.statusMessage = "old success message"
	msg := fetchErrMsg{err: errors.New("connection refused")}
	result, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("fetchErrMsg should return nil command")
	}
	if result != m {
		t.Error("should return same model")
	}
	if m.loading {
		t.Error("loading should be false after error")
	}
	if m.err == nil {
		t.Fatal("error should be set")
	}
	if m.err.Error() != "connection refused" {
		t.Errorf("expected 'connection refused', got %v", m.err)
	}
	if m.statusMessage != "" {
		t.Error("statusMessage should be cleared on fetch error")
	}
}

func TestMessage_BlockApplied(t *testing.T) {
	m := newTestModel(nil)
	m.state = viewForm
	m.loading = true
	msg := blockAppliedMsg{message: "twitter.com blocked successfully"}
	result, cmd := m.Update(msg)
	if result != m {
		t.Error("should return same model")
	}
	if m.state != viewList {
		t.Error("should return to list view after successful block")
	}
	if m.statusMessage != "twitter.com blocked successfully" {
		t.Errorf("statusMessage should be set, got %q", m.statusMessage)
	}
	if m.err != nil {
		t.Error("error should be nil after success")
	}
	if m.loading {
		t.Error("loading should be false after success")
	}
	if cmd == nil {
		t.Error("should dispatch a refresh command after block")
	}
}

func TestMessage_BlockErr(t *testing.T) {
	m := newTestModel(nil)
	m.state = viewForm
	m.loading = true
	msg := blockErrMsg{err: errors.New("netsh firewall add falhou")}
	result, cmd := m.Update(msg)
	if cmd == nil {
		t.Error("blockErrMsg should dispatch a refresh command")
	}
	if result != m {
		t.Error("should return same model")
	}
	if m.loading {
		t.Error("loading should be false after error")
	}
	if m.state != viewList {
		t.Error("should return to list view after block error")
	}
	if m.err == nil {
		t.Fatal("error should be set")
	}
	if m.err.Error() != "não foi possível aplicar o bloqueio. Verifique se o daemon está em execução e tente novamente" {
		t.Errorf("expected user-friendly error message, got %v", m.err)
	}
	if m.statusMessage != "" {
		t.Error("statusMessage should be cleared on block error")
	}
}

func TestKey_R_Refreshes(t *testing.T) {
	m := newTestModel(nil)
	m.loading = false
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Error("pressing 'r' should return a refresh command")
	}
	if !m.loading {
		t.Error("loading should be true after refresh")
	}
}

func TestKey_R_NoRefreshWhileLoading(t *testing.T) {
	m := newTestModel(nil)
	m.loading = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		t.Error("pressing 'r' while loading should be ignored")
	}
}

func TestKey_FormTabup_NavigatesFields(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.formFocus != 0 {
		t.Errorf("expected formFocus 0, got %d", m.formFocus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.formFocus != 1 {
		t.Errorf("expected formFocus 1 after down, got %d", m.formFocus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.formFocus != 0 {
		t.Errorf("expected formFocus 0 after up, got %d", m.formFocus)
	}
}

func TestWindowSize_SetsDimensions(t *testing.T) {
	m := New(nil)
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	result, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("WindowSizeMsg should return nil command")
	}
	if result != m {
		t.Error("should return same model")
	}
	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("expected height 40, got %d", m.height)
	}
}

func TestView_WithError(t *testing.T) {
	m := newTestModel(nil)
	m.err = errors.New("daemon not running")
	view := m.View()
	if !strings.Contains(view, "daemon not running") {
		t.Error("view should show error message")
	}
}

func TestView_WithStatusMessage(t *testing.T) {
	m := newTestModel(nil)
	m.statusMessage = "Domain blocked"
	view := m.View()
	if !strings.Contains(view, "Domain blocked") {
		t.Error("view should show success message")
	}
}

func TestProtectionLine_Active(t *testing.T) {
	m := newTestModel(nil)
	m.dohActive = true
	m.firewallRules = 9

	line := m.protectionLine()
	if !strings.Contains(line, "ATIVA") {
		t.Errorf("protection line should show ATIVA, got: %s", line)
	}
	if !strings.Contains(line, "9 regra(s)") {
		t.Errorf("protection line should show rule count, got: %s", line)
	}
}

func TestProtectionLine_Inactive(t *testing.T) {
	m := newTestModel(nil)
	m.dohActive = false
	m.firewallRules = 0

	line := m.protectionLine()
	if !strings.Contains(line, "inativa") {
		t.Errorf("protection line should show inativa, got: %s", line)
	}
}

func TestProtectionLine_ExpectedButMissing(t *testing.T) {
	m := newTestModel(nil)
	m.expectedDoH = true
	m.dohActive = false

	line := m.protectionLine()
	if !strings.Contains(line, "esperada") {
		t.Errorf("protection line should warn when expected but missing, got: %s", line)
	}
}

func TestProtectionLine_ActiveWithoutBlocks(t *testing.T) {
	m := newTestModel(nil)
	m.expectedDoH = false
	m.dohActive = true

	line := m.protectionLine()
	if !strings.Contains(line, "sem bloqueios ativos") {
		t.Errorf("protection line should note no active blocks, got: %s", line)
	}
}

func TestProtectionLine_Error(t *testing.T) {
	m := newTestModel(nil)
	m.protectionError = "netsh: permission denied"

	line := m.protectionLine()
	if !strings.Contains(line, "erro ao consultar") {
		t.Errorf("protection line should show error state, got: %s", line)
	}
	if !strings.Contains(line, "netsh: permission denied") {
		t.Errorf("protection line should include error detail, got: %s", line)
	}
}

func TestMessage_FetchErr_ResetsProtection(t *testing.T) {
	m := newTestModel(nil)
	m.dohActive = true
	m.firewallRules = 5
	m.protectionError = "old error"

	_, cmd := m.Update(fetchErrMsg{err: errors.New("connection refused")})
	if cmd != nil {
		t.Error("fetchErrMsg should return nil command")
	}
	if m.dohActive {
		t.Error("dohActive should be reset on fetch error")
	}
	if m.firewallRules != 0 {
		t.Errorf("firewallRules should be reset on fetch error, got %d", m.firewallRules)
	}
	if m.protectionError != "" {
		t.Errorf("protectionError should be reset on fetch error, got %q", m.protectionError)
	}
}

func TestView_ShowsProtectionLine(t *testing.T) {
	m := newTestModel(nil)
	m.dohActive = true
	m.firewallRules = 4

	view := m.View()
	if !strings.Contains(view, "DoH/DoT:") {
		t.Error("list view should include the protection status line")
	}
}

func TestView_KeybindFooter(t *testing.T) {
	m := newTestModel(nil)
	view := m.View()
	checks := []string{"[b]", "Bloquear", "[r]", "Atualizar", "[q]", "Sair"}
	for _, c := range checks {
		if !strings.Contains(view, c) {
			t.Errorf("view should contain %q in footer", c)
		}
	}
}

func TestFormView_KeybindFooter(t *testing.T) {
	m := New(nil)
	m.loading = false
	m.state = viewForm
	m.domainInput.Focus()
	view := m.View()
	checks := []string{"[enter]", "Bloquear", "[tab]", "Cancelar"}
	for _, c := range checks {
		if !strings.Contains(view, c) {
			t.Errorf("form view should contain %q in footer", c)
		}
	}
}

func TestForm_LoadingBlocksSubmit(t *testing.T) {
	m := newTestModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.domainInput.SetValue("test.com")
	m.durationInput.SetValue("1h")
	m.loading = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("submit while loading should be blocked")
	}
}

func TestView_FormLoadingState(t *testing.T) {
	m := New(nil)
	m.loading = true
	m.state = viewForm
	m.domainInput.Focus()
	view := m.View()
	if !strings.Contains(view, "Aplicando bloqueio") {
		t.Error("form loading state should show 'Aplicando bloqueio'")
	}
}
