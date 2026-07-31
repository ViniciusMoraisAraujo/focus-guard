//go:build windows

package main

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

type focusGuardWatchdogService struct{}

func (h *focusGuardWatchdogService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[FocusGuard Watchdog] Pânico recuperado: %v", r)
			}
		}()
		watchLoop()
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for c := range r {
		switch c.Cmd {
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		default:
			continue
		}
	}

	return false, 0
}

func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

func runAsService() {
	if err := svc.Run("FocusGuardWatchdog", &focusGuardWatchdogService{}); err != nil {
		log.Printf("[FocusGuard Watchdog] svc.Run retornou: %v", err)
	}
}
