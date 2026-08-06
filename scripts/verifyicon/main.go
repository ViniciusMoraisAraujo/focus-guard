//go:build ignore

// VerifyIcon verifica se o ícone embutido num executável (bin/focusguard-tray.exe
// por padrão) corresponde ao packaging/focusguard.ico. Como o go-winres re-embute
// o .ico com variações (ex.: reordena entradas ou re-encoda PNGs), a comparação
// é semântica: mesmas dimensões por entrada e pixels idênticos por payload PNG.
// Uso:
//
//	go run ./scripts/verifyicon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
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

type icoEntry struct {
	width, height byte
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
	orig, err := os.ReadFile("packaging/focusguard.ico")
	if err != nil {
		return err
	}

	gotEntries, gotPNGs, err := splitICO(got)
	if err != nil {
		return fmt.Errorf("ícone remontado inválido: %w", err)
	}
	origEntries, origPNGs, err := splitICO(orig)
	if err != nil {
		return err
	}

	if len(gotEntries) != len(origEntries) {
		return fmt.Errorf("nº de imagens difere: remontado %d vs focusguard.ico %d", len(gotEntries), len(origEntries))
	}

	// O go-winres pode reordenar as entradas do .ico ao embutir; casa por
	// dimensões em vez de por índice.
	byDims := make(map[[2]byte][]byte, len(origEntries))
	for i := range origEntries {
		byDims[[2]byte{origEntries[i].width, origEntries[i].height}] = origPNGs[i]
	}

	checked := 0
	for i := range gotEntries {
		ge := gotEntries[i]
		key := [2]byte{ge.width, ge.height}
		want, ok := byDims[key]
		if !ok {
			return fmt.Errorf("entrada %d: %dx%d não existe no focusguard.ico", i, sizeOf(ge.width), sizeOf(ge.height))
		}
		if !imagesEqual(gotPNGs[i], want) {
			return fmt.Errorf("entrada %d (%dx%d): pixels divergem", i, sizeOf(ge.width), sizeOf(ge.height))
		}
		fmt.Printf("  ✔ %dx%d pixels idênticos\n", sizeOf(ge.width), sizeOf(ge.height))
		checked++
	}
	fmt.Printf("✔ ícone remontado == focusguard.ico (%d bytes, %d/%d imagens verificadas)\n", len(got), checked, len(gotEntries))
	return nil
}

// splitICO separa o .ico em suas entradas (diretório) e payloads PNG.
func splitICO(data []byte) ([]icoEntry, [][]byte, error) {
	if len(data) < 6 {
		return nil, nil, fmt.Errorf("arquivo curto demais")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	entries := make([]icoEntry, 0, count)
	pngs := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		off := 6 + i*16
		if off+16 > len(data) {
			return nil, nil, fmt.Errorf("diretório truncado na entrada %d", i)
		}
		length := binary.LittleEndian.Uint32(data[off+8:])
		payloadOff := binary.LittleEndian.Uint32(data[off+12:])
		if int(payloadOff)+int(length) > len(data) {
			return nil, nil, fmt.Errorf("payload %d fora do arquivo", i)
		}
		entries = append(entries, icoEntry{
			width:  data[off],
			height: data[off+1],
		})
		pngs = append(pngs, data[payloadOff:payloadOff+length])
	}
	return entries, pngs, nil
}

func sizeOf(b byte) int {
	if b == 0 {
		return 256
	}
	return int(b)
}

// imagesEqual compara dois payloads PNG pixel a pixel (canais RGBA).
func imagesEqual(a, b []byte) bool {
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return false
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return false
	}
	if ia.Bounds() != ib.Bounds() {
		return false
	}
	ba := ia.Bounds()
	for y := ba.Min.Y; y < ba.Max.Y; y++ {
		for x := ba.Min.X; x < ba.Max.X; x++ {
			if !sameRGBA(ia.At(x, y), ib.At(x, y)) {
				return false
			}
		}
	}
	return true
}

func sameRGBA(a, b interface{ RGBA() (uint32, uint32, uint32, uint32) }) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

