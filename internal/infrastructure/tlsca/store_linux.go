//go:build linux

package tlsca

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
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

// chromiumPolicyDirs lista os diretórios de políticas gerenciadas do Chromium
// e navegadores derivados (Brave, Chrome, Chromium, Edge) no Linux. Var para testes.
var chromiumPolicyDirs = []string{
	"/etc/brave/policies/managed",
	"/etc/chromium/policies/managed",
	"/etc/opt/chrome/policies/managed",
	"/etc/opt/edge/policies/managed",
}

// firefoxPolicyDirs lista os diretórios de políticas corporativas do Firefox no Linux.
var firefoxPolicyDirs = []string{
	"/etc/firefox/policies",
}

// storeFileName é o nome do arquivo no ca-certs dir (extensão .crt é exigida
// pelo update-ca-certificates).
const storeFileName = "focusguard-ca.crt"

// storeInstalledName é o nome da cópia instalada em storeInstalledDir (o
// update-ca-certificates troca o sufixo .crt por .pem).
const storeInstalledName = "focusguard-ca.pem"

// chromiumPolicyFileName é o nome do arquivo de política gerenciada para navegadores Chromium.
const chromiumPolicyFileName = "focusguard-ca.json"

// firefoxPolicyFileName é o nome do arquivo de política corporativa para o Firefox.
const firefoxPolicyFileName = "policies.json"

type chromiumPolicyDoc struct {
	CACertificates []string `json:"CACertificates"`
}

// installIntoStore instala a CA no trust store do Linux: copia o PEM para
// /usr/local/share/ca-certificates e roda update-ca-certificates (precisa de
// root — o daemon roda como root), além de configurar as Enterprise Policies
// para navegadores Chromium (Brave, Chrome, Edge) e Firefox, garantindo confiança
// automática sem requerer importação manual pelo usuário.
func (c *CA) installIntoStore(run StoreRunner) error {
	dst := filepath.Join(caCertsDir, storeFileName)
	if err := os.WriteFile(dst, c.CertPEM(), 0o644); err != nil {
		return fmt.Errorf("tlsca: gravar %s: %w", dst, err)
	}
	out, err := run("update-ca-certificates")
	if err != nil {
		return fmt.Errorf("tlsca: update-ca-certificates falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	b64Cert := base64.StdEncoding.EncodeToString(c.crt.Raw)
	chromDoc := chromiumPolicyDoc{
		CACertificates: []string{b64Cert},
	}
	chromData, err := json.MarshalIndent(chromDoc, "", "  ")
	if err != nil {
		return fmt.Errorf("tlsca: serializar política chromium: %w", err)
	}
	chromData = append(chromData, '\n')

	for _, dir := range chromiumPolicyDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("tlsca: criar diretório de política %s: %w", dir, err)
		}
		target := filepath.Join(dir, chromiumPolicyFileName)
		if err := os.WriteFile(target, chromData, 0o644); err != nil {
			return fmt.Errorf("tlsca: gravar política %s: %w", target, err)
		}
	}

	for _, dir := range firefoxPolicyDirs {
		if err := c.installFirefoxPolicy(dir, dst); err != nil {
			return err
		}
	}

	return nil
}

func (c *CA) installFirefoxPolicy(dir, certPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tlsca: criar diretório %s: %w", dir, err)
	}
	target := filepath.Join(dir, firefoxPolicyFileName)
	root := make(map[string]any)
	if data, err := os.ReadFile(target); err == nil {
		_ = json.Unmarshal(data, &root)
	}
	policies, _ := root["policies"].(map[string]any)
	if policies == nil {
		policies = make(map[string]any)
		root["policies"] = policies
	}
	certs, _ := policies["Certificates"].(map[string]any)
	if certs == nil {
		certs = make(map[string]any)
		policies["Certificates"] = certs
	}
	var installList []string
	if rawList, ok := certs["Install"].([]any); ok {
		for _, item := range rawList {
			if s, ok := item.(string); ok && s != certPath {
				installList = append(installList, s)
			}
		}
	}
	installList = append(installList, certPath)
	certs["Install"] = installList

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("tlsca: serializar política firefox: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("tlsca: gravar %s: %w", target, err)
	}
	return nil
}

func (c *CA) removeFirefoxPolicy(dir, certPath string) error {
	target := filepath.Join(dir, firefoxPolicyFileName)
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	policies, ok := root["policies"].(map[string]any)
	if !ok {
		return nil
	}
	certs, ok := policies["Certificates"].(map[string]any)
	if !ok {
		return nil
	}
	var newInstall []string
	if rawList, ok := certs["Install"].([]any); ok {
		for _, item := range rawList {
			if s, ok := item.(string); ok && s != certPath {
				newInstall = append(newInstall, s)
			}
		}
	}
	if len(newInstall) > 0 {
		certs["Install"] = newInstall
	} else {
		delete(certs, "Install")
		if len(certs) == 0 {
			delete(policies, "Certificates")
		}
	}

	if len(policies) == 0 {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	outData, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	outData = append(outData, '\n')
	return os.WriteFile(target, outData, 0o644)
}

// removeFromStore remove o arquivo da CA e re-roda update-ca-certificates
// (--fresh descarta os symlinks órfãos do store antigo), além de remover
// os arquivos de política dos navegadores Chromium e desregistrar a CA do Firefox.
func (c *CA) removeFromStore(run StoreRunner) error {
	dst := filepath.Join(caCertsDir, storeFileName)
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tlsca: remover %s: %w", dst, err)
	}
	out, err := run("update-ca-certificates", "--fresh")
	if err != nil {
		return fmt.Errorf("tlsca: update-ca-certificates --fresh falhou: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	for _, dir := range chromiumPolicyDirs {
		target := filepath.Join(dir, chromiumPolicyFileName)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("tlsca: remover %s: %w", target, err)
		}
	}

	for _, dir := range firefoxPolicyDirs {
		if err := c.removeFirefoxPolicy(dir, dst); err != nil {
			return fmt.Errorf("tlsca: remover política firefox em %s: %w", dir, err)
		}
	}

	return nil
}

// IsInStore detecta a CA no trust store do Linux e nos navegadores:
//  1. A prova real do SO é a CÓPIA instalada em storeInstalledDir (o update-ca-certificates
//     copia a âncora local para lá) com DER idêntico.
//  2. Os navegadores baseados em Chromium (Brave, Chrome, Chromium, Edge) devem possuir
//     a política enterprise CACertificates configurada com o DER da CA.
//  3. O Firefox deve possuir a política corporativa Certificates.Install configurada.
func (c *CA) IsInStore(run StoreRunner) (bool, error) {
	// 1. Verifica no sistema operacional (cópia instalada pelo update-ca-certificates)
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
	if !bytes.Equal(crt.Raw, c.crt.Raw) {
		return false, nil
	}

	// 2. Verifica políticas do Chromium/Brave/Chrome/Edge
	b64Cert := base64.StdEncoding.EncodeToString(c.crt.Raw)
	for _, dir := range chromiumPolicyDirs {
		target := filepath.Join(dir, chromiumPolicyFileName)
		pData, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		var pol chromiumPolicyDoc
		if err := json.Unmarshal(pData, &pol); err != nil {
			return false, nil
		}
		found := false
		for _, item := range pol.CACertificates {
			if item == b64Cert {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}

	// 3. Verifica política do Firefox
	certPath := filepath.Join(caCertsDir, storeFileName)
	for _, dir := range firefoxPolicyDirs {
		target := filepath.Join(dir, firefoxPolicyFileName)
		pData, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		var root map[string]any
		if err := json.Unmarshal(pData, &root); err != nil {
			return false, nil
		}
		policies, _ := root["policies"].(map[string]any)
		if policies == nil {
			return false, nil
		}
		certs, _ := policies["Certificates"].(map[string]any)
		if certs == nil {
			return false, nil
		}
		list, _ := certs["Install"].([]any)
		found := false
		for _, item := range list {
			if s, ok := item.(string); ok && s == certPath {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}

	return true, nil
}
