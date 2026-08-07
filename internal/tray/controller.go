package tray

import (
	"fmt"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/domain/pomodoro"
	"focusguard/internal/transport/ipc"
)

const (
	quickBlockDuration = "4h"
	// daemonTimeout bounds every IPC call from the tray. The systray library
	// delivers clicks through a non-blocking channel: if a handler blocks
	// forever on a hung daemon, all subsequent clicks are silently dropped
	// and the tray appears dead.
	daemonTimeout = 5 * time.Second
	// updatePollInterval is how often the tray re-checks for a new update in
	// the background (Feature 2). 30min strikes a balance between noticing
	// releases promptly and not hammering the daemon.
	updatePollInterval = 30 * time.Minute
	// pomodoroPollInterval is how often the tray samples the pomodoro state to
	// raise native notifications on work/rest transitions (Feature 5). 10s is
	// fine-grained enough for 25-min phases while staying cheap on IPC.
	pomodoroPollInterval = 10 * time.Second
)

var quickBlockDomains = []string{
	"youtube.com",
	"twitter.com",
	"instagram.com",
	"tiktok.com",
	"reddit.com",
	"netflix.com",
}

// Daemon is the IPC surface used by the tray controller.
type Daemon interface {
	Send(req ipc.Request) (*ipc.Response, error)
	SendWithTimeout(req ipc.Request, timeout time.Duration) (*ipc.Response, error)
}

// Controller wires a Systray to a Daemon. All of its logic is plain Go and
// fully testable with mocks.
type Controller struct {
	systray      Systray
	daemon       Daemon
	openPanel    func()
	statusItem   MenuItem
	blockParent  MenuItem
	presetParent MenuItem
	updateItem   MenuItem
	panelItem    MenuItem
	quitItem     MenuItem
	quickItems   []MenuItem

	// notifiedVersion records the last version a native notification was shown
	// for, so the tray does not spam the user on every poll for the same
	// release (Feature 2).
	notifiedVersion string
	// lastPomo/pomoSeen track the last observed pomodoro state so work/rest
	// transitions raise exactly one native notification each (Feature 5).
	lastPomo pomodoro.State
	pomoSeen bool
}

// NewController builds a tray controller.
func NewController(s Systray, d Daemon, openPanel func()) *Controller {
	return &Controller{systray: s, daemon: d, openPanel: openPanel}
}

// Run blocks until the tray exits.
func (c *Controller) Run() {
	c.systray.Run(c.onReady, nil)
}

func (c *Controller) onReady() {
	c.systray.SetIcon(platformIcon())
	c.systray.SetTitle("FocusGuard")
	c.systray.SetTooltip("FocusGuard — carregando…")

	c.statusItem = c.systray.AddMenuItem("📊 Status", "Atualizar o status da proteção")
	c.blockParent = c.systray.AddMenuItem("🚫 Bloco rápido", "Bloquear um domínio por "+quickBlockDuration)
	for i := range quickBlockDomains {
		domain := quickBlockDomains[i]
		c.quickItems = append(c.quickItems, c.blockParent.AddSubMenuItem(domain, "Bloquear "+domain))
	}
	c.presetParent = c.systray.AddMenuItem("🗂 Categorias", "Bloquear uma categoria (preset) por "+quickBlockDuration)
	go c.populatePresetMenu()
	c.systray.AddSeparator()
	c.updateItem = c.systray.AddMenuItem("🔄 Verificar atualização", "Verificar e aplicar nova versão do daemon")
	c.systray.AddSeparator()
	c.panelItem = c.systray.AddMenuItem("🌐 Abrir painel", "Abrir a interface web no navegador")
	c.systray.AddSeparator()
	c.quitItem = c.systray.AddMenuItem("🚪 Sair", "Sair do FocusGuard (o daemon continua rodando)")

	go func() {
		for range c.statusItem.Clicked() {
			c.refreshStatus()
		}
	}()
	for i := range c.quickItems {
		domain := quickBlockDomains[i]
		item := c.quickItems[i]
		go func() {
			for range item.Clicked() {
				c.blockDomain(domain)
			}
		}()
	}
	go func() {
		for range c.updateItem.Clicked() {
			c.checkUpdate()
		}
	}()
	go func() {
		for range c.panelItem.Clicked() {
			if c.openPanel != nil {
				c.openPanel()
			}
		}
	}()
	go func() {
		for range c.quitItem.Clicked() {
			c.systray.Quit()
		}
	}()

	c.refreshStatus()
	c.startUpdatePolling()
	c.startPomodoroPolling()
}

// populatePresetMenu fetches the preset catalog (built-ins + user presets) from
// the daemon and fills the "Categorias" submenu. Best-effort: an unreachable
// daemon leaves the submenu titled "indisponível" without breaking the tray.
func (c *Controller) populatePresetMenu() {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "presets"}, daemonTimeout)
	if err != nil || resp == nil || !resp.Success || len(resp.Presets) == 0 {
		c.presetParent.SetTitle("🗂 Categorias (indisponível)")
		return
	}
	for i := range resp.Presets {
		p := resp.Presets[i]
		item := c.presetParent.AddSubMenuItem(p.Label, "Bloquear "+p.Label+" por "+quickBlockDuration)
		go func(name string) {
			for range item.Clicked() {
				c.blockPreset(name)
			}
		}(p.Name)
	}
}

// blockPreset blocks a whole category for the quick duration via the daemon.
func (c *Controller) blockPreset(presetName string) {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "block", Preset: presetName, Duration: quickBlockDuration}, daemonTimeout)
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Falha ao bloquear categoria " + presetName)
		return
	}
	if !resp.Success {
		c.systray.SetTooltip(errorTooltip("Falha ao bloquear "+presetName, resp))
		return
	}
	c.refreshStatus()
}

// startPomodoroPolling samples the pomodoro state periodically so work/rest
// transitions raise a native notification (Feature 5).
func (c *Controller) startPomodoroPolling() {
	ticker := time.NewTicker(pomodoroPollInterval)
	go func() {
		for range ticker.C {
			c.checkPomodoroNotification()
		}
	}()
}

// checkPomodoroNotification observes the pomodoro state from the daemon status
// and notifies exactly once per transition: session start (work), work→rest,
// rest→work and session end. The first observation only sets the baseline.
func (c *Controller) checkPomodoroNotification() {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "status"}, daemonTimeout)
	if err != nil || resp == nil || !resp.Success {
		return
	}
	st := resp.Pomodoro
	if st == nil {
		st = &pomodoro.State{}
	}
	prev := c.lastPomo
	c.lastPomo = *st
	if !c.pomoSeen {
		c.pomoSeen = true
		return
	}

	switch {
	case !prev.Active && st.Active:
		c.systray.Notify("Pomodoro", fmt.Sprintf("Hora de trabalhar! Ciclo %d/%d (%s)", st.Cycle, st.Cycles, pomoPhaseLabel(st.Phase)))
	case prev.Active && st.Active && prev.Phase != st.Phase:
		if st.Phase == pomodoro.PhaseRest {
			c.systray.Notify("Pomodoro", fmt.Sprintf("Pausa para descanso! Ciclo %d/%d", st.Cycle, st.Cycles))
		} else {
			c.systray.Notify("Pomodoro", fmt.Sprintf("Volta ao trabalho! Ciclo %d/%d", st.Cycle, st.Cycles))
		}
	case prev.Active && !st.Active:
		c.systray.Notify("Pomodoro", "Sessão concluída! 🎉")
	}
}

func pomoPhaseLabel(p pomodoro.Phase) string {
	if p == pomodoro.PhaseRest {
		return "descanso"
	}
	return "trabalho"
}

// startUpdatePolling runs the background update check (Feature 2): every
// updatePollInterval the tray asks the daemon's cached status and raises a
// native notification when a new version is available.
func (c *Controller) startUpdatePolling() {
	ticker := time.NewTicker(updatePollInterval)
	go func() {
		for range ticker.C {
			c.checkForUpdateNotification()
		}
	}()
}

// checkForUpdateNotification queries the daemon status and raises a native
// notification (balloon/toast) the first time a new version is seen. It is a
// no-op when the daemon is unreachable, the request fails, or the version was
// already notified.
func (c *Controller) checkForUpdateNotification() {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "status"}, daemonTimeout)
	if err != nil || resp == nil || !resp.Success {
		return
	}
	if !resp.UpdateAvailable || resp.UpdateVersion == "" {
		return
	}
	if resp.UpdateVersion == c.notifiedVersion {
		return
	}
	c.notifiedVersion = resp.UpdateVersion
	c.systray.Notify(
		"Nova versão do FocusGuard",
		fmt.Sprintf("v%s disponível. Abra o terminal e digite 'focusguard update' para atualizar.", resp.UpdateVersion),
	)
}

func (c *Controller) refreshStatus() {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "status"}, daemonTimeout)
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Daemon indisponível")
		return
	}
	if !resp.Success {
		c.systray.SetTooltip(errorTooltip("Falha ao consultar status", resp))
		return
	}
	c.systray.SetTooltip(statusTooltip(resp))
}

func (c *Controller) blockDomain(domain string) {
	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "block", Domain: domain, Duration: quickBlockDuration}, daemonTimeout)
	// Em caso de falha o tooltip de erro é o estado final; só em sucesso
	// atualiza o status (evita round-trip extra quando o daemon está fora).
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Falha ao bloquear " + domain)
		return
	}
	if !resp.Success {
		c.systray.SetTooltip(errorTooltip("Falha ao bloquear "+domain, resp))
		return
	}
	c.refreshStatus()
}

func (c *Controller) checkUpdate() {
	c.updateItem.Disable()
	defer c.updateItem.Enable()

	resp, err := c.daemon.SendWithTimeout(ipc.Request{Action: "update"}, daemonTimeout)
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Falha ao verificar atualização")
		return
	}
	if !resp.Success {
		c.systray.SetTooltip(errorTooltip("Falha ao verificar atualização", resp))
		return
	}
	// PendingReboot vem ANTES de UpdateAvailable: o daemon mantém Available=true
	// no fallback move-on-reboot (o update existe, só será aplicado no boot) —
	// se invertesse a ordem, diria "aplicada" indevidamente.
	if resp.UpdatePendingReboot {
		c.systray.SetTooltip(fmt.Sprintf("🔄 Atualização preparada: v%s → v%s (conclui no próximo reinício)", resp.CurrentVersion, resp.UpdateVersion))
		return
	}
	if resp.UpdateAvailable {
		c.systray.SetTooltip(fmt.Sprintf("🔄 Atualização aplicada: v%s → v%s", resp.CurrentVersion, resp.UpdateVersion))
		return
	}
	c.systray.SetTooltip(fmt.Sprintf("✔ Você está atualizado (v%s)", resp.CurrentVersion))
}

// errorTooltip builds a tooltip for a response the daemon rejected (Success
// false), surfacing the daemon's own Message when present.
func errorTooltip(action string, resp *ipc.Response) string {
	if resp.Message != "" {
		return "⚠ " + action + ": " + resp.Message
	}
	return "⚠ " + action
}

func statusTooltip(resp *ipc.Response) string {
	if resp.ProtectionError != "" {
		return "⚠ Não foi possível consultar o firewall"
	}
	state := "INATIVA"
	if resp.DoHActive {
		state = "ATIVA"
	}
	tt := fmt.Sprintf("🛡 FocusGuard — DoH/DoT %s · %d regras", state, resp.FirewallRules)
	if resp.UpdateAvailable {
		tt += fmt.Sprintf(" · 🔄 v%s disponível", resp.UpdateVersion)
	}
	if longest := longestBlockRemaining(resp.Blocks); longest != "" {
		tt += " · 🚫 " + longest
	}
	return tt
}

// longestBlockRemaining describes the active block with the most time left
// ("domain por mais 1h30m"), or "" when there are no active blocks.
func longestBlockRemaining(blocks []policy.Block) string {
	var (
		best    *policy.Block
		bestDur time.Duration
	)
	for i := range blocks {
		if !blocks[i].IsActive() {
			continue
		}
		if d := blocks[i].RemainingTime(); d > bestDur {
			bestDur = d
			best = &blocks[i]
		}
	}
	if best == nil {
		return ""
	}
	return best.Domain + " por mais " + formatDuration(bestDur)
}

// formatDuration renders a duration compactly as "1h30m" / "45m" / "2h",
// rounding up to the next minute so a 89m59s block still reads "1h30m" (the
// time between computing RemainingTime and rendering would otherwise show
// 1h29m for a freshly created 90-minute block).
func formatDuration(d time.Duration) string {
	d += time.Minute - time.Nanosecond // ceil até o minuto seguinte
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
