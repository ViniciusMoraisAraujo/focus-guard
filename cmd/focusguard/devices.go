package main

import (
	"fmt"
	"strings"

	"focusguard/internal/domain/devices"
	"focusguard/internal/transport/ipc"
)

// handleDevicesCommand gerencia as políticas por dispositivo (Fase 4 — edição
// Server): `focusguard devices`, `focusguard devices set <ip> --policy X`,
// `focusguard devices remove <ip>`.
func handleDevicesCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "set":
			handleDevicesSetCommand(client, args[1:])
			return
		case "remove":
			handleDevicesRemoveCommand(client, args[1:])
			return
		}
	}
	handleDevicesListCommand(client)
}

// handleDevicesListCommand lista o catálogo de dispositivos.
func handleDevicesListCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "devices-list"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao listar dispositivos: %s\n", resp.Message)
		osExit(1)
	}
	if len(resp.Devices) == 0 {
		fmt.Println("Nenhum dispositivo configurado — a política global vale para toda a rede.")
		return
	}
	fmt.Println("Dispositivos:")
	for _, d := range resp.Devices {
		label := d.Name
		if label == "" {
			label = "(sem nome)"
		}
		switch d.Policy {
		case devices.PolicyBlockAll:
			fmt.Printf("  %-16s %-20s bloquear tudo\n", d.IP, label)
		case devices.PolicyAllowList:
			fmt.Printf("  %-16s %-20s permitir: %s\n", d.IP, label, strings.Join(d.AllowedDomains, ", "))
		default:
			fmt.Printf("  %-16s %-20s herdar regra global\n", d.IP, label)
		}
	}
}

// handleDevicesSetCommand cria/atualiza a política de um dispositivo:
// `focusguard devices set <ip> --policy block_all|allow_list|inherit [--name X] [--allow d1,d2]`.
func handleDevicesSetCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o IP do dispositivo.")
		fmt.Println("Uso: focusguard devices set <ip> --policy <block_all|allow_list|inherit> [--name <nome>] [--allow <dom1,dom2>]")
		osExit(1)
	}
	ip := args[0]
	rest := args[1:]
	policy := ""
	name := ""
	var allow []string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--policy":
			if i+1 < len(rest) {
				i++
				policy = rest[i]
			}
		case "--name":
			if i+1 < len(rest) {
				i++
				name = rest[i]
			}
		case "--allow":
			if i+1 < len(rest) {
				i++
				allow = strings.Split(rest[i], ",")
			}
		}
	}
	if policy == "" {
		policy = string(devices.PolicyInherit)
	}
	if policy == "allow" {
		policy = string(devices.PolicyAllowList)
	}
	switch devices.Policy(policy) {
	case devices.PolicyBlockAll, devices.PolicyAllowList, devices.PolicyInherit:
	default:
		fmt.Printf("Política inválida %q (use block_all, allow_list ou inherit).\n", policy)
		osExit(1)
	}
	d := devices.Device{IP: ip, Name: name, Policy: devices.Policy(policy), AllowedDomains: allow}
	resp, err := client.Send(ipc.Request{Action: "devices-upsert", Device: &d})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao definir a política: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// handleDevicesRemoveCommand remove a política de um dispositivo.
func handleDevicesRemoveCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o IP do dispositivo a remover.")
		fmt.Println("Uso: focusguard devices remove <ip>")
		osExit(1)
	}
	resp, err := client.Send(ipc.Request{Action: "devices-remove", DeviceIP: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao remover o dispositivo: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
