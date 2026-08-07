package watchdog

import (
	"fmt"
	"net"
	"os"
	"time"
)

type HealthChecker interface {
	Ping() error
}

type Watchdog struct {
	socketPath string
	interval   time.Duration
	checker    HealthChecker
}

func New(checker HealthChecker, watchdogSec int) *Watchdog {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" || watchdogSec <= 0 {
		return nil
	}

	interval := time.Duration(watchdogSec/2) * time.Second

	return &Watchdog{
		socketPath: socket,
		interval:   interval,
		checker:    checker,
	}
}

func (w *Watchdog) Start() {
	if w == nil {
		return
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	_ = w.sendNotification("READY=1")

	for range ticker.C {
		if err := w.checker.Ping(); err != nil {
			continue
		}

		_ = w.sendNotification("WATCHDOG=1")
	}
}

func (w *Watchdog) sendNotification(state string) error {
	addr := &net.UnixAddr{
		Name: w.socketPath,
		Net:  "unixgram",
	}

	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return fmt.Errorf("watchdog: erro ao conectar no NOTIFY_SOCKET: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	return err
}
