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
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
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
		log.Fatalf("focusguard-web: %v", err)
	}
}
