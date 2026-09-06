//go:build linux

package tlsca

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	commands []string
	fail     bool
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.fail {
		return []byte("erro"), os.ErrPermission
	}
	return nil, nil
}

// useTempStoreDirs redireciona caCertsDir, storeInstalledDir, chromiumPolicyDirs
// e firefoxPolicyDirs para diretórios temporários (os testes nunca tocam no trust
// store nem nas políticas reais da máquina) e restaura os valores originais no fim.
func useTempStoreDirs(t *testing.T) {
	t.Helper()
	origCA, origInstalled := caCertsDir, storeInstalledDir
	origChromium, origFirefox := chromiumPolicyDirs, firefoxPolicyDirs

	tempBase := t.TempDir()
	caCertsDir = filepath.Join(tempBase, "ca-certificates")
	storeInstalledDir = filepath.Join(tempBase, "ssl/certs")
	chromiumPolicyDirs = []string{
		filepath.Join(tempBase, "brave/policies/managed"),
		filepath.Join(tempBase, "chrome/policies/managed"),
	}
	firefoxPolicyDirs = []string{
		filepath.Join(tempBase, "firefox/policies"),
	}

	if err := os.MkdirAll(caCertsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storeInstalledDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		caCertsDir, storeInstalledDir = origCA, origInstalled
		chromiumPolicyDirs, firefoxPolicyDirs = origChromium, origFirefox
	})
}

// simulateUpdateCACerts simula o efeito do update-ca-certificates: a âncora
// local (caCertsDir) é copiada para o diretório instalado (storeInstalledDir)
// — o fakeRunner não roda o comando real.
func simulateUpdateCACerts(t *testing.T, ca *CA) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallIntoStore_Linux(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	f := &fakeRunner{}
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("InstallIntoStore: %v", err)
	}

	// 1. Arquivo copiado para ca-certs dir + update-ca-certificates executado.
	if _, err := os.Stat(filepath.Join(caCertsDir, storeFileName)); err != nil {
		t.Errorf("CA não copiada para %s: %v", caCertsDir, err)
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "update-ca-certificates") {
		t.Errorf("comandos = %v, want update-ca-certificates", f.commands)
	}

	// 2. Arquivos de política Chromium criados e válidos.
	b64Expected := base64.StdEncoding.EncodeToString(ca.crt.Raw)
	for _, dir := range chromiumPolicyDirs {
		pPath := filepath.Join(dir, chromiumPolicyFileName)
		data, err := os.ReadFile(pPath)
		if err != nil {
			t.Fatalf("política chromium ausente em %s: %v", pPath, err)
		}
		var doc chromiumPolicyDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("JSON inválido em %s: %v", pPath, err)
		}
		if len(doc.CACertificates) != 1 || doc.CACertificates[0] != b64Expected {
			t.Errorf("conteúdo da política chromium incorreto em %s: got %v", pPath, doc.CACertificates)
		}
	}

	// 3. Arquivo de política Firefox criado e válido.
	expectedCertPath := filepath.Join(caCertsDir, storeFileName)
	for _, dir := range firefoxPolicyDirs {
		fPath := filepath.Join(dir, firefoxPolicyFileName)
		data, err := os.ReadFile(fPath)
		if err != nil {
			t.Fatalf("política firefox ausente em %s: %v", fPath, err)
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			t.Fatalf("JSON inválido em %s: %v", fPath, err)
		}
		policies := root["policies"].(map[string]any)
		certs := policies["Certificates"].(map[string]any)
		list := certs["Install"].([]any)
		if len(list) != 1 || list[0] != expectedCertPath {
			t.Errorf("conteúdo da política firefox incorreto em %s: got %v", fPath, list)
		}
	}

	// 4. Idempotente: com a CA instalada de verdade (cópia em storeInstalledDir),
	// a segunda chamada é no-op.
	simulateUpdateCACerts(t, ca)
	n := len(f.commands)
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("segunda chamada InstallIntoStore: %v", err)
	}
	if len(f.commands) != n {
		t.Errorf("InstallIntoStore idempotente deveria ser no-op, comandos = %v", f.commands)
	}
}

func TestInstallIntoStore_LinuxError(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	f := &fakeRunner{fail: true}
	if err := ca.InstallIntoStore(f.run); err == nil {
		t.Error("update-ca-certificates falhando deveria propagar erro")
	}
}

func TestIsInStore_Linux_InstalledCopy(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	run := func(string, ...string) ([]byte, error) { return nil, nil }

	// Nada instalado → false.
	got, err := ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("sem nada no store, IsInStore deveria ser false")
	}

	// Só o arquivo-fonte (update-ca-certificates ainda não rodou / cópia ausente) → false.
	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("arquivo-fonte sem a cópia instalada NÃO deveria contar como instalado")
	}

	// Cópia instalada no SO + políticas do navegador → true.
	simulateUpdateCACerts(t, ca)
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("cópia instalada no SO e políticas de navegadores presentes deveriam retornar true")
	}

	// Se faltar a política de um navegador Chromium → false.
	chromFile := filepath.Join(chromiumPolicyDirs[0], chromiumPolicyFileName)
	_ = os.Remove(chromFile)
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("sem a política do Chromium/Brave, IsInStore deveria retornar false")
	}
}

func TestIsInStore_Linux_ForeignOrStaleCert(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	run := func(string, ...string) ([]byte, error) { return nil, nil }

	// 1. Outra CA com o mesmo nome em storeInstalledDir.
	other := newTestCA(t)
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), other.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("cópia de OUTRA CA no SO não deveria contar como instalada")
	}

	// 2. Arquivo não-PEM no SO.
	if err := os.WriteFile(filepath.Join(storeInstalledDir, storeInstalledName), []byte("invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("arquivo não-PEM no SO não deveria contar como instalado")
	}

	// 3. SO ok, mas política Chromium aponta para outra CA (regenerada).
	simulateUpdateCACerts(t, ca)
	for _, dir := range chromiumPolicyDirs {
		_ = os.MkdirAll(dir, 0o755)
		staleDoc := chromiumPolicyDoc{
			CACertificates: []string{base64.StdEncoding.EncodeToString(other.crt.Raw)},
		}
		data, _ := json.Marshal(staleDoc)
		_ = os.WriteFile(filepath.Join(dir, chromiumPolicyFileName), data, 0o644)
	}
	for _, dir := range firefoxPolicyDirs {
		_ = os.MkdirAll(dir, 0o755)
		_ = ca.installFirefoxPolicy(dir, filepath.Join(caCertsDir, storeFileName))
	}

	got, err = ca.IsInStore(run)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("política Chromium com certificado de OUTRA CA deveria retornar false")
	}
}

func TestRemoveFromStore_Linux(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)
	f := &fakeRunner{}

	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore (ausente): %v", err)
	}
	if len(f.commands) != 0 {
		t.Errorf("RemoveFromStore (ausente) deveria ser no-op, comandos = %v", f.commands)
	}

	_ = ca.InstallIntoStore((&fakeRunner{}).run)
	simulateUpdateCACerts(t, ca)

	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore (presente): %v", err)
	}

	// Arquivo removido de caCertsDir.
	if _, err := os.Stat(filepath.Join(caCertsDir, storeFileName)); !os.IsNotExist(err) {
		t.Error("arquivo da CA deveria ter sido removido do ca-certs dir")
	}

	// update-ca-certificates --fresh rodado.
	if !strings.Contains(f.commands[len(f.commands)-1], "--fresh") {
		t.Errorf("comandos = %v, want update-ca-certificates --fresh", f.commands)
	}

	// Políticas Chromium removidas.
	for _, dir := range chromiumPolicyDirs {
		target := filepath.Join(dir, chromiumPolicyFileName)
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("política chromium não foi removida de %s", target)
		}
	}

	// Políticas Firefox limpas.
	for _, dir := range firefoxPolicyDirs {
		target := filepath.Join(dir, firefoxPolicyFileName)
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("política firefox deveria ter sido removida se vazia em %s", target)
		}
	}
}

func TestFirefoxPolicy_PreservesExistingPolicies(t *testing.T) {
	useTempStoreDirs(t)
	ca := newTestCA(t)

	for _, dir := range firefoxPolicyDirs {
		_ = os.MkdirAll(dir, 0o755)
		initial := map[string]any{
			"policies": map[string]any{
				"DisableAppUpdate": true,
				"Certificates": map[string]any{
					"Install": []any{"/opt/custom-corp-ca.crt"},
				},
			},
		}
		data, _ := json.Marshal(initial)
		_ = os.WriteFile(filepath.Join(dir, firefoxPolicyFileName), data, 0o644)
	}

	// Instala nossa CA
	f := &fakeRunner{}
	if err := ca.InstallIntoStore(f.run); err != nil {
		t.Fatalf("InstallIntoStore: %v", err)
	}

	// Verifica que DisableAppUpdate e a CA corporativa foram preservados
	ourCertPath := filepath.Join(caCertsDir, storeFileName)
	for _, dir := range firefoxPolicyDirs {
		target := filepath.Join(dir, firefoxPolicyFileName)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		var root map[string]any
		_ = json.Unmarshal(data, &root)
		policies := root["policies"].(map[string]any)
		if policies["DisableAppUpdate"] != true {
			t.Error("DisableAppUpdate deveria ter sido preservado")
		}
		certs := policies["Certificates"].(map[string]any)
		list := certs["Install"].([]any)
		if len(list) != 2 {
			t.Fatalf("esperava 2 certificados instalados, obteve %d", len(list))
		}
		if list[0] != "/opt/custom-corp-ca.crt" || list[1] != ourCertPath {
			t.Errorf("lista de certificados incorreta: %v", list)
		}
	}

	// Remove nossa CA
	simulateUpdateCACerts(t, ca)
	if err := ca.RemoveFromStore(f.run); err != nil {
		t.Fatalf("RemoveFromStore: %v", err)
	}

	// policies.json NÃO deve ter sido deletado, pois ainda tem DisableAppUpdate e custom-corp-ca
	for _, dir := range firefoxPolicyDirs {
		target := filepath.Join(dir, firefoxPolicyFileName)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("policies.json não deveria ter sido removido pois tinha outras regras: %v", err)
		}
		var root map[string]any
		_ = json.Unmarshal(data, &root)
		policies := root["policies"].(map[string]any)
		if policies["DisableAppUpdate"] != true {
			t.Error("DisableAppUpdate deveria ter permanecido")
		}
		certs := policies["Certificates"].(map[string]any)
		list := certs["Install"].([]any)
		if len(list) != 1 || list[0] != "/opt/custom-corp-ca.crt" {
			t.Errorf("esperava apenas custom-corp-ca na lista, obteve %v", list)
		}
	}
}
