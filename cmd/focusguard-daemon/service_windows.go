//go:build windows

package main

import (
	"log"
	"time"

	"golang.org/x/sys/windows/svc"
)

type focusGuardService struct{}

func (h *focusGuardService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	go func() {
		for {
			shouldExit := runDaemon()
			if shouldExit {
				log.Println("[FocusGuard Daemon] Serviço encerrado normalmente.")
				close(daemonDoneCh)
				return
			}
			log.Println("[FocusGuard Daemon] (serviço) Reiniciando...")
			time.Sleep(1 * time.Second)
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for c := range r {
		switch c.Cmd {
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			close(serviceStopCh)

			select {
			case <-daemonDoneCh:
				log.Println("[FocusGuard Daemon] Serviço finalizado após parada limpa.")
				return false, 0
			case <-time.After(30 * time.Second):
				log.Println("[FocusGuard Daemon] Parada do serviço ignorada: existem bloqueios ativos.")
				changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
				continue
			}
		default:
			continue
		}
	}

	return false, 0
}

func tryRunAsService() bool {
	ok, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("[FocusGuard Daemon] Erro ao verificar modo serviço: %v", err)
		return false
	}
	if !ok {
		return false
	}

	log.Println("[FocusGuard Daemon] Executando como serviço Windows...")
	if err := svc.Run("FocusGuard", &focusGuardService{}); err != nil {
		log.Printf("[FocusGuard Daemon] svc.Run retornou: %v", err)
		return false
	}
	return true
}
