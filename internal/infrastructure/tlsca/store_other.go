//go:build !windows && !linux

package tlsca

import "fmt"

// installIntoStore não é suportado fora de Windows/Linux (o FocusGuard só
// instala a CA nesses dois; outras plataformas seguem com cert auto-assinado).
func (c *CA) installIntoStore(run StoreRunner) error {
	return fmt.Errorf("tlsca: instalação no trust store não suportada nesta plataforma")
}

// removeFromStore é no-op em plataformas sem suporte.
func (c *CA) removeFromStore(run StoreRunner) error {
	return nil
}

// IsInStore reporta false em plataformas sem suporte.
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	return false, nil
}
