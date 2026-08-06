package main

import (
	"fmt"

	"focusguard/internal/ipc"
)

// handleAppsCommand manages the process denylist used by the process guard
// (which apps are terminated while a focus session is active).
// Usage: focusguard apps [add <proc> | remove <proc> | list]
func handleAppsCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			handleAppsAddCommand(client, args[1:])
			return
		case "remove", "rm":
			handleAppsRemoveCommand(client, args[1:])
			return
		}
	}
	handleAppsListCommand(client)
}

func handleAppsAddCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o nome do processo.")
		fmt.Println("Uso: focusguard apps add <processo> (ex: spotify.exe)")
		osExit(1)
	}
	resp, err := client.Send(ipc.Request{Action: "apps-add", AppName: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao adicionar processo: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleAppsRemoveCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o nome do processo.")
		fmt.Println("Uso: focusguard apps remove <processo>")
		osExit(1)
	}
	resp, err := client.Send(ipc.Request{Action: "apps-remove", AppName: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao remover processo: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleAppsListCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "apps-list"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao listar processos: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("Processos encerrados durante sessões de foco:")
	if len(resp.Apps) == 0 {
		fmt.Println("(nenhum — o guard está inativo)")
		return
	}
	for _, a := range resp.Apps {
		fmt.Printf("  • %s\n", a)
	}
}
