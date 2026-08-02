package tray

// MenuItem is a tray menu entry. Implementations must be safe for use from
// any goroutine.
type MenuItem interface {
	// Clicked returns a channel notified every time the item is clicked.
	Clicked() <-chan struct{}
	// AddSubMenuItem adds a nested sub-menu entry.
	AddSubMenuItem(title, tooltip string) MenuItem
	SetTitle(title string)
	SetTooltip(tooltip string)
	Enable()
	Disable()
}

// Systray is the notification-area abstraction used by Controller. All logic
// in this package is written against this interface so it can be tested
// without a real system tray.
type Systray interface {
	Run(onReady func(), onExit func())
	SetIcon(data []byte)
	SetTitle(title string)
	SetTooltip(tooltip string)
	// Notify shows a native balloon/toast (Windows) or desktop notification
	// (Linux notify-send). Implementations are best-effort: failures are
	// ignored because the tooltip still surfaces the same information.
	Notify(title, message string)
	AddMenuItem(title, tooltip string) MenuItem
	AddSeparator()
	Quit()
}
