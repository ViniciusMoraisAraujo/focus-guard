//go:build windows

package tray

// platformIcon returns the tray icon for Windows, which must be ICO bytes.
func platformIcon() []byte {
	data, err := GenerateIcon()
	if err != nil {
		return nil
	}
	return icoFromPNG(data)
}
