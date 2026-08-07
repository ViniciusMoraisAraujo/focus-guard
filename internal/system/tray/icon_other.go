//go:build !windows

package tray

// platformIcon returns the tray icon for non-Windows platforms (PNG).
func platformIcon() []byte {
	data, err := GenerateIcon()
	if err != nil {
		return nil
	}
	return data
}
