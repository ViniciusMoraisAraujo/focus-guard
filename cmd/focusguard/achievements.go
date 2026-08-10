package main

import (
	"fmt"

	"focusguard/internal/transport/ipc"
)

// handleAchievementsCommand lista as conquistas (Fase 5.2): badges com
// progresso/unlocked derivados das stats atuais — `focusguard achievements`.
func handleAchievementsCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "achievements-get"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter as conquistas: %s\n", resp.Message)
		osExit(1)
	}
	if len(resp.Achievements) == 0 {
		fmt.Println("Nenhuma conquista disponível.")
		return
	}
	unlocked := 0
	for _, a := range resp.Achievements {
		if a.Unlocked {
			unlocked++
		}
	}
	fmt.Printf("Conquistas (%d/%d desbloqueadas):\n", unlocked, len(resp.Achievements))
	for _, a := range resp.Achievements {
		icon := "🔒"
		status := "bloqueada"
		if a.Unlocked {
			icon = a.Icon
			if icon == "" {
				icon = "✅"
			}
			status = "desbloqueada"
		}
		fmt.Printf("  %s %-22s [%d%%] %s — %s\n", icon, a.Name, a.Progress, status, a.Description)
	}
}
