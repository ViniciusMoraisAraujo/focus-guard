package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"focusguard/internal/ipc"
	"focusguard/internal/tui"
)

var osExit = os.Exit
var runTUI = tui.Run

func main() {
	if len(os.Args) < 2 {
		runInteractive()
		return
	}

	client := ipc.NewClient()
	command := os.Args[1]

	switch command {
	case "block":
		handleBlockCommand(client, os.Args[2:])
	case "status":
		handleStatusCommand(client)
	case "interactive", "i":
		runInteractive()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Comando desconhecido: %s\n\n", command)
		printUsage()
		osExit(1)
	}
}

func runInteractive() {
	client := ipc.NewClient()
	if err := runTUI(client); err != nil {
		fmt.Fprintf(os.Stderr, "Erro no modo interativo: %v\n", err)
		osExit(1)
	}
}

func handleBlockCommand(client *ipc.Client, args []string) {
	blockCmd := flag.NewFlagSet("block", flag.ExitOnError)
	durationFlag := blockCmd.String("duration", "", "Duração do bloqueio (ex: 4h, 30m, 1h30m)")
	durationShortFlag := blockCmd.String("d", "", "Duração do bloqueio (shorthand)")

	_ = blockCmd.Parse(args)

	domain := blockCmd.Arg(0)
	if domain == "" {
		fmt.Println("Erro: É necessário informar o domínio a ser bloqueado.")
		fmt.Println("Uso: focusguard block <dominio> --duration <tempo>")
		osExit(1)
	}

	durationStr := *durationFlag
	if durationStr == "" {
		durationStr = *durationShortFlag
	}

	if durationStr == "" && blockCmd.NArg() > 1 {
		durationStr = blockCmd.Arg(1)
	}

	if durationStr == "" {
		fmt.Println("Erro: A duração do bloqueio deve ser informada (ex: --duration 4h).")
		osExit(1)
	}

	req := ipc.Request{
		Action:   "block",
		Domain:   domain,
		Duration: durationStr,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao aplicar bloqueio: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Printf("✔ %s\n", resp.Message)
}

func handleStatusCommand(client *ipc.Client) {
	req := ipc.Request{
		Action: "status",
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao obter status: %s\n", resp.Message)
		osExit(1)
	}

	if len(resp.Blocks) == 0 {
		fmt.Println("Nenhum bloqueio ativo no momento.")
		return
	}

	fmt.Println("\n🔒 Bloqueios Ativos:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DOMÍNIO\tINÍCIO\tEXPIRA EM\tTEMPO RESTANTE")
	fmt.Fprintln(w, "-------\t------\t---------\t--------------")

	for _, b := range resp.Blocks {
		remaining := time.Until(b.ExpiresAt).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			b.Domain,
			b.StartedAt.Local().Format("15:04 02/01"),
			b.ExpiresAt.Local().Format("15:04 02/01"),
			remaining.String(),
		)
	}
	w.Flush()
	fmt.Println()
}

func printUsage() {
	fmt.Println("FocusGuard - CLI para bloqueio focado")
	fmt.Println("\nUso:")
	fmt.Println("  focusguard                        Modo interativo (TUI)")
	fmt.Println("  focusguard block <dominio> --duration <tempo>")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard interactive             Modo interativo (TUI)")
	fmt.Println("\nExemplos:")
	fmt.Println("  focusguard block twitter.com --duration 4h")
	fmt.Println("  focusguard block youtube.com 30m")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard")
	fmt.Println("\nNota: Não existe comando de unblock manual por design.")
}
