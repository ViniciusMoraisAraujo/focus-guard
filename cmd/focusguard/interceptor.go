package main

import (
	"fmt"

	"focusguard/internal/transport/ipc"
)

// handleInterceptorCommand liga/desliga a Focus Interceptor Page (Fase 3):
// quando ativa, domínios bloqueados passam a mostrar a página de bloqueio
// (frase motivacional + tempo restante) no lugar do endereço morto padrão.
// Exige a porta 80 livre — bind falho não derruba o bloqueio, só a página.
func handleInterceptorCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "on":
			handleInterceptorSetCommand(client, true)
			return
		case "off":
			handleInterceptorSetCommand(client, false)
			return
		}
	}
	handleInterceptorStatusCommand(client)
}

func handleInterceptorSetCommand(client *ipc.Client, enabled bool) {
	resp, err := client.Send(ipc.Request{Action: "interceptor-set", InterceptorEnabled: enabled})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao alterar a página de bloqueio: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleInterceptorStatusCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "interceptor-status"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o status da página de bloqueio: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Println("Página de bloqueio:")
	if resp.InterceptorEnabled {
		fmt.Println("  Estado: Ativa — domínios bloqueados mostram o aviso (requer porta 80 livre)")
	} else {
		fmt.Println("  Estado: Desativada — domínios bloqueados resolvem para endereço morto")
	}
}
