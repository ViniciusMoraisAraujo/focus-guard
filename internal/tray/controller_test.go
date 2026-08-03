package tray

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/policy"
	"focusguard/internal/pomodoro"
	"focusguard/internal/preset"
)

// --- mocks ---

type mockMenuItem struct {
	mu       sync.Mutex
	title    string
	tooltip  string
	clicked  chan struct{}
	disabled bool
	children []*mockMenuItem
}

func newMockMenuItem(title, tooltip string) *mockMenuItem {
	return &mockMenuItem{title: title, tooltip: tooltip, clicked: make(chan struct{}, 10)}
}

func (m *mockMenuItem) Clicked() <-chan struct{} { return m.clicked }
func (m *mockMenuItem) AddSubMenuItem(title, tooltip string) MenuItem {
	child := newMockMenuItem(title, tooltip)
	m.children = append(m.children, child)
	return child
}
func (m *mockMenuItem) SetTitle(title string)     { m.title = title }
func (m *mockMenuItem) SetTooltip(tooltip string) { m.tooltip = tooltip }
func (m *mockMenuItem) Enable() {
	m.mu.Lock()
	m.disabled = false
	m.mu.Unlock()
}
func (m *mockMenuItem) Disable() {
	m.mu.Lock()
	m.disabled = true
	m.mu.Unlock()
}
func (m *mockMenuItem) isDisabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disabled
}
func (m *mockMenuItem) click() { m.clicked <- struct{}{} }

type mockSystray struct {
	mu         sync.Mutex
	iconCalls  int
	title      string
	tooltip    string
	items      []*mockMenuItem
	separators int
	quitCalls  int
	notifs     []string
}

func (m *mockSystray) Notify(title, message string) {
	m.mu.Lock()
	m.notifs = append(m.notifs, title+"|"+message)
	m.mu.Unlock()
}

func (m *mockSystray) notifications() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.notifs...)
}

func newMockSystray() *mockSystray { return &mockSystray{} }

func (m *mockSystray) Run(onReady, _ func()) { onReady() }
func (m *mockSystray) SetIcon(data []byte) {
	m.mu.Lock()
	m.iconCalls++
	m.mu.Unlock()
}
func (m *mockSystray) SetTitle(title string) {
	m.mu.Lock()
	m.title = title
	m.mu.Unlock()
}
func (m *mockSystray) SetTooltip(t string) {
	m.mu.Lock()
	m.tooltip = t
	m.mu.Unlock()
}
func (m *mockSystray) AddMenuItem(title, tooltip string) MenuItem {
	item := newMockMenuItem(title, tooltip)
	m.items = append(m.items, item)
	return item
}
func (m *mockSystray) AddSeparator() {
	m.mu.Lock()
	m.separators++
	m.mu.Unlock()
}
func (m *mockSystray) Quit() {
	m.mu.Lock()
	m.quitCalls++
	m.mu.Unlock()
}

func (m *mockSystray) getTooltip() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tooltip
}
func (m *mockSystray) getTitle() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.title
}
func (m *mockSystray) separatorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.separators
}
func (m *mockSystray) quitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.quitCalls
}
func (m *mockSystray) itemByTitle(title string) *mockMenuItem {
	for _, item := range m.items {
		if item.title == title {
			return item
		}
	}
	return nil
}

type mockDaemon struct {
	mu   sync.Mutex
	reqs []ipc.Request
	resp *ipc.Response
	err  error
}

func (m *mockDaemon) Send(req ipc.Request) (*ipc.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	return m.resp, m.err
}

func (m *mockDaemon) SendWithTimeout(req ipc.Request, _ time.Duration) (*ipc.Response, error) {
	return m.Send(req)
}

func (m *mockDaemon) requests() []ipc.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ipc.Request(nil), m.reqs...)
}

type blockingDaemon struct {
	mu      sync.Mutex
	reqs    []ipc.Request
	release chan struct{}
	resp    *ipc.Response
}

func (m *blockingDaemon) Send(req ipc.Request) (*ipc.Response, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	if req.Action == "update" {
		<-m.release
	}
	return m.resp, nil
}

func (m *blockingDaemon) SendWithTimeout(req ipc.Request, _ time.Duration) (*ipc.Response, error) {
	return m.Send(req)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condicao nao atingida dentro do timeout")
}

// --- controller tests ---

func TestController_BuildsMenuAndShowsStatus(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, DoHActive: true, FirewallRules: 7}}
	c := NewController(s, d, nil)
	c.Run()

	if s.iconCalls == 0 {
		t.Error("SetIcon nao foi chamado (o icone deve ser definido)")
	}
	if s.getTitle() != "FocusGuard" {
		t.Errorf("title = %q, want FocusGuard", s.getTitle())
	}
	for _, title := range []string{"📊 Status", "🚫 Bloco rápido", "🔄 Verificar atualização", "💻 Abrir TUI", "🚪 Sair"} {
		if s.itemByTitle(title) == nil {
			t.Errorf("menu item %q nao criado", title)
		}
	}
	if s.separatorCount() < 3 {
		t.Errorf("separators = %d, want >= 3", s.separatorCount())
	}
	blockParent := s.itemByTitle("🚫 Bloco rápido")
	if blockParent == nil || len(blockParent.children) != len(quickBlockDomains) {
		t.Fatalf("bloco rapido deve ter %d domínios, tem %d", len(quickBlockDomains), len(blockParent.children))
	}
	if !strings.Contains(s.getTooltip(), "DoH/DoT ATIVA · 7 regras") {
		t.Errorf("tooltip = %q, want status ATIVA", s.getTooltip())
	}
}

func TestController_StatusClickRefreshesTooltip(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, DoHActive: false, FirewallRules: 0}}
	c := NewController(s, d, nil)
	c.Run()

	// o polling de presets em background também fala com o daemon; contamos
	// apenas as requests status para não depender da ordem das goroutines
	statusCount := func() int {
		n := 0
		for _, req := range d.requests() {
			if req.Action == "status" {
				n++
			}
		}
		return n
	}
	if got := statusCount(); got != 1 {
		t.Fatalf("esperava fetch inicial de status, got %d requests status", got)
	}

	status := s.itemByTitle("📊 Status")
	status.click()

	waitFor(t, func() bool { return statusCount() >= 2 })
}

func TestController_QuickBlockSendsRequest(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, DoHActive: true, FirewallRules: 9}}
	c := NewController(s, d, nil)
	c.Run()

	blockParent := s.itemByTitle("🚫 Bloco rápido")
	blockParent.children[0].click()

	waitFor(t, func() bool {
		for _, req := range d.requests() {
			if req.Action == "block" {
				return true
			}
		}
		return false
	})

	var blockReq ipc.Request
	for _, req := range d.requests() {
		if req.Action == "block" {
			blockReq = req
		}
	}
	if blockReq.Domain != quickBlockDomains[0] {
		t.Errorf("domain = %q, want %q", blockReq.Domain, quickBlockDomains[0])
	}
	if blockReq.Duration != quickBlockDuration {
		t.Errorf("duration = %q, want %q", blockReq.Duration, quickBlockDuration)
	}
	waitFor(t, func() bool { return strings.Contains(s.getTooltip(), "9 regras") })
}

func TestController_UpdateClickDisablesItemWhileRunning(t *testing.T) {
	s := newMockSystray()
	d := &blockingDaemon{release: make(chan struct{}), resp: &ipc.Response{Success: true, CurrentVersion: "1.0.0"}}
	c := NewController(s, d, nil)
	c.Run()

	update := s.itemByTitle("🔄 Verificar atualização")
	update.click()

	waitFor(t, func() bool { return update.isDisabled() })
	close(d.release)
	waitFor(t, func() bool { return !update.isDisabled() })

	d.mu.Lock()
	defer d.mu.Unlock()
	found := false
	for _, req := range d.reqs {
		if req.Action == "update" {
			found = true
		}
	}
	if !found {
		t.Errorf("esperava request update, got %+v", d.reqs)
	}
}

func TestController_QuitClick(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true}}
	c := NewController(s, d, nil)
	c.Run()

	s.itemByTitle("🚪 Sair").click()
	waitFor(t, func() bool { return s.quitCount() == 1 })
}

func TestController_DaemonDownShowsTooltip(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{err: errDaemonDown}
	c := NewController(s, d, nil)
	c.Run()

	if !strings.Contains(s.getTooltip(), "Daemon indisponível") {
		t.Errorf("tooltip = %q, want daemon indisponivel", s.getTooltip())
	}
}

func TestController_OpenTUIIsCalledOnClick(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true}}
	opened := make(chan struct{})
	c := NewController(s, d, func() { close(opened) })
	c.Run()

	s.itemByTitle("💻 Abrir TUI").click()
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("openTUI nao foi chamado")
	}
}

func TestStatusTooltip(t *testing.T) {
	cases := []struct {
		name string
		resp *ipc.Response
		want string
	}{
		{"ativa", &ipc.Response{DoHActive: true, FirewallRules: 7}, "DoH/DoT ATIVA · 7 regras"},
		{"inativa", &ipc.Response{DoHActive: false}, "DoH/DoT INATIVA · 0 regras"},
		{"atualizacao", &ipc.Response{DoHActive: true, FirewallRules: 3, UpdateAvailable: true, UpdateVersion: "0.3.0"}, "v0.3.0 disponível"},
		{"erro firewall", &ipc.Response{ProtectionError: "elevacao"}, "Não foi possível consultar o firewall"},
		{"bloqueio ativo", &ipc.Response{
			DoHActive: true, FirewallRules: 4,
			Blocks: []policy.Block{
				{Domain: "twitter.com", StartedAt: time.Now(), ExpiresAt: time.Now().Add(90 * time.Minute)},
				{Domain: "youtube.com", StartedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute)},
			},
		}, "twitter.com por mais 1h30m"},
		{"bloqueio curto", &ipc.Response{
			DoHActive: true, FirewallRules: 4,
			Blocks: []policy.Block{{Domain: "reddit.com", StartedAt: time.Now(), ExpiresAt: time.Now().Add(5 * time.Minute)}},
		}, "reddit.com por mais 5m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := statusTooltip(tc.resp)
			if !strings.Contains(got, tc.want) {
				t.Errorf("statusTooltip = %q, want contains %q", got, tc.want)
			}
		})
	}
}

func TestStatusTooltip_NoBlockNoExtra(t *testing.T) {
	got := statusTooltip(&ipc.Response{DoHActive: true, FirewallRules: 2})
	if strings.Contains(got, "por mais") {
		t.Errorf("no block remaining info expected, got %q", got)
	}
}

var errDaemonDown = &daemonDownError{}

type daemonDownError struct{}

func (*daemonDownError) Error() string { return "connection refused" }

// --- regressions: daemon rejecting actions / IPC timeouts ---

func TestController_BlockFailureShowsDaemonMessage(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: false, Message: "falha ao resolver domínio"}}
	c := NewController(s, d, nil)
	c.Run()

	blockParent := s.itemByTitle("🚫 Bloco rápido")
	blockParent.children[0].click()

	// o tooltip inicial de status ja contem a mensagem do daemon, entao
	// assertamos o texto acao-especifico para exercitar de fato o blockDomain
	waitFor(t, func() bool {
		tt := s.getTooltip()
		return strings.Contains(tt, "Falha ao bloquear") && strings.Contains(tt, "falha ao resolver domínio")
	})
}

func TestController_UpdateFailureDoesNotClaimUpToDate(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: false, Message: "auto-update não configurado"}}
	c := NewController(s, d, nil)
	c.Run()

	s.itemByTitle("🔄 Verificar atualização").click()

	waitFor(t, func() bool {
		tt := s.getTooltip()
		return strings.Contains(tt, "Falha ao verificar atualização") && !strings.Contains(tt, "✔ Você está atualizado")
	})
}

func TestController_StatusFailureShowsError(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: false, Message: "erro ao consultar firewall"}}
	c := NewController(s, d, nil)
	c.Run()

	waitFor(t, func() bool {
		tt := s.getTooltip()
		return strings.Contains(tt, "erro ao consultar firewall") && !strings.Contains(tt, "🛡 FocusGuard")
	})
}

type timeoutRecordingDaemon struct {
	mu        sync.Mutex
	timeouts  []time.Duration
	sendCalls int
}

func (m *timeoutRecordingDaemon) Send(ipc.Request) (*ipc.Response, error) {
	m.mu.Lock()
	m.sendCalls++
	m.mu.Unlock()
	return nil, errors.New("controller must use SendWithTimeout")
}

func (m *timeoutRecordingDaemon) SendWithTimeout(_ ipc.Request, d time.Duration) (*ipc.Response, error) {
	m.mu.Lock()
	m.timeouts = append(m.timeouts, d)
	m.mu.Unlock()
	return &ipc.Response{Success: true, DoHActive: true, FirewallRules: 3}, nil
}

// ---------------------------------------------------------------------------
// Notificações nativas de update (Feature 2 — polling no tray)
// ---------------------------------------------------------------------------

func TestCheckForUpdateNotification_NotifiesOnNewVersion(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, UpdateAvailable: true, UpdateVersion: "1.1.0", CurrentVersion: "1.0.0"}}
	c := NewController(s, d, nil)

	c.checkForUpdateNotification()

	notifs := s.notifications()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d: %v", len(notifs), notifs)
	}
	if !strings.Contains(notifs[0], "Nova versão do FocusGuard") {
		t.Errorf("notification should carry the update title, got %q", notifs[0])
	}
	if !strings.Contains(notifs[0], "1.1.0") {
		t.Errorf("notification should mention the new version, got %q", notifs[0])
	}
	if !strings.Contains(notifs[0], "focusguard update") {
		t.Errorf("notification should tell the user how to update, got %q", notifs[0])
	}
}

func TestCheckForUpdateNotification_NotifiesOncePerVersion(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, UpdateAvailable: true, UpdateVersion: "1.1.0"}}
	c := NewController(s, d, nil)

	c.checkForUpdateNotification()
	c.checkForUpdateNotification()
	c.checkForUpdateNotification()

	if got := len(s.notifications()); got != 1 {
		t.Errorf("expected a single notification for the same version, got %d", got)
	}
}

func TestCheckForUpdateNotification_NoNotifyWithoutUpdate(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, UpdateAvailable: false}}
	c := NewController(s, d, nil)

	c.checkForUpdateNotification()

	if got := len(s.notifications()); got != 0 {
		t.Errorf("no notification expected without an update, got %d", got)
	}
}

func TestCheckForUpdateNotification_NoNotifyWhenDaemonDown(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{err: errDaemonDown}
	c := NewController(s, d, nil)

	c.checkForUpdateNotification()

	if got := len(s.notifications()); got != 0 {
		t.Errorf("no notification expected when the daemon is down, got %d", got)
	}
}

func TestCheckForUpdateNotification_NotifiesAgainOnNewerVersion(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{Success: true, UpdateAvailable: true, UpdateVersion: "1.1.0"}}
	c := NewController(s, d, nil)

	c.checkForUpdateNotification()

	d.mu.Lock()
	d.resp = &ipc.Response{Success: true, UpdateAvailable: true, UpdateVersion: "1.2.0"}
	d.mu.Unlock()
	c.checkForUpdateNotification()

	if got := len(s.notifications()); got != 2 {
		t.Errorf("expected a new notification for v1.2.0, got %d: %v", got, s.notifications())
	}
}

// ---------------------------------------------------------------------------
// Categorias no tray (presets personalizados + builtins)
// ---------------------------------------------------------------------------

func TestController_PresetSubmenu_BlocksPreset(t *testing.T) {
	s := newMockSystray()
	// os presets entram na resposta ANTES do Run(): populatePresetMenu roda em
	// goroutine e o mock responde o mesmo para tudo — injetar depois deixaria
	// o submenu em "(indisponível)" por causa do race
	d := &mockDaemon{resp: &ipc.Response{Success: true, Presets: []preset.Preset{
		{Name: "social", Label: "Redes sociais", Domains: []string{"twitter.com"}},
		{Name: "estudo", Label: "Estudo", Domains: []string{"khanacademy.org"}},
	}}}
	c := NewController(s, d, nil)
	c.Run()

	presetParent := s.itemByTitle("🗂 Categorias")
	if presetParent == nil {
		t.Fatal("menu Categorias não criado")
	}
	// espera o submenu ser populado em background
	waitFor(t, func() bool { return len(presetParent.children) == 2 })

	presetParent.children[0].click()

	waitFor(t, func() bool {
		for _, req := range d.requests() {
			if req.Action == "block" && req.Preset == "social" {
				return true
			}
		}
		return false
	})
	for _, req := range d.requests() {
		if req.Action == "block" && req.Preset == "social" {
			if req.Duration != quickBlockDuration {
				t.Errorf("duration = %q, want %q", req.Duration, quickBlockDuration)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Notificações de transição do pomodoro (Feature 5)
// ---------------------------------------------------------------------------

func TestPomodoroNotify_WorkStart(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{
		Success:  true,
		Pomodoro: nil, // baseline: sem sessão ativa
	}}
	c := NewController(s, d, nil)

	// primeira observação define o baseline — sem notificação
	c.checkPomodoroNotification()
	if got := len(s.notifications()); got != 0 {
		t.Fatalf("primeira observação não deve notificar, got %d", got)
	}

	// transição inativa → trabalho notifica
	d.mu.Lock()
	d.resp.Pomodoro = &pomodoro.State{Active: true, Phase: pomodoro.PhaseWork, Cycle: 1, Cycles: 4}
	d.mu.Unlock()
	c.checkPomodoroNotification()

	notifs := s.notifications()
	if len(notifs) != 1 {
		t.Fatalf("esperava 1 notificação de trabalho, got %d: %v", len(notifs), notifs)
	}
	if !strings.Contains(notifs[0], "trabalhar") || !strings.Contains(notifs[0], "1/4") {
		t.Errorf("notificação de trabalho inesperada: %q", notifs[0])
	}
}

func TestPomodoroNotify_WorkToRest(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{
		Success:  true,
		Pomodoro: &pomodoro.State{Active: true, Phase: pomodoro.PhaseWork, Cycle: 1, Cycles: 4},
	}}
	c := NewController(s, d, nil)
	c.checkPomodoroNotification() // baseline: trabalho

	d.mu.Lock()
	d.resp.Pomodoro = &pomodoro.State{Active: true, Phase: pomodoro.PhaseRest, Cycle: 1, Cycles: 4}
	d.mu.Unlock()
	c.checkPomodoroNotification()

	notifs := s.notifications()
	if len(notifs) != 1 || !strings.Contains(notifs[0], "descanso") {
		t.Errorf("esperava notificação de descanso, got %v", notifs)
	}
}

func TestPomodoroNotify_SessionEnd(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{
		Success:  true,
		Pomodoro: &pomodoro.State{Active: true, Phase: pomodoro.PhaseWork, Cycle: 4, Cycles: 4},
	}}
	c := NewController(s, d, nil)
	c.checkPomodoroNotification() // baseline: ativo

	d.mu.Lock()
	d.resp.Pomodoro = &pomodoro.State{Active: false}
	d.mu.Unlock()
	c.checkPomodoroNotification()

	notifs := s.notifications()
	if len(notifs) != 1 || !strings.Contains(notifs[0], "concluída") {
		t.Errorf("esperava notificação de conclusão, got %v", notifs)
	}
}

func TestPomodoroNotify_NoSpamOnSamePhase(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{resp: &ipc.Response{
		Success:  true,
		Pomodoro: &pomodoro.State{Active: true, Phase: pomodoro.PhaseWork, Cycle: 2, Cycles: 4},
	}}
	c := NewController(s, d, nil)
	c.checkPomodoroNotification()
	c.checkPomodoroNotification()
	c.checkPomodoroNotification()

	if got := len(s.notifications()); got != 0 {
		t.Errorf("sem transição não deve notificar, got %d", got)
	}
}

func TestPomodoroNotify_NoNotifyWhenDaemonDown(t *testing.T) {
	s := newMockSystray()
	d := &mockDaemon{err: errDaemonDown}
	c := NewController(s, d, nil)
	c.checkPomodoroNotification()
	c.checkPomodoroNotification()

	if got := len(s.notifications()); got != 0 {
		t.Errorf("sem daemon não deve notificar, got %d", got)
	}
}

func TestController_UsesTimeoutForAllIPCCalls(t *testing.T) {
	s := newMockSystray()
	d := &timeoutRecordingDaemon{}
	c := NewController(s, d, nil)
	c.Run()

	// o fetch inicial de status deve usar SendWithTimeout com deadline positivo
	waitFor(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.timeouts) >= 1
	})

	s.itemByTitle("🚪 Sair").click()
	waitFor(t, func() bool { return s.quitCount() == 1 })

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sendCalls != 0 {
		t.Errorf("Send chamado %d vez(es); todo IPC deve usar SendWithTimeout", d.sendCalls)
	}
	for i, to := range d.timeouts {
		if to <= 0 {
			t.Errorf("timeout[%d] = %v, esperava deadline positivo", i, to)
		}
	}
}
