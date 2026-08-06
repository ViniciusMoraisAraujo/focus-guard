package main

import (
	"fmt"
	"time"

	"focusguard/internal/ipc"
)

// handleGoalCommand shows or sets the daily focus goal.
// Usage: focusguard goal [set <duração>]
func handleGoalCommand(client *ipc.Client, args []string) {
	if len(args) > 0 && args[0] == "set" {
		handleGoalSetCommand(client, args[1:])
		return
	}

	resp, err := client.Send(ipc.Request{Action: "goal-get"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao consultar a meta: %s\n", resp.Message)
		osExit(1)
	}

	if resp.Goal <= 0 {
		fmt.Println("Nenhuma meta diária definida.")
		fmt.Println("Defina uma com: focusguard goal set <duração> (ex: focusguard goal set 4h)")
		return
	}
	fmt.Printf("🎯 Meta diária de foco: %s\n", resp.Goal.Round(time.Minute))
}

// handleGoalSetCommand sets the daily focus goal from a duration string
// (e.g. "4h", "90m").
func handleGoalSetCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe a duração (ex: focusguard goal set 4h).")
		osExit(1)
	}
	d, err := time.ParseDuration(args[0])
	if err != nil || d <= 0 {
		fmt.Println("Erro: Duração inválida (use ex: 4h, 90m, 1h30m).")
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "goal-set", GoalMinutes: int(d.Minutes())})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao definir meta: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
