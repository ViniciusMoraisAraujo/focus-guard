// Command focusguard-icon generates the FocusGuard application icon from the
// canonical artwork (img/focusguard.png):
//
//   - focusguard.ico: multi-size Windows ICO (16..256) embedded in the .exe
//     via go-winres (RT_GROUP_ICON) — this is the icon Explorer shows for the
//     daemon and the CLI, and the one the desktop shortcut references.
//   - focusguard.png: 256px PNG used by the Linux desktop shortcut
//     (~/.local/share/icons/hicolor/256x256/apps/focusguard.png).
//   - internal/tray/icon_source.png: 32px PNG embedded in the tray binary
//     (Linux tray) — same artwork, so every icon stays consistent.
//
// It is a build-time tool (pure stdlib + golang.org/x/image, no CGO) so the
// release pipeline can regenerate both artifacts deterministically.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"focusguard/internal/icon"
)

var osExit = os.Exit

// defaultSizes are the standard Windows icon sizes.
var defaultSizes = []int{16, 32, 48, 64, 128, 256}

func main() {
	src := flag.String("src", "img/focusguard.png", "caminho do PNG de origem (artwork 1024px)")
	icoOut := flag.String("ico", "focusguard.ico", "caminho do .ico multi-tamanho gerado")
	pngOut := flag.String("png", "focusguard.png", "caminho do .png 256px gerado")
	trayOut := flag.String("tray", "internal/tray/icon_source.png", "caminho do .png 32px do tray; vazio desativa")
	sizesFlag := flag.String("sizes", "", "tamanhos do .ico separados por vírgula (default: 16,32,48,64,128,256)")
	flag.Parse()

	if err := run(*src, *icoOut, *pngOut, *trayOut, *sizesFlag); err != nil {
		fmt.Fprintf(os.Stderr, "focusguard-icon: %v\n", err)
		osExit(1)
	}
}

// run loads the artwork and writes all three outputs. Extracted from main so
// tests can call it directly.
func run(src, icoOut, pngOut, trayOut, sizesFlag string) error {
	sizes := defaultSizes
	if sizesFlag != "" {
		parsed, err := parseSizes(sizesFlag)
		if err != nil {
			return err
		}
		sizes = parsed
	}

	art, err := icon.LoadSource(src)
	if err != nil {
		return err
	}

	ico, err := icon.GenerateICO(art, sizes)
	if err != nil {
		return fmt.Errorf("gerar .ico: %w", err)
	}
	if err := os.WriteFile(icoOut, ico, 0644); err != nil {
		return fmt.Errorf("escrever %s: %w", icoOut, err)
	}

	png, err := icon.GeneratePNG(art, 256)
	if err != nil {
		return fmt.Errorf("gerar .png: %w", err)
	}
	if err := os.WriteFile(pngOut, png, 0644); err != nil {
		return fmt.Errorf("escrever %s: %w", pngOut, err)
	}

	if trayOut != "" {
		tray, err := icon.GeneratePNG(art, 32)
		if err != nil {
			return fmt.Errorf("gerar .png do tray: %w", err)
		}
		if err := os.WriteFile(trayOut, tray, 0644); err != nil {
			return fmt.Errorf("escrever %s: %w", trayOut, err)
		}
	}
	return nil
}

func parseSizes(s string) ([]int, error) {
	var sizes []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("tamanho inválido %q", part)
		}
		sizes = append(sizes, n)
	}
	if len(sizes) == 0 {
		return nil, fmt.Errorf("nenhum tamanho válido em %q", s)
	}
	return sizes, nil
}
