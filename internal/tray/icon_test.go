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

func TestIcoFromPNG_Header(t *testing.T) {
	pngData, err := GenerateIcon()
	if err != nil {
		t.Fatalf("GenerateIcon erro: %v", err)
	}
	ico := icoFromPNG(pngData)

	wantHeader := []byte{0, 0, 1, 0, 1, 0} // reserved, type=icon, count=1
	if !bytes.HasPrefix(ico, wantHeader) {
		t.Errorf("header do ico = %x, want %x", ico[:6], wantHeader)
	}
	if ico[6] != iconSize || ico[7] != iconSize {
		t.Errorf("dimensoes do entry = %dx%d, want %d", ico[6], ico[7], iconSize)
	}
	n := int(ico[14]) | int(ico[15])<<8 | int(ico[16])<<16 | int(ico[17])<<24
	if n != len(pngData) {
		t.Errorf("bytesInRes = %d, want %d", n, len(pngData))
	}
	if want := 22 + len(pngData); len(ico) != want {
		t.Errorf("len(ico) = %d, want %d", len(ico), want)
	}
	if !bytes.Equal(ico[22:], pngData) {
		t.Error("payload PNG do ico nao corresponde")
	}
}
