//go:build linux

package tlsca

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// caCertsDir é o diretório de âncoras de confiança do sistema (Debian-style
// update-ca-certificates — padrão nas distros com systemd que o FocusGuard
// suporta).
const caCertsDir = "/usr/local/share/ca-certificates"

// storeFileName é o nome do arquivo no ca-certs dir (extensão .crt é exigida
// pelo update-ca-certificates).
const storeFileName = "focusguard-ca.crt"

// installIntoStore instala a CA no trust store do Linux: copia o PEM para
// /usr/local/share/ca-certificates e roda update-ca-certificates (precisa de
// root — o daemon roda como root).
func (c *CA) installIntoStore(run StoreRunner) error {
	dst := filepath.Join(caCertsDir, storeFileName)
	if err := os.WriteFile(dst, c.CertPEM(), 0o644); err != nil {
		return fmt.Errorf("tlsca: gravar %s: %w", dst, err)
	}
	out, err := run("update-ca-certificates")
	if err != nil {
		return fmt.Errorf("tlsca: update-ca-certificates falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeFromStore remove o arquivo da CA e re-roda update-ca-certificates
// (--fresh descarta os symlinks órfãos do store antigo).
func (c *CA) removeFromStore(run StoreRunner) error {
	dst := filepath.Join(caCertsDir, storeFileName)
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tlsca: remover %s: %w", dst, err)
	}
	out, err := run("update-ca-certificates", "--fresh")
	if err != nil {
		return fmt.Errorf("tlsca: update-ca-certificates --fresh falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInStore detecta a CA no trust store: o arquivo persistido no ca-certs dir
// é a fonte da verdade (o update-ca-certificates consome exatamente ele).
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	_, err := os.Stat(filepath.Join(caCertsDir, storeFileName))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
