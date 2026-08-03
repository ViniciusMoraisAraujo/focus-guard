package tray

import (
	"errors"

	_ "embed"
)

// iconSize is the tray icon edge, in pixels.
const iconSize = 32

// icon_source.png é o ícone do tray (32px), gerado por cmd/focusguard-icon a
// partir de img/focusguard.png e embutido no binário. É o mesmo artwork do
// focusguard.ico/.png do sistema, então o tray acompanha qualquer redesign.
//
//go:embed icon_source.png
var iconSourcePNG []byte

// GenerateIcon returns the tray icon (a shield with a checkmark) as PNG
// bytes. The drawing lives in the embedded 32px asset so the Windows .ico,
// the Linux desktop PNG and the tray share the exact same artwork.
func GenerateIcon() ([]byte, error) {
	if len(iconSourcePNG) == 0 {
		return nil, errors.New("tray: icon_source.png ausente (rode make icon)")
	}
	return iconSourcePNG, nil
}
