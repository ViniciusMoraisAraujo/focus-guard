package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"focusguard/internal/analytics"
	"focusguard/internal/autostart"
	"focusguard/internal/httpapi"
	"focusguard/internal/ipc"
	"focusguard/internal/schedule"
	"focusguard/internal/tamper"
)

var osExit = os.Exit

func main() {
	if len(os.Args) < 2 {
		// Sem argumentos, o FocusGuard abre a interface web no navegador (a
		// antiga TUI interativa foi removida em favor da UI).
		handleWebCommand()
		return
	}

	client := ipc.NewClient()
	command := os.Args[1]

	switch command {
	case "block":
		handleBlockCommand(client, os.Args[2:])
	case "presets":
		handlePresetsCommand(client)
	case "preset":
		handlePresetCommand(client, os.Args[2:])
	case "schedule":
		handleScheduleCommand(client, os.Args[2:])
	case "apps":
		handleAppsCommand(client, os.Args[2:])
	case "dns":
		handleDNSCommand(client, os.Args[2:])
	case "pomodoro":
		handlePomodoroCommand(client, os.Args[2:])
	case "pomodoro-defaults":
		handlePomodoroDefaultsCommand(client)
	case "pomodoro-stop":
		handlePomodoroStopCommand(client)
	case "stats":
		handleStatsCommand(client, os.Args[2:])
	case "missions", "mission":
		handleMissionCommand(client)
	case "report":
		handleReportCommand(client)
	case "tamper-log":
		handleTamperLogCommand(client)
	case "goal":
		handleGoalCommand(client, os.Args[2:])
	case "status":
		handleStatusCommand(client)
	case "update":
		handleUpdateCommand(client, os.Args[2:])
	case "web":
		handleWebCommand()
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
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Comando desconhecido: %s\n\n", command)
		printUsage()
		osExit(1)
	}
}

// webURL é a URL da interface web servida pelo focusguard-web (sempre
// localhost). O CLI a usa para abrir o navegador e sondar o servidor.
var webURL = "http://" + httpapi.DefaultAddr

// webExePath resolve o foco do servidor web junto ao executável do CLI.
func webExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := "focusguard-web"
	if ext != "" {
		base += ext
	}
	return filepath.Join(dir, base)
}

// probeWebServerFn / spawnWebServerFn / waitWebServerFn / openBrowserFn /
// webExePathFn são injetáveis nos testes para não tocar rede, processos,
// navegador ou binários reais.
var (
	probeWebServerFn = webServerUp
	spawnWebServerFn = spawnWebServer
	waitWebServerFn  = waitForWebServer
	openBrowserFn    = openBrowser
	webExePathFn     = webExePath
)

// webServerUp sonda o health do focusguard-web: true quando o servidor já
// está de pé (não depende do daemon — o health responde sempre que o
// servidor roda).
func webServerUp() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(webURL + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForWebServer sonda o health em intervalos até o servidor responder ou
// o timeout expirar.
func waitForWebServer(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if webServerUp() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// openBrowser abre a URL no navegador padrão do sistema.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// handleWebCommand inicia o focusguard-web por demanda (se ainda não estiver
// no ar) e abre a interface no navegador padrão. Se o servidor já roda, só
// reabre o navegador — nunca sobe uma segunda instância.
func handleWebCommand() {
	if probeWebServerFn() {
		openBrowserFn(webURL)
		fmt.Printf("✔ Interface web já está no ar: %s\n", webURL)
		return
	}

	path := webExePathFn()
	if path == "" {
		fmt.Println("Erro: Não foi possível determinar o caminho do focusguard-web.")
		osExit(1)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("Erro: focusguard-web não encontrado em %s\n", path)
		fmt.Println("Compile o servidor web primeiro: go build ./cmd/focusguard-web")
		osExit(1)
	}
	if err := spawnWebServerFn(path); err != nil {
		fmt.Printf("Erro ao iniciar o focusguard-web: %v\n", err)
		osExit(1)
	}
	if !waitWebServerFn(5 * time.Second) {
		fmt.Println("⚠ Servidor web iniciado, mas ainda não respondeu. Abrindo o navegador mesmo assim...")
	}
	openBrowserFn(webURL)
	fmt.Printf("✔ Interface web: %s\n", webURL)
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
	if runtime.GOOS == "linux" {
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
		if err := autostart.InstallSvc(path); err != nil {
			fmt.Printf("Falha ao instalar: %v\n", err)
			osExit(1)
		}
		fmt.Printf("✔ Daemon instalado como serviço de inicialização.\n")
		fmt.Printf("  Binário: %s\n", path)
		fmt.Printf("  Iniciará automaticamente no próximo login.\n")
		return
	}

	// Windows: instala sempre em %ProgramFiles%\FocusGuard (Sistema / Todos os
	// Usuários) e cria um atalho na área de trabalho pública.
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("Erro: Não foi possível determinar o caminho do instalador: %v\n", err)
		osExit(1)
	}
	srcDir := filepath.Dir(exe)

	daemonSrc := filepath.Join(srcDir, "focusguard-daemon.exe")
	if _, err := os.Stat(daemonSrc); os.IsNotExist(err) {
		fmt.Printf("Erro: Daemon não encontrado em %s\n", daemonSrc)
		fmt.Println("Compile o daemon primeiro: go build ./cmd/focusguard-daemon")
		osExit(1)
	}

	installDir, err := autostart.InstallBinaries(srcDir)
	if err != nil {
		fmt.Printf("Falha ao copiar os binários para %s: %v\n", autostart.InstallDir(), err)
		fmt.Println("Se o daemon estiver em execução, rode 'focusguard uninstall' antes de reinstalar.")
		osExit(1)
	}

	daemonInstalled := filepath.Join(installDir, "focusguard-daemon.exe")
	if err := autostart.Install(daemonInstalled); err != nil {
		fmt.Printf("Falha ao instalar: %v\n", err)
		osExit(1)
	}

	cliInstalled := filepath.Join(installDir, "focusguard.exe")
	if err := autostart.CreateDesktopShortcut(cliInstalled); err != nil {
		fmt.Printf("⚠ Aviso: serviço instalado, mas não foi possível criar o atalho: %v\n", err)
	} else {
		fmt.Printf("✔ Atalho 'FocusGuard' criado na área de trabalho (Todos os Usuários).\n")
	}

	// Registra o tray na inicialização automaticamente, como faria o comando
	// install-tray (releases sempre incluem o tray; builds de dev podem não ter).
	trayInstalled := filepath.Join(installDir, "focusguard-tray.exe")
	if _, err := os.Stat(trayInstalled); err == nil {
		if err := autostart.InstallTray(trayInstalled); err != nil {
			fmt.Printf("⚠ Aviso: tray instalado, mas não foi possível registrar na inicialização: %v\n", err)
		} else {
			fmt.Printf("✔ Tray registrado para iniciar com o Windows (HKCU Run).\n")
		}
	} else if !os.IsNotExist(err) {
		fmt.Printf("⚠ Aviso: não foi possível verificar o tray: %v\n", err)
	}

	// Instala o watchdog externo (Windows) automaticamente, como faria o
	// comando install-watchdog (releases sempre incluem; builds de dev podem não).
	watchdogInstalled := filepath.Join(installDir, "focusguard-watchdog.exe")
	if _, err := os.Stat(watchdogInstalled); err == nil {
		if err := autostart.InstallWatchdog(watchdogInstalled); err != nil {
			fmt.Printf("⚠ Aviso: não foi possível instalar o watchdog: %v\n", err)
		} else {
			fmt.Printf("✔ Watchdog instalado como serviço e iniciado.\n")
		}
	} else if !os.IsNotExist(err) {
		fmt.Printf("⚠ Aviso: não foi possível verificar o watchdog: %v\n", err)
	}

	fmt.Printf("✔ Daemon instalado como serviço de inicialização.\n")
	fmt.Printf("  Instalação: %s\n", installDir)
	fmt.Printf("  Binário do daemon: %s\n", daemonInstalled)
	fmt.Printf("  Iniciará automaticamente no próximo login.\n")
}

func handleUninstallCommand() {
	if runtime.GOOS == "linux" {
		if err := autostart.UninstallSvc(); err != nil {
			fmt.Printf("Falha ao desinstalar: %v\n", err)
			osExit(1)
		}
		fmt.Println("✔ Daemon removido da inicialização automática.")
		return
	}

	if err := autostart.Uninstall(); err != nil {
		fmt.Printf("Falha ao desinstalar: %v\n", err)
		osExit(1)
	}
	// Remove o watchdog e o tray (instalados automaticamente pelo install)
	// antes de limpar o diretório, para não deixar serviços órfãos.
	if err := autostart.UninstallWatchdog(); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível remover o watchdog: %v\n", err)
	}
	if err := autostart.RemoveInstall(); err != nil {
		fmt.Printf("⚠ Aviso: serviço removido, mas não foi possível limpar a instalação: %v\n", err)
	}
	if err := autostart.UninstallTray(); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível remover o tray da inicialização: %v\n", err)
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

	installed, err := autostart.EnsureInInstallDir(path)
	if err != nil {
		fmt.Printf("Falha ao copiar o watchdog para a instalação: %v\n", err)
		osExit(1)
	}
	if err := autostart.InstallWatchdog(installed); err != nil {
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

	installed, err := autostart.EnsureInInstallDir(path)
	if err != nil {
		fmt.Printf("Falha ao copiar o tray para a instalação: %v\n", err)
		osExit(1)
	}
	if err := autostart.InstallTray(installed); err != nil {
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

// splitExtendReplaceArgs removes the --extend/--replace tokens from anywhere in
// args and returns the remaining tokens plus the extracted booleans. Go's flag
// package stops parsing at the first positional argument, so a user writing
// "focusguard block twitter.com --extend 30m" would never have the flag parsed
// (it would become Arg(1) and be mistaken for the duration); extracting it
// before Parse makes the flag position-independent.
func splitExtendReplaceArgs(args []string) ([]string, bool, bool) {
	var extend, replace bool
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--extend":
			extend = true
		case "--replace":
			replace = true
		default:
			out = append(out, a)
		}
	}
	return out, extend, replace
}

func handleBlockCommand(client *ipc.Client, args []string) {
	blockCmd := flag.NewFlagSet("block", flag.ExitOnError)
	durationFlag := blockCmd.String("duration", "", "Duração do bloqueio (ex: 4h, 30m, 1h30m)")
	durationShortFlag := blockCmd.String("d", "", "Duração do bloqueio (shorthand)")
	presetFlag := blockCmd.String("preset", "", "Bloquear uma categoria inteira (ex: social, video, news, games)")
	internetFlag := blockCmd.Bool("internet", false, "Bloquear toda a internet (modo pânico) por um período")
	allowFlag := blockCmd.String("allow", "", "No modo --internet: domínios permitidos (allowlist), separados por vírgula")
	extendFlag := blockCmd.Bool("extend", false, "Somar à duração do bloqueio já ativo do domínio (em vez de perguntar)")
	replaceFlag := blockCmd.Bool("replace", false, "Reiniciar o bloqueio do domínio a partir de agora, descartando o anterior")

	// Go's flag package para de parsear no primeiro argumento posicional —
	// "focusguard block <dominio> --extend 30m" deixaria --extend sem efeito.
	// Extrai esses flags de qualquer posição antes do Parse.
	args, argExtend, argReplace := splitExtendReplaceArgs(args)
	_ = blockCmd.Parse(args)
	extend := *extendFlag || argExtend
	replace := *replaceFlag || argReplace

	domain := blockCmd.Arg(0)
	if domain == "" && *presetFlag == "" && !*internetFlag {
		fmt.Println("Erro: Informe um domínio, --preset ou --internet para bloquear.")
		fmt.Println("Uso: focusguard block <dominio> --duration <tempo>  |  focusguard block --preset <categoria> --duration <tempo>  |  focusguard block --internet [--allow <dominios>] --duration <tempo>")
		osExit(1)
	}
	if (extend || replace) && (domain == "" || *presetFlag != "" || *internetFlag) {
		fmt.Println("Erro: --extend e --replace só se aplicam a um domínio específico.")
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
		Extend:   extend,
		Replace:  replace,
	}
	if *internetFlag {
		req.Action = "block-all"
		if *allowFlag != "" {
			for _, d := range strings.Split(*allowFlag, ",") {
				if d = strings.TrimSpace(d); d != "" {
					req.Allowlist = append(req.Allowlist, d)
				}
			}
		}
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}

	if resp.Conflict {
		fmt.Printf("Domínio já bloqueado até %s.\n", resp.ConflictBlock.ExpiresAt.Local().Format("15:04:05 02/01/2006"))
		fmt.Println("Use --extend para somar a duração atual ou --replace para reiniciar o bloqueio.")
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
// Flags não informadas enviam sentinelas (work=0, rest=-1, cycles=0) para o
// daemon resolver os padrões salvos (--save) ou o clássico 25/5/4. O rest é o
// único que distingue "não informado" (-1) de "sem descanso" (0 explícito).
func handlePomodoroCommand(client *ipc.Client, args []string) {
	pomCmd := flag.NewFlagSet("pomodoro", flag.ExitOnError)
	presetFlag := pomCmd.String("preset", "", "Categoria de domínios (ex: social, video)")
	workFlag := pomCmd.Int("work", 0, "Minutos de trabalho por ciclo (padrão: salvo ou 25)")
	restFlag := pomCmd.Int("rest", -1, "Minutos de descanso entre ciclos (0 = sem descanso; omitido = salvo ou 5)")
	cyclesFlag := pomCmd.Int("cycles", 0, "Número de ciclos (padrão: salvo ou 4)")
	strictFlag := pomCmd.Bool("strict", false, "Sessão estrita (não pode ser encerrada antecipadamente)")
	saveFlag := pomCmd.Bool("save", false, "Salvar estes parâmetros como padrão para as próximas sessões")
	labelFlag := pomCmd.String("label", "", "Nome da missão para o relatório (ex: --label \"Estudar ENEM\")")

	_ = pomCmd.Parse(args)

	if *presetFlag == "" {
		fmt.Println("Erro: Informe um preset (ex: --preset social).")
		fmt.Println("Uso: focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict] [--save] [--label \"missão\"]")
		osExit(1)
	}

	req := ipc.Request{
		Action:  "pomodoro",
		Preset:  *presetFlag,
		WorkMin: *workFlag,
		RestMin: *restFlag,
		Cycles:  *cyclesFlag,
		Strict:  *strictFlag,
		Save:    *saveFlag,
		Label:   *labelFlag,
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

// handlePomodoroDefaultsCommand shows the current persisted pomodoro defaults
// (or the classic 25/5/4 when none were saved).
func handlePomodoroDefaultsCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "pomodoro-defaults"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao consultar padrões: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("🍅 Padrões atuais do pomodoro: %dm trabalho / %dm descanso / %d ciclos\n",
		resp.PomodoroWork, resp.PomodoroRest, resp.PomodoroCycle)
	fmt.Println("  Salve novos padrões com: focusguard pomodoro --preset <categoria> --work X --rest Y --cycles Z --save")
}

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

// ---------------------------------------------------------------------------
// Agendamento recorrente (schedule add/list/remove)
// ---------------------------------------------------------------------------

// handleScheduleCommand dispatches the schedule subcommands
// (add/import/list/remove). With no subcommand it lists the current rules.
func handleScheduleCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			handleScheduleAddCommand(client, args[1:])
			return
		case "import":
			handleScheduleImportCommand(client, args[1:])
			return
		case "remove", "rm":
			handleScheduleRemoveCommand(client, args[1:])
			return
		}
	}
	handleScheduleListCommand(client)
}

// handleScheduleImportCommand imports weekly events from an .ics calendar file
// as recurring block rules: focusguard schedule import --file <arquivo.ics> --preset <categoria>
func handleScheduleImportCommand(client *ipc.Client, args []string) {
	impCmd := flag.NewFlagSet("schedule-import", flag.ExitOnError)
	presetFlag := impCmd.String("preset", "", "Categoria a bloquear (ex: social, video)")
	fileFlag := impCmd.String("file", "", "Caminho do arquivo .ics")
	_ = impCmd.Parse(args)

	path := *fileFlag
	if path == "" {
		fmt.Println("Erro: Informe o arquivo .ics (--file <arquivo.ics>).")
		fmt.Println("Uso: focusguard schedule import --file <arquivo.ics> --preset <categoria>")
		osExit(1)
	}
	if *presetFlag == "" {
		fmt.Println("Erro: Informe o preset (ex: --preset social).")
		fmt.Println("Uso: focusguard schedule import --file <arquivo.ics> --preset <categoria>")
		osExit(1)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Erro ao ler %s: %v\n", path, err)
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "schedule-import", ICSContent: string(data), ICSPreset: *presetFlag})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao importar calendário: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// scheduleDayNames maps English and Portuguese weekday abbreviations to
// time.Weekday values (0=Sunday), case-insensitively.
var scheduleDayNames = map[string]int{
	"sun": 0, "dom": 0,
	"mon": 1, "seg": 1,
	"tue": 2, "ter": 2,
	"wed": 3, "qua": 3,
	"thu": 4, "qui": 4,
	"fri": 5, "sex": 5,
	"sat": 6, "sab": 6,
}

// parseScheduleDays converts a comma-separated list of day abbreviations into
// time.Weekday ints (0=Sunday), rejecting unknown names.
func parseScheduleDays(raw string) ([]int, error) {
	var days []int
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(strings.ToLower(tok))
		d, ok := scheduleDayNames[tok]
		if !ok {
			return nil, fmt.Errorf("dia inválido %q (use mon..sun ou seg..dom)", tok)
		}
		days = append(days, d)
	}
	return days, nil
}

// handleScheduleAddCommand creates a recurring block rule for a preset.
// Example: focusguard schedule add --preset social --days mon-fri --start 08:00 --end 12:00
func handleScheduleAddCommand(client *ipc.Client, args []string) {
	addCmd := flag.NewFlagSet("schedule-add", flag.ExitOnError)
	presetFlag := addCmd.String("preset", "", "Categoria a bloquear (ex: social, video)")
	daysFlag := addCmd.String("days", "", "Dias da semana (ex: mon,tue,wed ou seg,ter,qua)")
	startFlag := addCmd.String("start", "", "Início no formato HH:MM (ex: 08:00)")
	endFlag := addCmd.String("end", "", "Fim no formato HH:MM (ex: 12:00)")
	windowsFlag := addCmd.String("windows", "", "Janelas múltiplas HH:MM-HH:MM separadas por vírgula (ex: 08:00-12:00,14:00-18:00)")
	labelFlag := addCmd.String("label", "", "Rótulo opcional (ex: Estudo matinal)")

	_ = addCmd.Parse(args)

	if *presetFlag == "" || *daysFlag == "" || (*windowsFlag == "" && (*startFlag == "" || *endFlag == "")) {
		fmt.Println("Erro: Informe --preset, --days e (--start/--end OU --windows).")
		fmt.Println("Uso: focusguard schedule add --preset <categoria> --days <dias> --start HH:MM --end HH:MM [--label \"...\"]")
		fmt.Println("     focusguard schedule add --preset <categoria> --days <dias> --windows 08:00-12:00,14:00-18:00 [--label \"...\"]")
		osExit(1)
	}

	days, err := parseScheduleDays(*daysFlag)
	if err != nil {
		fmt.Printf("Erro: %v\n", err)
		osExit(1)
	}

	rule := schedule.Rule{
		Preset:  *presetFlag,
		Label:   *labelFlag,
		Days:    days,
		Start:   *startFlag,
		End:     *endFlag,
		Enabled: true,
	}
	if *windowsFlag != "" {
		rule.Windows = strings.Split(*windowsFlag, ",")
	}

	req := ipc.Request{
		Action:       "schedule-add",
		ScheduleRule: rule,
	}

	resp, err := client.Send(req)
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao criar agendamento: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// handleScheduleListCommand prints the recurring rules.
func handleScheduleListCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "schedule-list"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao listar agendamentos: %s\n", resp.Message)
		osExit(1)
	}

	if len(resp.Schedules) == 0 {
		fmt.Println("Nenhum agendamento recorrente configurado.")
		fmt.Println("Crie um com: focusguard schedule add --preset <categoria> --days <dias> --start HH:MM --end HH:MM")
		return
	}

	fmt.Println("📅 Agendamentos recorrentes:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tPRESET\tDIAS\tJANELA")
	fmt.Fprintln(w, "--\t------\t----\t------")
	for _, r := range resp.Schedules {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s%s\n", r.ID, r.Preset, scheduleDaysString(r.Days), scheduleWindowString(r), scheduleStateSuffix(r))
	}
	w.Flush()
}

// scheduleWindowString renders the rule's time window(s): the Windows list
// when present, otherwise the legacy "HH:MM-HH:MM" from Start/End.
func scheduleWindowString(r schedule.Rule) string {
	if len(r.Windows) > 0 {
		return strings.Join(r.Windows, ", ")
	}
	return r.Start + "-" + r.End
}

// scheduleDaysString renders weekday ints as abbreviations (seg..dom).
func scheduleDaysString(days []int) string {
	names := []string{"dom", "seg", "ter", "qua", "qui", "sex", "sab"}
	var out []string
	for _, d := range days {
		if d >= 0 && d < 7 {
			out = append(out, names[d])
		}
	}
	return strings.Join(out, ",")
}

func scheduleStateSuffix(r schedule.Rule) string {
	if !r.Enabled {
		return " (desativada)"
	}
	return ""
}

// handleScheduleRemoveCommand deletes a recurring rule by ID.
func handleScheduleRemoveCommand(client *ipc.Client, args []string) {
	if len(args) < 1 {
		fmt.Println("Erro: Informe o ID da regra.")
		fmt.Println("Uso: focusguard schedule remove <id>")
		osExit(1)
	}

	resp, err := client.Send(ipc.Request{Action: "schedule-remove", ScheduleID: args[0]})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao remover agendamento: %s\n", resp.Message)
		osExit(1)
	}
	fmt.Printf("✔ %s\n", resp.Message)
}

// handleMissionCommand lists the focus totals per named mission (sessions
// started with pomodoro --label "...").
func handleMissionCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "missions"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter missões: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("🎯 Missões de foco:")
	if len(resp.LabelStats) == 0 {
		fmt.Println("Nenhuma missão nomeada ainda — inicie uma com: focusguard pomodoro --preset <cat> --label \"missão\"")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "MISSÃO\tFOCO\tSESSÕES")
	fmt.Fprintln(w, "------\t----\t-------")
	for _, ls := range resp.LabelStats {
		fmt.Fprintf(w, "%s\t%s\t%d\n", ls.Label, ls.Duration.Round(time.Minute), ls.Sessions)
	}
	w.Flush()
}

// handleStatsCommand fetches the focus analytics and renders the ASCII chart
// (default), or exports the report as CSV/JSON with --export.
func handleStatsCommand(client *ipc.Client, args []string) {
	statsCmd := flag.NewFlagSet("stats", flag.ExitOnError)
	exportFlag := statsCmd.String("export", "", "Exportar como csv ou json")
	missionFlag := statsCmd.String("mission", "", "Filtrar por uma missão (label da sessão)")
	_ = statsCmd.Parse(args)

	resp, err := client.Send(ipc.Request{Action: "stats", Mission: *missionFlag})
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

	switch strings.ToLower(*exportFlag) {
	case "csv":
		fmt.Print(analytics.ExportCSV(resp.Stats))
	case "json":
		if out, err := analytics.ExportJSON(resp.Stats); err != nil {
			fmt.Printf("Falha ao exportar JSON: %v\n", err)
			osExit(1)
		} else {
			fmt.Println(out)
		}
	case "html":
		fmt.Print(analytics.ExportHTML(resp.Stats))
	default:
		fmt.Print(analytics.RenderStats(resp.Stats, 30))
	}
}

// handleReportCommand prints a compact weekly focus summary derived from the
// stats report (same IPC action — no new daemon surface needed).
func handleReportCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "stats"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o resumo: %s\n", resp.Message)
		osExit(1)
	}
	if resp.Stats == nil {
		fmt.Println("Sem estatísticas registradas ainda — faça uma sessão de foco primeiro.")
		return
	}
	fmt.Print(analytics.RenderWeeklySummary(resp.Stats))
}

// handleTamperLogCommand prints the detected tamper attempts (external edits
// to the hosts/state files that were detected and restored).
func handleTamperLogCommand(client *ipc.Client) {
	resp, err := client.Send(ipc.Request{Action: "tamper-log"})
	if err != nil {
		fmt.Printf("Erro de comunicação: %v\n", err)
		osExit(1)
	}
	if !resp.Success {
		fmt.Printf("Falha ao obter o histórico: %s\n", resp.Message)
		osExit(1)
	}

	fmt.Println("🛡 Histórico de tentativas de burla (adulterações detectadas e revertidas):")
	if len(resp.TamperLog) == 0 {
		fmt.Println("Nenhuma tentativa registrada. 👌")
		return
	}
	for _, e := range resp.TamperLog {
		fmt.Printf("  %s\n", tamper.FormatEvent(e))
	}
}

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

func handleDNSCommand(client *ipc.Client, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "start":
			handleDNSStartCommand(client)
			return
		case "stop":
			handleDNSStopCommand(client)
			return
		}
	}
	handleDNSStatusCommand(client)
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

	// PendingReboot vem ANTES de UpdateAvailable: o server mantém Available=true
	// no fallback move-on-reboot (o update existe, só será aplicado no boot) —
	// se invertesse a ordem, diria "aplicada" indevidamente.
	if resp.UpdatePendingReboot {
		// Fallback move-on-reboot: um binário em execução estava travado e a
		// troca da suíte foi agendada para o próximo boot (MoveFileEx).
		fmt.Printf("✔ Atualização preparada: %s → %s\n", resp.CurrentVersion, resp.UpdateVersion)
		fmt.Println("  Os binários em uso serão substituídos no próximo reinício do computador.")
		return
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
	fmt.Println("  focusguard                        Abre a interface web no navegador")
	fmt.Println("  focusguard block <dominio> --duration <tempo>")
	fmt.Println("  focusguard block --preset <categoria> --duration <tempo>")
	fmt.Println("  focusguard block --internet [--allow <dominios>] --duration <tempo>   Modo pânico / allowlist")
	fmt.Println("  focusguard presets                     Listar categorias de bloqueio")
	fmt.Println("  focusguard preset add <nome> <dominio...>   Criar preset personalizado")
	fmt.Println("  focusguard preset remove <nome>         Remover preset personalizado")
	fmt.Println("  focusguard schedule add --preset <cat> --days <dias> --start HH:MM --end HH:MM")
	fmt.Println("  focusguard schedule import --file <arquivo.ics> --preset <cat>   Importar calendário (eventos semanais)")
	fmt.Println("  focusguard schedule list                Listar agendamentos recorrentes")
	fmt.Println("  focusguard schedule remove <id>         Remover um agendamento")
	fmt.Println("  focusguard apps [list]                  Listar processos da denylist")
	fmt.Println("  focusguard apps add <processo>          Encerrar processo durante sessões de foco")
	fmt.Println("  focusguard apps remove <processo>       Parar de encerrar um processo")
	fmt.Println("  focusguard dns start                    Iniciar o servidor DNS sinkhole (porta 53)")
	fmt.Println("  focusguard dns stop                     Desligar o servidor DNS sinkhole")
	fmt.Println("  focusguard dns status                   Mostrar o status do servidor DNS")
	fmt.Println("  focusguard pomodoro --preset <categoria> [--work 25] [--rest 5] [--cycles 4] [--strict] [--save] [--label \"missão\"]")
	fmt.Println("  focusguard pomodoro-defaults          Mostrar os padrões salvos do pomodoro")
	fmt.Println("  focusguard mission                    Resumo de foco por missão nomeada")
	fmt.Println("  focusguard pomodoro-stop               Encerrar a sessão pomodoro")
	fmt.Println("  focusguard stats [--export csv|json|html] [--mission <nome>]   Gráfico de foco / exportar relatório")
	fmt.Println("  focusguard report                     Resumo semanal de foco")
	fmt.Println("  focusguard tamper-log                 Histórico de tentativas de burla")
	fmt.Println("  focusguard goal                        Mostrar a meta diária de foco")
	fmt.Println("  focusguard goal set <duracao>         Definir a meta diária (ex: 4h)")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard web                     Abrir a interface web no navegador")
	fmt.Println("  focusguard update [--channel beta]  Verificar e aplicar atualizações do daemon")
	fmt.Println("  focusguard install                 Instalar daemon + tray + watchdog")
	fmt.Println("  focusguard uninstall               Remover daemon da inicialização")
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
	fmt.Println("  focusguard block --internet --duration 30m")
	fmt.Println("  focusguard block --internet --allow docs.google.com,drive.google.com --duration 2h")
	fmt.Println("  focusguard pomodoro --preset social --work 25 --rest 5 --cycles 4 --strict")
	fmt.Println("  focusguard schedule add --preset social --days seg,ter,qua,qui,sex --start 08:00 --end 12:00")
	fmt.Println("  focusguard schedule")
	fmt.Println("  focusguard stats")
	fmt.Println("  focusguard presets")
	fmt.Println("  focusguard status")
	fmt.Println("  focusguard")
	fmt.Println("\nNota: Não existe comando de unblock manual por design.")
}
