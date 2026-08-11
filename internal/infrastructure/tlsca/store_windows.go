//go:build windows

package tlsca

import (
	"fmt"
	"os"
	"strings"
)

// installIntoStore instala a CA no Root store do Windows via certutil (precisa
// de elevação). O cert é gravado temporariamente como .cer ao lado do PEM e
// removido após o comando.
func (c *CA) installIntoStore(run StoreRunner) error {
	cer := c.CertPath() + ".cer"
	if err := os.WriteFile(cer, c.CertPEM(), 0o644); err != nil {
		return fmt.Errorf("tlsca: gravar cert temporário: %w", err)
	}
	defer os.Remove(cer)

	out, err := run("certutil", "-addstore", "-f", "Root", cer)
	if err != nil {
		return fmt.Errorf("tlsca: certutil -addstore falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeFromStore remove a CA do Root store via certutil -delstore (CN é o
// identificador aceito pelo comando).
func (c *CA) removeFromStore(run StoreRunner) error {
	out, err := run("certutil", "-delstore", "Root", c.SubjectCN())
	if err != nil {
		return fmt.Errorf("tlsca: certutil -delstore falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInStore detecta a CA no Root store: o certutil -store Root lista os
// certificados e o CN da CA aparece na linha "Subject:" de cada entrada.
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	out, err := run("certutil", "-store", "Root")
	if err != nil {
		return false, fmt.Errorf("tlsca: certutil -store falhou: %v", err)
	}
	return strings.Contains(string(out), c.SubjectCN()), nil
}
