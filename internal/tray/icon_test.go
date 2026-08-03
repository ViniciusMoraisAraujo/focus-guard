package tray

import (
	"bytes"
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
	// o ícone precisa ter pixels não transparentes (o artwork real cobre o
	// centro); o formato exato varia com o design, então só checamos que não
	// está em branco.
	opaque := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0 {
				opaque++
			}
		}
	}
	if opaque == 0 {
		t.Error("icone totalmente transparente (artwork ausente?)")
	}
}
