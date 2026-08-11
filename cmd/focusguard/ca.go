package main

import (
	"fmt"
	"path/filepath"

	"focusguard/internal/infrastructure/tlsca"
)

// caDirPath localiza o diretório da CA local (ao lado do state.json — mesmo
// caminho que o daemon usa para gerar/instalar a CA).
func caDirPath() string {
	return filepath.Join(filepath.Dir(stateFilePath()), "ca")
}

// handleCAInstallCommand instala a CA local no trust store do SO — o passo que
// faz a página de bloqueio HTTPS abrir sem o aviso de "conexão não segura".
// Requer shell elevado (administrador/root): o certutil/update-ca-certificates
// falham sem ele. A CA é gerada se ainda não existir (idempotente).
func handleCAInstallCommand() {
	ca, err := tlsca.LoadOrCreate(caDirPath())
	if err != nil {
		fmt.Printf("Erro: não foi possível obter a CA local: %v\n", err)
		osExit(1)
	}
	if !isElevated() {
		fmt.Println("⚠ Execute com privilégio de administrador/root para instalar a CA no trust store.")
		fmt.Println("  Windows: rode o prompt como administrador.  Linux: sudo.")
		osExit(1)
	}
	if err := ca.InstallIntoStore(tlsca.DefaultStoreRunner()); err != nil {
		fmt.Printf("Falha ao instalar a CA no trust store: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ CA do FocusGuard instalada no trust store do sistema.")
	fmt.Println("  A partir de agora, a página de bloqueio HTTPS abre sem aviso de certificado")
	fmt.Println("  em Chrome/Edge. O Firefox usa trust store próprio: ative")
	fmt.Println("  'security.enterprise_roots.enabled' em about:config para herdar a confiança.")
}

// handleCAUninstallCommand remove a CA local do trust store do SO (o uninstall
// do FocusGuard deve ser acompanhado deste comando para não deixar a âncora de
// confiança órfã). Requer elevação; a CA ausente é um no-op.
func handleCAUninstallCommand() {
	ca, err := tlsca.LoadOrCreate(caDirPath())
	if err != nil {
		fmt.Printf("Erro: não foi possível obter a CA local: %v\n", err)
		osExit(1)
	}
	if !isElevated() {
		fmt.Println("⚠ Execute com privilégio de administrador/root para remover a CA do trust store.")
		osExit(1)
	}
	if err := ca.RemoveFromStore(tlsca.DefaultStoreRunner()); err != nil {
		fmt.Printf("Falha ao remover a CA do trust store: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ CA do FocusGuard removida do trust store do sistema.")
	fmt.Println("  A página de bloqueio HTTPS volta a exibir o aviso de certificado (clique em 'Avançado → Continuar').")
	fmt.Println("  ⚠ O daemon reinstala a CA automaticamente no próximo boot enquanto a página de bloqueio estiver ativa")
	fmt.Println("    (é o comportamento do fix completo). Para manter a CA fora, desative a página (interceptor off)")
	fmt.Println("    ou remova a chave também: " + ca.CertPath() + " e o arquivo .key ao lado (ou rode o uninstall).")
}
