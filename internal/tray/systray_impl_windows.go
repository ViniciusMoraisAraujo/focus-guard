//go:build windows

package tray

import "github.com/getlantern/systray"

type systrayImpl struct{}

// NewSystray returns the platform systray implementation.
func NewSystray() Systray { return &systrayImpl{} }

func (s *systrayImpl) Run(onReady, onExit func()) { systray.Run(onReady, onExit) }
func (s *systrayImpl) SetIcon(data []byte)        { systray.SetIcon(data) }
func (s *systrayImpl) SetTitle(title string)      { systray.SetTitle(title) }
func (s *systrayImpl) SetTooltip(tooltip string)  { systray.SetTooltip(tooltip) }
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
