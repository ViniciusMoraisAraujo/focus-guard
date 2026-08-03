package tray

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestGenerateIcon_IsValidPNG(t *testing.T) {
	data, err := GenerateIcon()
	if err != nil {
		t.Fatalf("GenerateIcon erro: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png invalido: %v", err)
	}
	if b := img.Bounds(); b.Dx() != iconSize || b.Dy() != iconSize {
		t.Errorf("icone %dx%d, want %dx%d", b.Dx(), b.Dy(), iconSize, iconSize)
	}
}

func TestGenerateIcon_IsNotBlank(t *testing.T) {
	data, err := GenerateIcon()
	if err != nil {
		t.Fatalf("GenerateIcon erro: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png invalido: %v", err)
	}
	// amostra o centro (o escudo desenhado) e um canto (transparente)
	center := img.At(iconSize/2, iconSize/2)
	corner := img.At(0, 0)
	_, _, _, a1 := center.RGBA()
	_, _, _, a2 := corner.RGBA()
	if a1 == 0 {
		t.Error("centro do icone esta transparente (escudo nao desenhado)")
	}
	if a2 != 0 {
		t.Error("canto do icone deveria ser transparente")
	}
}

func TestRenderShield_CenterOpaqueCornersTransparent(t *testing.T) {
	img := RenderShield(64, 4)
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("icone %dx%d, want 64x64", b.Dx(), b.Dy())
	}

	if _, _, _, a := img.At(32, 32).RGBA(); a == 0 {
		t.Error("centro do escudo deveria ser opaco")
	}
	for _, c := range []image.Point{{0, 0}, {63, 0}, {0, 63}, {63, 63}} {
		if _, _, _, a := img.At(c.X, c.Y).RGBA(); a != 0 {
			t.Errorf("canto %v deveria ser transparente", c)
		}
	}
}

func TestRenderShield_ContainsNavyAndGreen(t *testing.T) {
	img := RenderShield(64, 4)

	hasNavy := false
	hasGreen := false
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			if b8 > 80 && b8 > g8 && r8 < 70 {
				hasNavy = true
			}
			if g8 > 140 && g8 > r8 && g8 > b8 {
				hasGreen = true
			}
		}
	}
	if !hasNavy {
		t.Error("escudo deveria conter pixels azul-marinho")
	}
	if !hasGreen {
		t.Error("escudo deveria conter o checkmark verde")
	}
}
