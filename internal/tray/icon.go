package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

const iconSize = 32

// GenerateIcon renders the FocusGuard tray icon (a shield with a checkmark)
// as PNG bytes.
func GenerateIcon() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	shield := color.RGBA{0x16, 0x2A, 0x4A, 0xFF}
	check := color.RGBA{0x2E, 0xCC, 0x71, 0xFF}
	drawFilledCircle(img, 16, 16, 15, shield)
	drawLine(img, 9, 16, 14, 21, check, 3)
	drawLine(img, 14, 21, 24, 10, check, 3)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// icoFromPNG wraps PNG data in an ICO container (Vista+ supports PNG payloads).
func icoFromPNG(pngData []byte) []byte {
	buf := &bytes.Buffer{}
	buf.Write([]byte{0, 0, 1, 0})
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	buf.WriteByte(iconSize)
	buf.WriteByte(iconSize)
	buf.WriteByte(0)
	buf.WriteByte(0)
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(32))
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(pngData)))
	_ = binary.Write(buf, binary.LittleEndian, uint32(22))
	buf.Write(pngData)
	return buf.Bytes()
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				setPixel(img, x, y, col)
			}
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA, width int) {
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
	maxSteps := dx - dy + 2
	for i := 0; i < maxSteps; i++ {
		for oy := -half; oy <= half; oy++ {
			for ox := -half; ox <= half; ox++ {
				setPixel(img, x0+ox, y0+oy, col)
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

func setPixel(img *image.RGBA, x, y int, col color.RGBA) {
	if x >= 0 && y >= 0 && x < iconSize && y < iconSize {
		img.SetRGBA(x, y, col)
	}
}
