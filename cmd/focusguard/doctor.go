package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"focusguard/internal/infrastructure/store"
	"focusguard/internal/transport/ipc"
)

// handleDoctorCommand roda o diagnóstico de instalação (Fase 1 do
// features-plan): uma cadeia de checagens que reporta pass/warn/fail com
// sugestão de correção e um exit code para scripts (0 = ok, 1 = problemas,
// 2 = erro de execução). --json emite a saída estruturada para automação.
func handleDoctorCommand(client *ipc.Client, args []string) {
	cmd := flag.NewFlagSet("doctor", flag.ExitOnError)
	jsonOut := cmd.Bool("json", false, "Saída estruturada JSON")
	_ = cmd.Parse(args)

	results := runDoctor(doctorEnv{
		client:    client,
		statePath: stateFilePath(),
		hostsPath: hostsFilePath(),
		exec:      execOutput,
		isAdmin:   isElevated,
		version:   daemonVersionFromStatus(client),
		exeDir:    executableDir(),
	})

	exit := doctorExitCode(results)
	if *jsonOut {
		printDoctorJSON(results, exit)
	} else {
		printDoctorText(results)
	}
	osExit(exit)
}

// ---------------------------------------------------------------------------
// Tipos do diagnóstico
// ---------------------------------------------------------------------------

type doctorStatus string

const (
	statusPass doctorStatus = "pass"
	statusWarn doctorStatus = "warn"
	statusFail doctorStatus = "fail"
)

// doctorResult é o resultado de uma checagem: status, mensagem legível e uma
// sugestão de correção (quando o status não é pass).
type doctorResult struct {
	Name    string
	Status  doctorStatus
	Message string
	Fix     string
}

// doctorClient é a superfície IPC que o doctor usa — *ipc.Client a satisfaz;
// os testes injetam um fake.
type doctorClient interface {
	Send(ipc.Request) (*ipc.Response, error)
}

// execOutput é o executor de comandos do doctor (sc query / systemctl) —
// stubbable nos testes.
var execOutput = func(name string, args ...string) ([]byte, error) {
	out, err := execCommand(name, args...).CombinedOutput()
	return out, err
}

// doctorEnv carrega as dependências do diagnóstico — tudo injetável para o
// teste exercitar cada checagem sem ambiente real.
type doctorEnv struct {
	client    doctorClient
	statePath string
	hostsPath string
	exec      func(name string, args ...string) ([]byte, error)
	isAdmin   func() bool
	version   string
	// exeDir é o diretório onde o doctor procura os binários irmãos (default:
	// o dir do executável atual). Injetável para os testes simularem a suíte.
	exeDir string
}

// ---------------------------------------------------------------------------
// Cadeia de checagens
// ---------------------------------------------------------------------------

// runDoctor executa todas as checagens em ordem. Cada checagem é isolada: uma
// falha nunca aborta as demais (degrau para warn quando não dá para
// verificar, nunca derruba o comando inteiro).
func runDoctor(env doctorEnv) []doctorResult {
	return []doctorResult{
		checkElevation(env),
		checkServices(env),
		checkIPC(env),
		checkState(env),
		checkHosts(env),
		checkFirewall(env),
		checkVersions(env),
		checkDNS(env),
	}
}

// doctorExitCode converte os resultados em exit code: qualquer fail → 1,
// senão qualquer warn → 1 (problemas), tudo pass → 0. O 2 (erro de execução)
// é reservado pelo chamador quando nem dá para rodar as checagens.
func doctorExitCode(results []doctorResult) int {
	for _, r := range results {
		if r.Status == statusFail {
			return 1
		}
	}
	for _, r := range results {
		if r.Status == statusWarn {
			return 1
		}
	}
	return 0
}

// checkElevation reporta se o shell tem privilégio de administrador/root —
// informação, não problema: o daemon é quem precisa de elevação, o CLI não.
func checkElevation(env doctorEnv) doctorResult {
	if env.isAdmin == nil {
		return doctorResult{Name: "Elevação", Status: statusWarn, Message: "não foi possível verificar", Fix: ""}
	}
	if env.isAdmin() {
		return doctorResult{Name: "Elevação", Status: statusPass, Message: "shell elevado (administrador/root)"}
	}
	return doctorResult{
		Name: "Elevação", Status: statusPass,
		Message: "shell NÃO elevado (normal para o CLI)",
		Fix:     "O daemon é quem precisa de elevação — rode 'focusguard install' como administrador se o serviço não estiver instalado.",
	}
}

// checkServices verifica se o serviço do daemon (e do watchdog no Windows)
// está instalado e rodando. Best-effort: sem o binário de consulta (sc /
// systemctl), vira warn, nunca falha o doctor inteiro.
func checkServices(env doctorEnv) doctorResult {
	names := serviceNames()
	var missing, stopped []string
	for _, n := range names {
		st, err := queryService(env.exec, n)
		if err != nil {
			return doctorResult{
				Name: "Serviços", Status: statusWarn,
				Message: fmt.Sprintf("não foi possível consultar %s: %v", n, err),
				Fix:     "Confirme que o daemon está instalado (focusguard install).",
			}
		}
		switch st {
		case serviceRunning:
			// ok
		case serviceInstalled:
			stopped = append(stopped, n)
		default:
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return doctorResult{
			Name: "Serviços", Status: statusFail,
			Message: fmt.Sprintf("serviço não instalado: %s", strings.Join(missing, ", ")),
			Fix:     "Rode 'focusguard install' (Linux: systemctl enable --now focusguard).",
		}
	}
	if len(stopped) > 0 {
		return doctorResult{
			Name: "Serviços", Status: statusFail,
			Message: fmt.Sprintf("serviço instalado, mas parado: %s", strings.Join(stopped, ", ")),
			Fix:     "Inicie o serviço: systemctl start focusguard (Linux) ou services.msc (Windows).",
		}
	}
	return doctorResult{Name: "Serviços", Status: statusPass, Message: strings.Join(names, ", ") + " instalados e rodando"}
}

// checkIPC verifica a conectividade com o daemon via ping.
func checkIPC(env doctorEnv) doctorResult {
	if env.client == nil {
		return doctorResult{Name: "IPC", Status: statusFail, Message: "cliente IPC ausente", Fix: ""}
	}
	resp, err := env.client.Send(ipc.Request{Action: "ping"})
	if err != nil {
		return doctorResult{
			Name: "IPC", Status: statusFail,
			Message: "daemon não respondeu ao ping: " + err.Error(),
			Fix:     "Verifique se o serviço FocusGuard está rodando (status acima).",
		}
	}
	if !resp.Success {
		return doctorResult{Name: "IPC", Status: statusFail, Message: "ping falhou: " + resp.Message, Fix: ""}
	}
	return doctorResult{Name: "IPC", Status: statusPass, Message: "daemon acessível (socket IPC)"}
}

// checkState verifica se o state.json existe, é JSON válido e é gravável.
func checkState(env doctorEnv) doctorResult {
	data, err := os.ReadFile(env.statePath)
	if err != nil {
		return doctorResult{
			Name: "Estado", Status: statusFail,
			Message: "state.json não encontrado em " + env.statePath + ": " + err.Error(),
			Fix:     "Inicie o daemon uma vez para ele criar o estado (ou rode focusguard install).",
		}
	}
	var st store.State
	if err := json.Unmarshal(data, &st); err != nil {
		return doctorResult{
			Name: "Estado", Status: statusFail,
			Message: "state.json corrompido: " + err.Error(),
			Fix:     "O daemon auto-recupera (réplica oculta) — reinicie o serviço; o arquivo foi ou será curado.",
		}
	}
	f, err := os.OpenFile(env.statePath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		// Shell não elevado (CLI comum) não consegue abrir para escrita mesmo
		// com permissões corretas — o daemon elevado é quem grava. Degrada para
		// warn (não falha) nesse caso, evitando o falso positivo de "state.json
		// não é gravável" quando a instalação está saudável.
		if env.isAdmin != nil && !env.isAdmin() && os.IsPermission(err) {
			return doctorResult{
				Name: "Estado", Status: statusWarn,
				Message: "não foi possível confirmar a gravabilidade (shell não elevado): " + err.Error(),
				Fix:     "Rode o doctor como administrador para confirmar a gravação, ou ignore se o daemon grava normalmente.",
			}
		}
		return doctorResult{
			Name: "Estado", Status: statusFail,
			Message: "state.json não é gravável: " + err.Error(),
			Fix:     "Ajuste as permissões do arquivo (o daemon precisa de escrita).",
		}
	}
	_ = f.Close()
	return doctorResult{Name: "Estado", Status: statusPass, Message: "state.json válido e gravável"}
}

// checkHosts cruza as entradas FOCUSGUARD do arquivo hosts com os bloqueios
// ativos do daemon (status): entradas órfãs (sem bloco ativo) ou bloqueios
// ativos sem entrada no hosts são problema — sinal de limpeza incompleta ou
// tamper externo.
func checkHosts(env doctorEnv) doctorResult {
	if env.client == nil {
		return doctorResult{Name: "Hosts", Status: statusWarn, Message: "cliente IPC ausente", Fix: ""}
	}
	resp, err := env.client.Send(ipc.Request{Action: "status"})
	if err != nil {
		return doctorResult{Name: "Hosts", Status: statusWarn, Message: "não foi possível obter o status: " + err.Error(), Fix: ""}
	}

	data, err := os.ReadFile(env.hostsPath)
	if err != nil {
		return doctorResult{
			Name: "Hosts", Status: statusWarn,
			Message: "não foi possível ler o hosts (" + env.hostsPath + "): " + err.Error(),
			Fix:     "O doctor não pode verificar a integridade do hosts sem ler o arquivo.",
		}
	}
	entries := fgHostsEntries(string(data))

	expected := make(map[string]bool)
	for _, b := range resp.Blocks {
		if b.Domain == "*all-internet*" {
			continue
		}
		if time.Now().After(b.ExpiresAt) {
			continue
		}
		expected[b.Domain] = true
		expected["www."+b.Domain] = true
	}

	var orphans, missing []string
	for d := range entries {
		if !expected[d] {
			orphans = append(orphans, d)
		}
	}
	for d := range expected {
		if !entries[d] {
			missing = append(missing, d)
		}
	}

	if len(orphans) > 0 {
		return doctorResult{
			Name: "Hosts", Status: statusFail,
			Message: "entradas FOCUSGUARD órfãs no hosts: " + strings.Join(orphans, ", "),
			Fix:     "O daemon deve limpá-las num Reconcile — reinicie o serviço; persistiram, investigue tamper (focusguard tamper-log).",
		}
	}
	if len(missing) > 0 {
		return doctorResult{
			Name: "Hosts", Status: statusFail,
			Message: "bloqueios ativos sem entrada no hosts: " + strings.Join(missing, ", "),
			Fix:     "O daemon deve re-aplicá-los — reinicie o serviço.",
		}
	}
	return doctorResult{Name: "Hosts", Status: statusPass, Message: "hosts consistente com os bloqueios ativos"}
}

// checkFirewall cruza a proteção esperada (há bloqueios?) com o estado real
// das regras DoH/firewall reportadas pelo daemon.
func checkFirewall(env doctorEnv) doctorResult {
	if env.client == nil {
		return doctorResult{Name: "Firewall", Status: statusWarn, Message: "cliente IPC ausente", Fix: ""}
	}
	resp, err := env.client.Send(ipc.Request{Action: "status"})
	if err != nil {
		return doctorResult{Name: "Firewall", Status: statusWarn, Message: "não foi possível obter o status: " + err.Error(), Fix: ""}
	}
	if resp.ProtectionError != "" {
		return doctorResult{
			Name: "Firewall", Status: statusWarn,
			Message: "não foi possível consultar o firewall: " + resp.ProtectionError,
			Fix:     "O daemon pode precisar de elevação para gerenciar as regras.",
		}
	}
	switch {
	case resp.ExpectedDoH && !resp.DoHActive:
		return doctorResult{
			Name: "Firewall", Status: statusFail,
			Message: "há bloqueios ativos, mas as regras DoH/DoT não estão aplicadas",
			Fix:     "Rode um Reconcile (toque no estado) ou reinicie o serviço para o daemon re-aplicar.",
		}
	case !resp.ExpectedDoH && resp.DoHActive:
		return doctorResult{
			Name: "Firewall", Status: statusWarn,
			Message: "regras DoH/DoT presentes sem bloqueios ativos (restos a limpar)",
			Fix:     "O próximo Reconcile sem bloqueios deve removê-las — se persistirem, reinicie o serviço.",
		}
	default:
		return doctorResult{
			Name: "Firewall", Status: statusPass,
			Message: fmt.Sprintf("regras de firewall consistentes (%d regras, DoH %s)", resp.FirewallRules, boolStr(resp.DoHActive)),
		}
	}
}

// checkVersions reporta a versão do daemon (do status) e avisa sobre suíte
// mista (binários irmãos faltando ao lado do CLI).
func checkVersions(env doctorEnv) doctorResult {
	dir := env.exeDir
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return doctorResult{Name: "Versões", Status: statusWarn, Message: "não foi possível localizar o executável", Fix: ""}
		}
		dir = filepath.Dir(exe)
	}
	ext := filepath.Ext(os.Args[0])
	var missing []string
	for _, n := range []string{"focusguard-daemon", "focusguard-tray", "focusguard-watchdog", "focusguard-web"} {
		if _, err := os.Stat(filepath.Join(dir, n+ext)); err != nil {
			missing = append(missing, n+ext)
		}
	}
	msg := "daemon " + env.version
	if len(missing) > 0 {
		return doctorResult{
			Name: "Versões", Status: statusWarn,
			Message: "suíte possivelmente mista — faltam binários irmãos: " + strings.Join(missing, ", ") + "; " + msg,
			Fix:     "Instale a mesma versão de todos os componentes (focusguard install).",
		}
	}
	return doctorResult{Name: "Versões", Status: statusPass, Message: msg + " e binários irmãos presentes"}
}

// checkDNS verifica o estado do sinkhole: habilitado mas parado (bind error)
// é problema; desligado é ok (configuração).
func checkDNS(env doctorEnv) doctorResult {
	if env.client == nil {
		return doctorResult{Name: "DNS", Status: statusWarn, Message: "cliente IPC ausente", Fix: ""}
	}
	resp, err := env.client.Send(ipc.Request{Action: "dns-status"})
	if err != nil {
		return doctorResult{Name: "DNS", Status: statusWarn, Message: "não foi possível obter o status do DNS: " + err.Error(), Fix: ""}
	}
	if !resp.Success {
		return doctorResult{Name: "DNS", Status: statusWarn, Message: resp.Message, Fix: ""}
	}
	switch {
	case !resp.DNSEnabled:
		return doctorResult{Name: "DNS", Status: statusPass, Message: "sinkhole desativado (configuração)"}
	case resp.DNSListening:
		return doctorResult{
			Name: "DNS", Status: statusPass,
			Message: fmt.Sprintf("sinkhole ativo em %s (upstream %s)", resp.DNSAddr, resp.DNSUpstream),
		}
	default:
		return doctorResult{
			Name: "DNS", Status: statusFail,
			Message: "sinkhole habilitado, mas não está ouvindo: " + resp.DNSBindError,
			Fix:     "Porta 53 em uso? Desative o ICS (Windows) ou libere a porta e reinicie o daemon.",
		}
	}
}

// ---------------------------------------------------------------------------
// Saída
// ---------------------------------------------------------------------------

// printDoctorText renderiza os resultados como linhas [ PASS ]/[ WARN ]/[ FAIL ]
// com a sugestão de correção embaixo dos não-pass.
func printDoctorText(results []doctorResult) {
	fmt.Println("🩺 FocusGuard — Diagnóstico de instalação")
	fmt.Println("──────────────────────────────────────────")
	for _, r := range results {
		switch r.Status {
		case statusPass:
			fmt.Printf("  [ PASS ] %s: %s\n", r.Name, r.Message)
		case statusWarn:
			fmt.Printf("  [ WARN ] %s: %s\n", r.Name, r.Message)
		default:
			fmt.Printf("  [ FAIL ] %s: %s\n", r.Name, r.Message)
		}
		if r.Status != statusPass && r.Fix != "" {
			fmt.Printf("           → Correção: %s\n", r.Fix)
		}
	}
	if code := doctorExitCode(results); code == 0 {
		fmt.Println("\n✅ Instalação saudável.")
	} else {
		fmt.Println("\n⚠ Há problemas — corrija acima (exit code 1).")
	}
}

// doctorJSON é o formato estruturado do --json: lista de checagens + status
// geral (exit code).
type doctorJSON struct {
	Checks  []doctorCheckJSON `json:"checks"`
	Overall int               `json:"overall_status"`
}

type doctorCheckJSON struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

func printDoctorJSON(results []doctorResult, exit int) {
	out := doctorJSON{Overall: exit, Checks: make([]doctorCheckJSON, 0, len(results))}
	for _, r := range results {
		out.Checks = append(out.Checks, doctorCheckJSON{
			Name: r.Name, Status: string(r.Status), Message: r.Message, Fix: r.Fix,
		})
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fgHostsEntries extrai os domínios das entradas marcadas FOCUSGUARD do
// arquivo hosts (formato: "<ip> <dominio> # FOCUSGUARD: <dominio>").
func fgHostsEntries(content string) map[string]bool {
	out := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, "# FOCUSGUARD:") {
			continue
		}
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) < 2 {
			continue
		}
		out[strings.ToLower(fields[1])] = true
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "ativa"
	}
	return "inativa"
}

// executableDir retorna o diretório do executável atual (para a checagem de
// binários irmãos). Best-effort: vazio quando não dá para localizar.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// daemonVersionFromStatus consulta a versão do daemon via status (usada pela
// checagem de versões). Best-effort: vazio quando o daemon está fora.
func daemonVersionFromStatus(client doctorClient) string {
	if client == nil {
		return ""
	}
	resp, err := client.Send(ipc.Request{Action: "status"})
	if err != nil || !resp.Success {
		return ""
	}
	return resp.CurrentVersion
}

// serviceNames retorna os serviços a verificar por plataforma.
func serviceNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"FocusGuard", "FocusGuardWatchdog"}
	}
	return []string{"focusguard"}
}
