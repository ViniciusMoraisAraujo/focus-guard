//go:build linux

package tlsca

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// caCertsDir é o diretório de âncoras locais (Debian-style
// update-ca-certificates — padrão nas distros com systemd que o FocusGuard
// suporta; o update-ca-certificates consome os .crt daqui). Var (não const)
// para os testes apontarem para diretórios temporários e nunca tocarem no
// trust store real da máquina.
var caCertsDir = "/usr/local/share/ca-certificates"

// storeInstalledDir é onde o update-ca-certificates INSTALA as âncoras
// locais (cópia <nome>.pem): a prova real de que a CA está no trust store.
var storeInstalledDir = "/etc/ssl/certs"

// storeFileName é o nome do arquivo no ca-certs dir (extensão .crt é exigida
// pelo update-ca-certificates).
const storeFileName = "focusguard-ca.crt"

// storeInstalledName é o nome da cópia instalada em storeInstalledDir (o
// update-ca-certificates troca o sufixo .crt por .pem).
const storeInstalledName = "focusguard-ca.pem"

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

// IsInStore detecta a CA no trust store: a prova real é a CÓPIA instalada em
// storeInstalledDir (o update-ca-certificates copia a âncora local para lá).
// Checar só o arquivo-fonte em caCertsDir reportaria "instalada" mesmo quando
// o update-ca-certificates falhou depois do write. A cópia precisa ser o
// NOSSO certificado (DER idêntico): um arquivo estranho com o mesmo nome, ou
// uma âncora antiga de uma CA regenerada, não conta como instalada.
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	installed := filepath.Join(storeInstalledDir, storeInstalledName)
	data, err := os.ReadFile(installed)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false, nil // não é PEM — não é a nossa âncora
	}
	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, nil // PEM inválido — não é a nossa âncora
	}
	return bytes.Equal(crt.Raw, c.crt.Raw), nil
}
