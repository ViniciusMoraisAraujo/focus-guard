// Package icon renders the FocusGuard artwork at any size and builds the
// multi-size Windows .ico embedded in the executables via go-winres, plus the
// 256px PNG used by the Linux desktop shortcut. The canonical source is the
// high-res PNG in img/focusguard.png, resized here with CatmullRom (the best
// quality the golang.org/x/image/draw scalers offer for large downscales).
package icon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"

	"golang.org/x/image/draw"
)

// LoadSource decodes a PNG source image from path — the canonical artwork
// (img/focusguard.png). Build tooling resizes it into the .ico and desktop
// .png, so the only source of truth is the file the designer edits.
func LoadSource(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("icon: abrir fonte %s: %w", path, err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("icon: decodificar fonte %s: %w", path, err)
	}
	return img, nil
}

// GeneratePNG resizes src to the requested size (CatmullRom, high quality for
// downscaling large artwork) and encodes it as PNG bytes.
func GeneratePNG(src image.Image, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("icon: tamanho deve ser positivo (got %d)", size)
	}

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateICO builds a multi-size ICO container (Vista+ supports PNG
// payloads) with the given sizes, sorted ascending and de-duplicated. The
// directory entries follow the ICONDIR layout: width/height (0 = 256),
// planes=1, bitCount=32 and PNG payloads at the end of the file.
func GenerateICO(src image.Image, sizes []int) ([]byte, error) {
	if len(sizes) == 0 {
		return nil, errors.New("icon: nenhum tamanho para gerar")
	}
	sizes = slices.Clone(sizes)
	slices.Sort(sizes)
	sizes = slices.Compact(sizes)

	payloads := make([][]byte, len(sizes))
	for i, size := range sizes {
		data, err := GeneratePNG(src, size)
		if err != nil {
			return nil, err
		}
		payloads[i] = data
	}

	const entryLen = 16
	dataStart := 6 + entryLen*len(sizes)

	buf := &bytes.Buffer{}
	buf.Write([]byte{0, 0, 1, 0}) // reserved=0, type=1 (icon)
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(sizes)))

	offset := dataStart
	for i, size := range sizes {
		dim := byte(size)
		if size == 256 {
			dim = 0 // 0 = 256 in ICONDIR
		}
		buf.WriteByte(dim)                                     // width
		buf.WriteByte(dim)                                     // height
		buf.WriteByte(0)                                       // color count
		buf.WriteByte(0)                                       // reserved
		_ = binary.Write(buf, binary.LittleEndian, uint16(1))  // planes
		_ = binary.Write(buf, binary.LittleEndian, uint16(32)) // bit count
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(payloads[i])))
		_ = binary.Write(buf, binary.LittleEndian, uint32(offset))
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		buf.Write(p)
	}
	return buf.Bytes(), nil
}
