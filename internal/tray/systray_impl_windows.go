//go:build windows

package tray

import (
	"fmt"
	"os/exec"

	"github.com/getlantern/systray"
)

type systrayImpl struct{}

// NewSystray returns the platform systray implementation.
func NewSystray() Systray { return &systrayImpl{} }

func (s *systrayImpl) Run(onReady, onExit func()) { systray.Run(onReady, onExit) }
func (s *systrayImpl) SetIcon(data []byte)        { systray.SetIcon(data) }
func (s *systrayImpl) SetTitle(title string)      { systray.SetTitle(title) }
func (s *systrayImpl) SetTooltip(tooltip string)  { systray.SetTooltip(tooltip) }

// Notify raises a native Windows balloon via PowerShell's WinForms
// NotifyIcon.ShowBalloonTip — the tray library used by FocusGuard does not
// expose Shell_NotifyIcon (NIF_INFO) publicly. Best-effort: a missing
// PowerShell or a failure is ignored (the tooltip still carries the info).
func (s *systrayImpl) Notify(title, message string) {
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.Visible = $true
$n.ShowBalloonTip(8000, %q, %q, [System.Windows.Forms.ToolTipIcon]::Info)
Start-Sleep -Seconds 9
$n.Dispose()
`, title, message)
	_ = exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script).Run()
}
func (s *systrayImpl) AddMenuItem(title, tooltip string) MenuItem {
	return &menuItemImpl{systray.AddMenuItem(title, tooltip)}
}
func (s *systrayImpl) AddSeparator() { systray.AddSeparator() }
func (s *systrayImpl) Quit()         { systray.Quit() }

type menuItemImpl struct{ inner *systray.MenuItem }

func (m *menuItemImpl) Clicked() <-chan struct{} { return m.inner.ClickedCh }
func (m *menuItemImpl) AddSubMenuItem(title, tooltip string) MenuItem {
	return &menuItemImpl{m.inner.AddSubMenuItem(title, tooltip)}
}
func (m *menuItemImpl) SetTitle(title string)     { m.inner.SetTitle(title) }
func (m *menuItemImpl) SetTooltip(tooltip string) { m.inner.SetTooltip(tooltip) }
func (m *menuItemImpl) Enable()                   { m.inner.Enable() }
func (m *menuItemImpl) Disable()                  { m.inner.Disable() }
