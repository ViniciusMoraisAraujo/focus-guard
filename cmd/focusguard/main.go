package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"focusguard/internal/analytics"
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
	case "presets":
		handlePresetsCommand(client)
	case "pomodoro":
		handlePomodoroCommand(client, os.Args[2:])
	case "pomodoro-stop":
		handlePomodoroStopCommand(client)
	case "stats":
		handleStatsCommand(client)
	case "status":
		handleStatusCommand(client)
	case "update":
		handleUpdateCommand(client, os.Args[2:])
	case "install":
		handleInstallCommand()
	case "uninstall":
		handleUninstallCommand()
	case "install-watchdog":
		handleInstallWatchdogCommand()
	case "uninstall-watchdog":
		handleUninstallWatchdogCommand()
	case "install-tray":
		handleInstallTrayCommand()
	case "uninstall-tray":
		handleUninstallTrayCommand()
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

func trayExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-tray"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

func handleInstallTrayCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Autostart do tray é exclusivo do Windows (chave Run HKCU).")
		return
	}

	path := trayExePath()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do tray.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: Tray não encontrado em %s\n", path)
		fmt.Println("Compile o tray primeiro: go build ./cmd/focusguard-tray")
		osExit(1)
	}

	if err := autostart.InstallTray(path); err != nil {
		fmt.Printf("Falha ao registrar tray: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Tray registrado para iniciar com o Windows (HKCU Run).")
}

func handleUninstallTrayCommand() {
	if runtime.GOOS != "windows" {
		fmt.Println("Autostart do tray é exclusivo do Windows.")
		return
	}

	if err := autostart.UninstallTray(); err != nil {
		fmt.Printf("Falha ao remover registro do tray: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ Tray removido da inicialização do Windows.")
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
	presetFlag := blockCmd.String("preset", "", "Bloquear uma categoria inteira (ex: social, video, news, games)")

	_ = blockCmd.Parse(args)

	domain := blockCmd.Arg(0)
	if domain == "" && *presetFlag == "" {
		fmt.Println("Erro: Informe um domínio ou --preset para bloquear.")
		fmt.Println("Uso: focusguard block <dominio> --duration <tempo>  |  focusguard block --preset <categoria> --duration <tempo>")
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
		Preset:   *presetFlag,
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

// handlePomodoroCommand starts a pomodoro session over a preset's domains.
// Defaults follow the classic pomodoro: 25m work / 5m rest / 4 cycles.
func handlePomodoroCommand(client *ipc.Client, args []string) {
	pomCmd := flag.NewFlagSet("pomodoro", flag.ExitOnError)
	presetFlag := pomCmd.String("preset", "", "Categoria de domínios (ex: social, video)")
	workFlag := pomCmd.Int("work", 25, "Minutos de trabalho por ciclo")
	restFlag := pomCmd.Int("rest", 5, "Minutos de descanso entre ciclos")
	cyclesFlag := pomCmd.Int("cycles", 4, "Número de ciclos")
	strictFlag := pomCmd.Bool("strict", false, "Sessão estrita (não pode ser encerrada antecipadamente)")

	_ = pomCmd.Parse(args)

	if *presetFlag == "" {
		fmt.Println("Erro: Informe um preset (ex: --preset social).")
		fmt.Println("Uso: focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict]")
		osExit(1)
	}

	req := ipc.Request{
		Action:  "pomodoro",
		Preset:  *presetFlag,
		WorkMin: *workFlag,
		RestMin: *restFlag,
		Cycles:  *cyclesFlag,
		Strict:  *strictFlag,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao iniciar pomodoro: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Printf("✔ %s\n", resp.Message)
}

// handleStatsCommand fetches the focus analytics and renders the ASCII chart.
func handleStatsCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "stats"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter estatísticas: %s\n", resp.Message)
		osExit(1)
	}

	if resp.Stats == nil {
		fmt.Println("Sem estatísticas registradas ainda — faça uma sessão de foco primeiro.")
		return
	}
	fmt.Print(analytics.RenderStats(resp.Stats, 30))
}

// handlePomodoroStopCommand ends the active pomodoro session.
func handlePomodoroStopCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "pomodoro-stop"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao encerrar pomodoro: %s\n", resp.Message)
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

func handleUpdateCommand(client *ipc.Client, args []string) {
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	channelFlag := updateCmd.String("channel", "", "Canal de release: stable (padrão) ou beta (prereleases)")
	_ = updateCmd.Parse(args)

	req := ipc.Request{
		Action:  "update",
		Channel: *channelFlag,
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
	fmt.Println("  focusguard block --preset <categoria> --duration <tempo>")
	fmt.Println("  focusguard presets                     Listar categorias de bloqueio")
	fmt.Println("  focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict]")
	fmt.Println("  focusguard pomodoro-stop               Encerrar a sessão pomodoro")
	fmt.Println("  focusguard stats                       Gráfico de foco em arte ASCII")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard update [--channel beta]  Verificar e aplicar atualizações do daemon")
	fmt.Println("  focusguard install                 Instalar daemon na inicialização")
	fmt.Println("  focusguard uninstall               Remover daemon da inicialização")
	fmt.Println("  focusguard interactive             Modo interativo (TUI)")
	fmt.Println("  focusguard install-watchdog         Instalar watchdog externo (Windows)")
	fmt.Println("  focusguard uninstall-watchdog       Remover watchdog externo")
	fmt.Println("  focusguard install-tray             Iniciar o tray com o Windows (HKCU Run)")
	fmt.Println("  focusguard uninstall-tray           Remover o tray da inicialização")
	fmt.Println("\nExemplos:")
	fmt.Println("  focusguard install")
	fmt.Println("  focusguard install-watchdog")
	fmt.Println("  focusguard block twitter.com --duration 4h")
	fmt.Println("  focusguard block youtube.com 30m")
	fmt.Println("  focusguard block --preset social --duration 2h")
	fmt.Println("  focusguard pomodoro --preset social --work 25 --rest 5 --cycles 4 --strict")
	fmt.Println("  focusguard stats")
	fmt.Println("  focusguard presets")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard")
	fmt.Println("\nNota: Não existe comando de unblock manual por design.")
}
