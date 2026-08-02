package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

type exitPanic struct {
	code int
}

func runMain(args []string) (caught bool, code int) {
	orig := os.Args
	os.Args = append([]string{"focusguard-icon"}, args...)
	defer func() { os.Args = orig }()

	origExit := osExit
	osExit = func(c int) { panic(exitPanic{c}) }
	defer func() { osExit = origExit }()

	defer func() {
		if r := recover(); r != nil {
			if ep, ok := r.(exitPanic); ok {
				caught = true
				code = ep.code
			} else {
				panic(r)
			}
		}
	}()

	main()
	return false, 0
}

// TestRun_GeneratesICOAndPNG verifies the generator writes a valid multi-size
// .ico and a 256px .png into the output directory.
func TestRun_GeneratesICOAndPNG(t *testing.T) {
	dir := t.TempDir()
	icoPath := filepath.Join(dir, "focusguard.ico")
	pngPath := filepath.Join(dir, "focusguard.png")

	if err := run(icoPath, pngPath, "16,32,48,64"); err != nil {
		t.Fatalf("run erro: %v", err)
	}

	ico, err := os.ReadFile(icoPath)
	if err != nil {
		t.Fatalf("ReadFile ico: %v", err)
	}
	if got := int(binary.LittleEndian.Uint16(ico[4:6])); got != 4 {
		t.Errorf("ico count = %d, want 4", got)
	}

	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		t.Fatalf("ReadFile png: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Errorf("png %dx%d, want 256x256", b.Dx(), b.Dy())
	}
}

// TestRun_DefaultSizes verifies the default size list produces the standard
// Windows sizes.
func TestRun_DefaultSizes(t *testing.T) {
	dir := t.TempDir()
	if err := run(filepath.Join(dir, "a.ico"), filepath.Join(dir, "a.png"), ""); err != nil {
		t.Fatalf("run erro: %v", err)
	}
	ico, err := os.ReadFile(filepath.Join(dir, "a.ico"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := int(binary.LittleEndian.Uint16(ico[4:6])); got != len(defaultSizes) {
		t.Errorf("count = %d, want %d", got, len(defaultSizes))
	}
}

// TestRun_InvalidSize verifies malformed size lists are rejected.
func TestRun_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	if err := run(filepath.Join(dir, "b.ico"), filepath.Join(dir, "b.png"), "16,abc"); err == nil {
		t.Error("expected error for invalid size token")
	}
	if err := run(filepath.Join(dir, "c.ico"), filepath.Join(dir, "c.png"), "0,16"); err == nil {
		t.Error("expected error for non-positive size")
	}
}

// TestMain_ExitsOnError verifies main exits non-zero when generation fails.
func TestMain_ExitsOnError(t *testing.T) {
	caught, code := runMain([]string{"-sizes", "bogus"})
	if !caught {
		t.Fatal("expected osExit to be called")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
