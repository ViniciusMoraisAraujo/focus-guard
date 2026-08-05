// focusguard-web serve a interface web do FocusGuard (React + TypeScript) e
// faz proxy das ações IPC para o daemon, permitindo controlar o FocusGuard
// pelo navegador sem acesso direto ao socket/TCP.
//
// O processo roda em user-space (sem manifest de administrador) e por demanda
// — o comando "focusguard web" o inicia quando necessário. A UI compilada é
// embutida no binário via go:embed (execute "make ui" antes de compilar para
// incluir a última versão do frontend).
package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"focusguard/internal/httpapi"
	"focusguard/internal/ipc"
)

//go:embed all:assets
var embeddedAssets embed.FS

func main() {
	stopLog := setupLogging()
	defer stopLog()

	addr := flag.String("addr", httpapi.DefaultAddr, "endereço de escuta (somente localhost)")
	assetsDir := flag.String("assets", "", "diretório alternativo com a UI compilada (dev; vazio = embutida)")
	flag.Parse()

	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		log.Fatalf("focusguard-web: assets embutidos indisponíveis: %v", err)
	}
	if *assetsDir != "" {
		if fi, err := os.Stat(*assetsDir); err != nil || !fi.IsDir() {
			log.Fatalf("focusguard-web: diretório de assets inválido: %q", *assetsDir)
		}
		assets = os.DirFS(*assetsDir)
	}

	handler := httpapi.New(ipc.NewClient(), assets).Handler()
	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("[focusguard-web] Interface web: http://%s", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		if isAddrInUse(err) {
			// O "loop" da edição Server: uma instância antiga segurando a
			// porta faz cada spawn novo morrer aqui em silêncio. Deixa a
			// causa explícita no log de auditoria para quem investigar.
			log.Fatalf("focusguard-web: a porta %s já está em uso por outra instância (%v). Encerre o processo antigo (taskkill /f /im focusguard-web.exe) e tente de novo — ou abra http://%s no navegador se a interface já estiver no ar.", *addr, err, *addr)
		}
		log.Fatalf("focusguard-web: %v", err)
	}
}

// isAddrInUse reports whether err is a TCP bind "address already in use"
// (EADDRINUSE / WSAEADDRINUSE=10048 / mensagens localizadas do Windows).
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 10048 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "in use") || strings.Contains(msg, "em uso") ||
		strings.Contains(msg, "already in use") || strings.Contains(msg, "uma utilização")
}
