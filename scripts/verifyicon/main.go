//go:build ignore

// VerifyIcon verifica se o ícone embutido num executável (bin/focusguard-tray.exe
// por padrão) remonta byte a byte o focusguard.ico da raiz. Uso:
//
//	go run ./scripts/verifyicon
package main

import (
	"bytes"
	"fmt"
	"os"

	"focusguard/internal/tray"
	"golang.org/x/sys/windows"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func run() error {
	mod, err := windows.LoadLibraryEx("bin/focusguard-tray.exe", 0, windows.LOAD_LIBRARY_AS_DATAFILE)
	if err != nil {
		return err
	}
	defer windows.FreeLibrary(mod)

	got, err := tray.IconFromModule(mod)
	if err != nil {
		return err
	}
	orig, err := os.ReadFile("focusguard.ico")
	if err != nil {
		return err
	}
	if bytes.Equal(got, orig) {
		fmt.Printf("✔ ícone remontado == focusguard.ico (%d bytes)\n", len(got))
		return nil
	}
	return fmt.Errorf("ícone remontado (%d bytes) difere de focusguard.ico (%d bytes)", len(got), len(orig))
}
