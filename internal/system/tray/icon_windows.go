//go:build windows

package tray

// platformIcon returns the tray icon for Windows, which must be ICO bytes
// extraídos dos recursos do executável (o focusguard.ico embutido). Todos os
// builds Windows geram resource.syso com o ícone, então não há fallback de
// renderização em runtime: se o ícone estiver ausente (build sem recursos),
// retorna nil e o systray fica sem ícone personalizado.
func platformIcon() []byte {
	data, err := embeddedIcon()
	if err != nil {
		return nil
	}
	return data
}
