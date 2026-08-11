package tlsca

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// StoreRunner é o executor de comandos do sistema usado pelos helpers de trust
// store (certutil no Windows, update-ca-certificates no Linux) — injetável nos
// testes, espelhando o padrão do doctor (execOutput).
type StoreRunner func(name string, args ...string) ([]byte, error)

// DefaultStoreRunner é o executor real (exec.Command) usado quando o chamador
// não injeta um runner (daemon/CLI em produção).
func DefaultStoreRunner() StoreRunner {
	return func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
}

// InstallIntoStore instala o certificado da CA no trust store do SO (Root no
// Windows; ca-certificates no Linux). Requer privilégio elevado — o daemon
// roda como SYSTEM/root e os comandos de instalação falham sem ele. Idempotente:
// a CA já presente no store é um no-op (detectada antes do comando de escrita).
func (c *CA) InstallIntoStore(run StoreRunner) error {
	if run == nil {
		return fmt.Errorf("tlsca: runner ausente")
	}
	installed, err := c.IsInStore(run)
	if err != nil {
		return fmt.Errorf("tlsca: verificar CA no trust store: %w", err)
	}
	if installed {
		return nil
	}
	if err := c.installIntoStore(run); err != nil {
		return err
	}
	return nil
}

// RemoveFromStore remove o certificado da CA do trust store do SO. Best-effort
// no mesmo espírito do InstallIntoStore: a CA ausente é um no-op.
func (c *CA) RemoveFromStore(run StoreRunner) error {
	if run == nil {
		return fmt.Errorf("tlsca: runner ausente")
	}
	installed, err := c.IsInStore(run)
	if err != nil {
		return fmt.Errorf("tlsca: verificar CA no trust store: %w", err)
	}
	if !installed {
		return nil
	}
	return c.removeFromStore(run)
}

// CertPath devolve o caminho do arquivo do certificado da CA persistido.
func (c *CA) CertPath() string {
	return filepath.Join(c.dir, caCertFile)
}
