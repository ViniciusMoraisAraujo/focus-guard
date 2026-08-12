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

// IsInStore detecta a CA no Root store pelo SERIAL (não pelo CN): uma CA
// regenerada tem o MESMO CN ("FocusGuard Local CA"), então o cert antigo no
// store faria a reinstalação ser pulada e o navegador rejeitaria os leafs
// novos (chain assinada pela CA nova) — o serial é o identificador estável
// entre gerações. O serial é aleatório (128 bits): a chance de o hex aparecer
// por acaso em outro cert do output é nula. O certutil exibe o serial em
// pares hex separados por espaço (e pode prefixar 00 quando o bit alto está
// setado), então a normalização remove tudo que não é hex antes de comparar
// em minúsculas — cobre também o serial exibido sem espaços/prefixo.
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	out, err := run("certutil", "-store", "Root")
	if err != nil {
		return false, fmt.Errorf("tlsca: certutil -store falhou: %v", err)
	}
	serialHex := strings.ToLower(c.crt.SerialNumber.Text(16))
	var b strings.Builder
	for _, r := range out {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			b.WriteByte(r)
		}
	}
	return strings.Contains(strings.ToLower(b.String()), serialHex), nil
}
