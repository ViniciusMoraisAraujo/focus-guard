package tray

import (
	"fmt"
	"time"

	"focusguard/internal/ipc"
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
	systray     Systray
	daemon      Daemon
	openTUI     func()
	statusItem  MenuItem
	blockParent MenuItem
	updateItem  MenuItem
	tuiItem     MenuItem
	quitItem    MenuItem
	quickItems  []MenuItem

	// notifiedVersion records the last version a native notification was shown
	// for, so the tray does not spam the user on every poll for the same
	// release (Feature 2).
	notifiedVersion string
}

// NewController builds a tray controller.
func NewController(s Systray, d Daemon, openTUI func()) *Controller {
	return &Controller{systray: s, daemon: d, openTUI: openTUI}
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
	c.systray.AddSeparator()
	c.updateItem = c.systray.AddMenuItem("🔄 Verificar atualização", "Verificar e aplicar nova versão do daemon")
	c.systray.AddSeparator()
	c.tuiItem = c.systray.AddMenuItem("💻 Abrir TUI", "Abrir a interface de terminal")
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
		for range c.tuiItem.Clicked() {
			if c.openTUI != nil {
				c.openTUI()
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
	return tt
}
