package icon

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strconv"
	"testing"
)

// TestGeneratePNG_ValidAtVariousSizes verifies the shield renders as a valid
// PNG at every supported size, with the expected dimensions.
func TestGeneratePNG_ValidAtVariousSizes(t *testing.T) {
	for _, size := range []int{16, 32, 48, 64, 128, 256} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			data, err := GeneratePNG(size)
			if err != nil {
				t.Fatalf("GeneratePNG(%d) erro: %v", size, err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("png inválido no tamanho %d: %v", size, err)
			}
			if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
				t.Errorf("imagem %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
			}
		})
	}
}

// TestGeneratePNG_NotBlankAtCenter verifies the shield body is drawn (center
// opaque) while the corner stays transparent.
func TestGeneratePNG_NotBlankAtCenter(t *testing.T) {
	data, err := GeneratePNG(32)
	if err != nil {
		t.Fatalf("GeneratePNG erro: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png inválido: %v", err)
	}
	_, _, _, aCenter := img.At(16, 16).RGBA()
	_, _, _, aCorner := img.At(0, 0).RGBA()
	if aCenter == 0 {
		t.Error("centro deveria ser opaco (escudo desenhado)")
	}
	if aCorner != 0 {
		t.Error("canto deveria ser transparente")
	}
}

// TestGeneratePNG_InvalidSize verifies non-positive sizes are rejected.
func TestGeneratePNG_InvalidSize(t *testing.T) {
	for _, size := range []int{0, -1, -32} {
		if _, err := GeneratePNG(size); err == nil {
			t.Errorf("GeneratePNG(%d) deveria errar", size)
		}
	}
}

// TestGenerateICO_MultiSizeHeader verifies the ICONDIR header: reserved=0,
// type=1 (icon) and count=len(sizes).
func TestGenerateICO_MultiSizeHeader(t *testing.T) {
	sizes := []int{16, 32, 48, 64}
	ico, err := GenerateICO(sizes)
	if err != nil {
		t.Fatalf("GenerateICO erro: %v", err)
	}
	if ico[0] != 0 || ico[1] != 0 {
		t.Errorf("reserved = %x%x, want 0000", ico[1], ico[0])
	}
	if ico[2] != 1 || ico[3] != 0 {
		t.Errorf("type = %x%x, want 0100 (icon)", ico[3], ico[2])
	}
	if got := int(binary.LittleEndian.Uint16(ico[4:6])); got != len(sizes) {
		t.Errorf("count = %d, want %d", got, len(sizes))
	}
	if got := len(ico); got != 6+16*len(sizes)+sumPNGLengths(t, sizes) {
		t.Errorf("len(ico) = %d, want header+entries+payloads", got)
	}
}

// TestGenerateICO_Entries verifies each 16-byte directory entry: width/height
// (0 = 256), planes=1, bitCount=32, bytesInRes = payload size and ascending
// offsets starting after header+entries.
func TestGenerateICO_Entries(t *testing.T) {
	sizes := []int{16, 32, 48, 64}
	ico, err := GenerateICO(sizes)
	if err != nil {
		t.Fatalf("GenerateICO erro: %v", err)
	}

	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		entry := ico[6+i*16 : 6+(i+1)*16]
		want := byte(size)
		if size == 256 {
			want = 0
		}
		if entry[0] != want || entry[1] != want {
			t.Errorf("entry[%d] dimensões = %dx%d, want %dx%d", i, entry[0], entry[1], want, want)
		}
		if got := binary.LittleEndian.Uint16(entry[4:6]); got != 1 {
			t.Errorf("entry[%d] planes = %d, want 1", i, got)
		}
		if got := binary.LittleEndian.Uint16(entry[6:8]); got != 32 {
			t.Errorf("entry[%d] bitCount = %d, want 32", i, got)
		}
		payloadLen := int(binary.LittleEndian.Uint32(entry[8:12]))
		if payloadLen <= 0 {
			t.Errorf("entry[%d] bytesInRes = %d, want > 0", i, payloadLen)
		}
		start := int(binary.LittleEndian.Uint32(entry[12:16]))
		if start != offset {
			t.Errorf("entry[%d] offset = %d, want %d", i, start, offset)
		}
		if !bytes.HasPrefix(ico[start:start+8], []byte{0x89, 'P', 'N', 'G'}) {
			t.Errorf("entry[%d] payload não é PNG", i)
		}
		offset += payloadLen
	}
}

// TestGenerateICO_PayloadsArePNG verifies each payload decodes as a PNG with
// the advertised size (256 is encoded as 0 in the directory entry).
func TestGenerateICO_PayloadsArePNG(t *testing.T) {
	sizes := []int{16, 32, 64, 256}
	ico, err := GenerateICO(sizes)
	if err != nil {
		t.Fatalf("GenerateICO erro: %v", err)
	}

	for i, size := range sizes {
		entry := ico[6+i*16 : 6+(i+1)*16]
		payloadLen := int(binary.LittleEndian.Uint32(entry[8:12]))
		start := int(binary.LittleEndian.Uint32(entry[12:16]))
		img, err := png.Decode(bytes.NewReader(ico[start : start+payloadLen]))
		if err != nil {
			t.Fatalf("payload[%d] png inválido: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("payload[%d] %dx%d, want %dx%d", i, b.Dx(), b.Dy(), size, size)
		}
	}
}

// TestGenerateICO_SortedAndDeduped verifies unsorted/duplicated sizes are
// normalized to ascending unique order.
func TestGenerateICO_SortedAndDeduped(t *testing.T) {
	ico, err := GenerateICO([]int{64, 16, 32, 16, 256})
	if err != nil {
		t.Fatalf("GenerateICO erro: %v", err)
	}
	if got := int(binary.LittleEndian.Uint16(ico[4:6])); got != 4 {
		t.Errorf("count = %d, want 4 (16,32,64,256 deduped)", got)
	}
	// verifica ordem crescente pelas dimensões dos entries
	prev := 0
	for i := 0; i < 4; i++ {
		entry := ico[6+i*16 : 6+(i+1)*16]
		w := int(entry[0])
		if w == 0 {
			w = 256
		}
		if w <= prev {
			t.Errorf("ordem dos sizes não crescente em %d", i)
		}
		prev = w
	}
}

// TestGenerateICO_EmptySizes verifies an empty size list is rejected.
func TestGenerateICO_EmptySizes(t *testing.T) {
	if _, err := GenerateICO(nil); err == nil {
		t.Error("GenerateICO(nil) deveria errar")
	}
}

// sumPNGLengths helper: total payload bytes so the test can assert total size.
func sumPNGLengths(t *testing.T, sizes []int) int {
	t.Helper()
	total := 0
	for _, s := range sizes {
		data, err := GeneratePNG(s)
		if err != nil {
			t.Fatalf("GeneratePNG(%d): %v", s, err)
		}
		total += len(data)
	}
	return total
}
