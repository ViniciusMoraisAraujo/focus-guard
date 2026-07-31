package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"text/tabwriter"
	"time"

	"focusguard/internal/autostart"
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
	case "update":
		handleUpdateCommand(client)
	case "install":
		handleInstallCommand()
	case "uninstall":
		handleUninstallCommand()
	case "install-watchdog":
		handleInstallWatchdogCommand()
	case "uninstall-watchdog":
		handleUninstallWatchdogCommand()
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

func daemonExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-daemon"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallCommand() {
	path := daemonExePath()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do daemon.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: Daemon não encontrado em %s\n", path)
		fmt.Println("Compile o daemon primeiro: go build ./cmd/focusguard-daemon")
		osExit(1)
	}
	var err error
	if runtime.GOOS == "linux" {
		err = autostart.InstallSvc(path)
	} else {
		err = autostart.Install(path)
	}
	if err != nil {
		fmt.Printf("Falha ao instalar: %v\n", err)
		osExit(1)
	}
	fmt.Printf("✔ Daemon instalado como serviço de inicialização.\n")
	fmt.Printf("  Binário: %s\n", path)
	fmt.Printf("  Iniciará automaticamente no próximo login.\n")
}

func handleUninstallCommand() {
	var err error
	if runtime.GOOS == "linux" {
		err = autostart.UninstallSvc()
	} else {
		err = autostart.Uninstall()
	}
	if err != nil {
		fmt.Printf("Falha ao desinstalar: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Daemon removido da inicialização automática.")
}

func watchdogExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-watchdog"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallWatchdogCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Watchdog externo é exclusivo do Windows (Linux usa systemd watchdog).")
		return
	}

	path := watchdogExePath()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do watchdog.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: Watchdog não encontrado em %s\n", path)
		fmt.Println("Compile o watchdog primeiro: go build ./cmd/focusguard-watchdog")
		osExit(1)
	}

	if err := autostart.InstallWatchdog(path); err != nil {
		fmt.Printf("Falha ao instalar watchdog: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Watchdog instalado como serviço e iniciado.")
}

func handleUninstallWatchdogCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Watchdog só está presente no Windows.")
		return
	}

	if err := autostart.UninstallWatchdog(); err != nil {
		fmt.Printf("Falha ao desinstalar watchdog: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Watchdog removido.")
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

	printProtectionStatus(resp)

	if resp.UpdateAvailable {
		fmt.Printf("🔄 Nova versão disponível: %s → %s (rode 'focusguard update')\n", resp.CurrentVersion, resp.UpdateVersion)
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

func handleUpdateCommand(client *ipc.Client) {
	req := ipc.Request{
		Action: "update",
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if !resp.Success {
		fmt.Printf("Falha ao atualizar: %s\n", resp.Message)
		osExit(1)
	}

	if resp.UpdateAvailable {
		fmt.Printf("✔ Atualização aplicada: %s → %s\n", resp.CurrentVersion, resp.UpdateVersion)
		fmt.Println("  A nova versão será usada na próxima reinicialização do daemon.")
		return
	}

	fmt.Printf("✔ Você já está na versão mais recente (%s).\n", resp.CurrentVersion)
}

func printProtectionStatus(resp *ipc.Response) {
	fmt.Println("🛡 Proteção DoH/DoT:")

	if resp.ProtectionError != "" {
		fmt.Printf("  ⚠ Não foi possível consultar o firewall: %s\n", resp.ProtectionError)
		return
	}

	status := "inativa"
	if resp.DoHActive {
		status = "ATIVA"
	}
	fmt.Printf("  Estado: %s\n", status)
	fmt.Printf("  Regras de firewall (FocusGuard): %d\n", resp.FirewallRules)

	switch {
	case resp.ExpectedDoH && !resp.DoHActive:
		fmt.Println("  ⚠ Atenção: há bloqueios ativos, mas as regras DoH/DoT não foram encontradas.")
	case !resp.ExpectedDoH && resp.DoHActive:
		fmt.Println("  ℹ As regras DoH/DoT estão presentes, mas não há bloqueios ativos.")
	}
}

func printUsage() {
	fmt.Println("FocusGuard - CLI para bloqueio focado")
	fmt.Println("\nUso:")
	fmt.Println("  focusguard                        Modo interativo (TUI)")
	fmt.Println("  focusguard block <dominio> --duration <tempo>")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard update                  Verificar e aplicar atualizações do daemon")
	fmt.Println("  focusguard install                 Instalar daemon na inicialização")
	fmt.Println("  focusguard uninstall               Remover daemon da inicialização")
	fmt.Println("  focusguard interactive             Modo interativo (TUI)")
	fmt.Println("  focusguard install-watchdog         Instalar watchdog externo (Windows)")
	fmt.Println("  focusguard uninstall-watchdog       Remover watchdog externo")
	fmt.Println("\nExemplos:")
	fmt.Println("  focusguard install")
	fmt.Println("  focusguard install-watchdog")
	fmt.Println("  focusguard block twitter.com --duration 4h")
	fmt.Println("  focusguard block youtube.com 30m")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard")
	fmt.Println("\nNota: Não existe comando de unblock manual por design.")
}
