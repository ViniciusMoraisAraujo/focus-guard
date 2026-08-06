package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"focusguard/internal/httpapi"
)

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
// webExePathFn / webRecheckFn / killStaleWebServerFn são injetáveis nos testes
// para não tocar rede, processos, navegador ou binários reais.
var (
	probeWebServerFn     = webServerUp
	spawnWebServerFn     = spawnWebServer
	waitWebServerFn      = waitForWebServer
	openBrowserFn        = openBrowser
	webExePathFn         = webExePath
	webRecheckFn         = func(timeout time.Duration) bool { return waitForWebServer(timeout) }
	killStaleWebServerFn = killStaleWebServer
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

// webRecheckTimeout é a janela de re-sonda antes de o CLI declarar o servidor
// morto e encerrar instâncias antigas: um servidor apenas lento (máquina
// ocupada) não deve ser morto à toa; depois desta janela, o CLI assume zumbi
// segurando a porta e limpa antes de subir uma instância nova.
const webRecheckTimeout = 3 * time.Second

// handleWebCommand inicia o focusguard-web por demanda (se ainda não estiver
// no ar) e abre a interface no navegador padrão. Se o servidor já roda, só
// reabre o navegador — nunca sobe uma segunda instância.
func handleWebCommand() {
	if probeWebServerFn() {
		openBrowserFn(webURL)
		fmt.Printf("✔ Interface web já está no ar: %s\n", webURL)
		return
	}

	// O health falhou, mas a porta pode estar retida por um focusguard-web
	// antigo/travado que não responde — cada spawn novo morreria com
	// EADDRINUSE em silêncio (o "loop" da edição Server). Re-sonda por alguns
	// segundos; se o servidor continuar fora do ar, encerra as instâncias
	// antigas antes de subir uma nova, para o bind nunca esbarrar num zumbi.
	if webRecheckFn(webRecheckTimeout) {
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

	// O servidor continuou fora do ar na re-sonda: pode haver uma instância
	// antiga/travada segurando a porta 48902 (o "loop" da edição Server — cada
	// spawn novo morre com EADDRINUSE em silêncio). Encerra as instâncias
	// antigas antes de subir uma nova, para o bind nunca esbarrar num zumbi.
	if err := killStaleWebServerFn(); err != nil {
		fmt.Printf("⚠ Aviso: não foi possível encerrar instâncias antigas do focusguard-web: %v\n", err)
	}
	if err := spawnWebServerFn(path); err != nil {
		fmt.Printf("Erro ao iniciar o focusguard-web: %v\n", err)
		osExit(1)
	}
	if !waitWebServerFn(5 * time.Second) {
		fmt.Println("⚠ Servidor web iniciado, mas ainda não respondeu. Abrindo o navegador mesmo assim...")
		fmt.Println("  Se a interface não abrir, o log de auditoria do web mostra o motivo:")
		fmt.Println("    focusguard-web.log ao lado do executável, ou em %PROGRAMDATA%\\FocusGuard\\ (Windows) / /var/lib/focusguard/ (Linux)")
	}
	openBrowserFn(webURL)
	fmt.Printf("✔ Interface web: %s\n", webURL)
}
