package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"focusguard/internal/transport/ipc"
)

// handlePresetsCommand lists the available preset categories from the daemon.
func handlePresetsCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "presets"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao listar presets: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("Presets disponíveis (use --preset <nome>):")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NOME\tDESCRIÇÃO\tDOMÍNIOS")
	fmt.Fprintln(w, "----\t----------\t--------")
	for _, p := range resp.Presets {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Label, strings.Join(p.Domains, ", "))
	}
	w.Flush()
}

// handlePresetCommand dispatches the preset subcommands (add/remove).
func handlePresetCommand(client *ipc.Client, args []string) {
	if len(args) == 0 {
		fmt.Println("Erro: Informe um subcomando (add | remove).")
		fmt.Println("Uso: focusguard preset add <nome> <dominio...>  |  focusguard preset remove <nome>")
		osExit(1)
	}
	switch args[0] {
	case "add":
		handlePresetAddCommand(client, args[1:])
	case "remove", "rm":
		handlePresetRemoveCommand(client, args[1:])
	default:
		fmt.Printf("Subcomando desconhecido: %s\n", args[0])
		fmt.Println("Uso: focusguard preset add <nome> <dominio...>  |  focusguard preset remove <nome>")
		osExit(1)
	}
}

// handlePresetAddCommand creates a user-defined preset with the given name and
// domains (merged with the built-ins for block --preset / pomodoro).
func handlePresetAddCommand(client *ipc.Client, args []string) {
	if len(args) < 2 {
		fmt.Println("Erro: Informe o nome e ao menos um domínio.")
		fmt.Println("Uso: focusguard preset add <nome> <dominio...>")
		osExit(1)
	}

	req := ipc.Request{
		Action:        "preset-add",
		PresetName:    args[0],
		PresetLabel:   args[0],
		PresetDomains: args[1:],
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao criar preset: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// handlePresetRemoveCommand removes a user-defined preset.
func handlePresetRemoveCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o nome do preset.")
		fmt.Println("Uso: focusguard preset remove <nome>")
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "preset-remove", PresetName: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao remover preset: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}
