package main

import (
	"fmt"

	"focusguard/internal/transport/ipc"
)

func handleDNSCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "start":
			handleDNSStartCommand(client)
			return
		case "stop":
			handleDNSStopCommand(client)
			return
		case "upstream":
			handleDNSUpstreamCommand(client, args[1:])
			return
		}
	}
	handleDNSStatusCommand(client)
}

// handleDNSUpstreamCommand changes the upstream resolver the sinkhole forwards
// allowed queries to: focusguard dns upstream <host[:porta]>.
func handleDNSUpstreamCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o upstream (ex: focusguard dns upstream 9.9.9.9).")
		fmt.Println("Uso: focusguard dns upstream <host[:porta]>  (ex: 1.1.1.2, 9.9.9.9:53, dns.google)")
		osExit(1)
	}
	resp, err := client.Send(ipc.Request{Action: "dns-set-upstream", Upstream: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao alterar o upstream: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleDNSStartCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "dns-start"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao iniciar o servidor DNS: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleDNSStopCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "dns-stop"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao desligar o servidor DNS: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

func handleDNSStatusCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "dns-status"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o status do servidor DNS: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("Servidor DNS:")
	if !resp.DNSEnabled {
		fmt.Println("  Estado: Desativado")
		return
	}
	if resp.DNSListening {
		fmt.Printf("  Estado: Ativo (ouvindo em %s)\n", resp.DNSAddr)
	} else {
		fmt.Println("  Estado: Habilitado, mas parado")
		if resp.DNSBindError != "" {
			fmt.Printf("  Erro:   %s\n", resp.DNSBindError)
		}
	}
	fmt.Printf("  Upstream: %s\n", resp.DNSUpstream)
	fmt.Printf("  Consultas: %d\n", resp.DNSQueries)
	fmt.Printf("  Bloqueios: %d\n", resp.DNSBlocked)
}
