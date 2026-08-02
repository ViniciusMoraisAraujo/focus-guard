// Package icon renders the FocusGuard shield at any size and builds the
// multi-size Windows .ico embedded in the executables via go-winres, plus the
// 256px PNG used by the Linux desktop shortcut. It is dependency-free (only
// the standard library) so it can be used by build tooling with CGO disabled.
package icon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
)

// Shield palette (same as the tray icon).
var (
	shieldCol = color.RGBA{0x16, 0x2A, 0x4A, 0xFF}
	checkCol  = color.RGBA{0x2E, 0xCC, 0x71, 0xFF}
)

// GeneratePNG renders the FocusGuard shield (a circle with a checkmark) at
// the given size as PNG bytes. The drawing is scaled proportionally from the
// 32px reference used by the tray icon.
func GeneratePNG(size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("icon: tamanho deve ser positivo (got %d)", size)
	}
	img := renderShield(size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateICO builds a multi-size ICO container (Vista+ supports PNG
// payloads) with the given sizes, sorted ascending and de-duplicated. The
// directory entries follow the ICONDIR layout: width/height (0 = 256),
// planes=1, bitCount=32 and PNG payloads at the end of the file.
func GenerateICO(sizes []int) ([]byte, error) {
	if len(sizes) == 0 {
		return nil, errors.New("icon: nenhum tamanho para gerar")
	}
	sizes = slices.Clone(sizes)
	slices.Sort(sizes)
	sizes = slices.Compact(sizes)

	payloads := make([][]byte, len(sizes))
	for i, size := range sizes {
		data, err := GeneratePNG(size)
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

// renderShield draws the shield at the requested size, scaling the reference
// 32px coordinates proportionally.
func renderShield(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scale := func(v int) int { return v * size / 32 }

	cx, cy := size/2, size/2
	radius := scale(15)
	drawFilledCircle(img, cx, cy, radius, shieldCol)

	lineW := scale(3)
	if lineW < 1 {
		lineW = 1
	}
	// checkmark: (9,16)-(14,21)-(24,10) in the 32px reference
	drawLine(img, scale(9), scale(16), scale(14), scale(21), checkCol, lineW, size)
	drawLine(img, scale(14), scale(21), scale(24), scale(10), checkCol, lineW, size)
	return img
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	b := img.Bounds()
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r && inBounds(b, x, y) {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA, width, size int) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy > 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	half := width / 2
	b := img.Bounds()
	for {
		for oy := -half; oy <= half; oy++ {
			for ox := -half; ox <= half; ox++ {
				if inBounds(b, x0+ox, y0+oy) {
					img.SetRGBA(x0+ox, y0+oy, col)
				}
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func inBounds(b image.Rectangle, x, y int) bool {
	return x >= b.Min.X && y >= b.Min.Y && x < b.Max.X && y < b.Max.Y
}
