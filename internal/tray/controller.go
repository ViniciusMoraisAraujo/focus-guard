package tray

import (
	"fmt"

	"focusguard/internal/ipc"
)

const quickBlockDuration = "4h"

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
}

func (c *Controller) refreshStatus() {
	resp, err := c.daemon.Send(ipc.Request{Action: "status"})
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Daemon indisponível")
		return
	}
	c.systray.SetTooltip(statusTooltip(resp))
}

func (c *Controller) blockDomain(domain string) {
	_, err := c.daemon.Send(ipc.Request{Action: "block", Domain: domain, Duration: quickBlockDuration})
	c.refreshStatus()
	if err != nil {
		c.systray.SetTooltip("⚠ Falha ao bloquear " + domain)
	}
}

func (c *Controller) checkUpdate() {
	c.updateItem.Disable()
	defer c.updateItem.Enable()

	resp, err := c.daemon.Send(ipc.Request{Action: "update"})
	if err != nil || resp == nil {
		c.systray.SetTooltip("⚠ Falha ao verificar atualização")
		return
	}
	if resp.UpdateAvailable {
		c.systray.SetTooltip(fmt.Sprintf("🔄 Atualização aplicada: v%s → v%s", resp.CurrentVersion, resp.UpdateVersion))
		return
	}
	c.systray.SetTooltip(fmt.Sprintf("✔ Você está atualizado (v%s)", resp.CurrentVersion))
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
