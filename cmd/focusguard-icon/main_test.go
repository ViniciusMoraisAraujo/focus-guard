package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

type exitPanic struct {
	code int
}

// testSource writes a synthetic 32x32 source PNG into a temp dir and returns
// its path, so the generator tests do not depend on the repo's artwork.
func testSource(t *testing.T) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 8; y < 24; y++ {
		for x := 8; x < 24; x++ {
			img.SetRGBA(x, y, color.RGBA{0x16, 0x2A, 0x4A, 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encodar fonte: %v", err)
	}

	path := filepath.Join(t.TempDir(), "source.png")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("escrever fonte: %v", err)
	}
	return path
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
	src := testSource(t)
	icoPath := filepath.Join(dir, "focusguard.ico")
	pngPath := filepath.Join(dir, "focusguard.png")

	if err := run(src, icoPath, pngPath, "", "16,32,48,64"); err != nil {
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
	src := testSource(t)
	if err := run(src, filepath.Join(dir, "a.ico"), filepath.Join(dir, "a.png"), "", ""); err != nil {
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

// TestRun_GeneratesTrayIcon verifies the 32px tray PNG is written and has the
// right dimensions.
func TestRun_GeneratesTrayIcon(t *testing.T) {
	dir := t.TempDir()
	src := testSource(t)
	trayPath := filepath.Join(dir, "icon_source.png")

	if err := run(src, filepath.Join(dir, "a.ico"), filepath.Join(dir, "a.png"), trayPath, ""); err != nil {
		t.Fatalf("run erro: %v", err)
	}

	data, err := os.ReadFile(trayPath)
	if err != nil {
		t.Fatalf("ReadFile tray: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("tray png inválido: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Errorf("tray %dx%d, want 32x32", b.Dx(), b.Dy())
	}
}

// TestRun_MissingSource verifies a nonexistent source file is reported.
func TestRun_MissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := run(filepath.Join(dir, "nope.png"), filepath.Join(dir, "a.ico"), filepath.Join(dir, "a.png"), "", ""); err == nil {
		t.Error("expected error for missing source image")
	}
}

// TestRun_InvalidSize verifies malformed size lists are rejected.
func TestRun_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	src := testSource(t)
	if err := run(src, filepath.Join(dir, "b.ico"), filepath.Join(dir, "b.png"), "", "16,abc"); err == nil {
		t.Error("expected error for invalid size token")
	}
	if err := run(src, filepath.Join(dir, "c.ico"), filepath.Join(dir, "c.png"), "", "0,16"); err == nil {
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
