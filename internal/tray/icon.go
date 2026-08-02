package tray

import (
	"bytes"
	"encoding/binary"

	"focusguard/internal/icon"
)

const iconSize = 32

// GenerateIcon renders the FocusGuard tray icon (a shield with a checkmark)
// as PNG bytes. The drawing lives in internal/icon so the Windows .ico and
// the Linux desktop PNG share the exact same artwork.
func GenerateIcon() ([]byte, error) {
	return icon.GeneratePNG(iconSize)
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
