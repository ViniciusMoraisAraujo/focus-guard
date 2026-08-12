package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"focusguard/internal/infrastructure/tlsca"
)

// caDirPath localiza o diretório da CA local (ao lado do state.json — mesmo
// caminho que o daemon usa para gerar/instalar a CA).
func caDirPath() string {
	return filepath.Join(filepath.Dir(stateFilePath()), "ca")
}

// errCARequiresElevation é o erro devolvido quando o ca-install/ca-uninstall
// roda sem privilégio — a elevação é a precondição checada ANTES de qualquer
// efeito colateral (gerar a CA, tocar o trust store).
var errCARequiresElevation = errors.New("execute com privilégio de administrador/root")

// isElevatedCheck é a checagem de elevação usada pelos comandos ca-* — var
// (padrão execOutput/osExit do pacote) para os testes simularem os dois
// sentidos sem o SO; em produção aponta para isElevated (por plataforma).
var isElevatedCheck = isElevated

// caInstallRuns executa o ca-install com um diretório de CA injetável (o
// handler passa caDirPath(); o teste, um temp dir). A elevação é verificada
// PRIMEIRO: um ca-install não elevado aborta sem gerar a CA — pendência INFO
// do docs/verification-plan.md (antes, a CA era gerada e a mensagem de erro
// só vinha depois, no write do trust store). Com elevação, a CA é obtida
// (gerando se ainda não existir — idempotente) e instalada no trust store.
func caInstallRuns(dir string) error {
	if !isElevatedCheck() {
		return errCARequiresElevation
	}
	ca, err := tlsca.LoadOrCreate(dir)
	if err != nil {
		return fmt.Errorf("não foi possível obter a CA local: %w", err)
	}
	if err := ca.InstallIntoStore(tlsca.DefaultStoreRunner()); err != nil {
		return fmt.Errorf("falha ao instalar a CA no trust store: %w", err)
	}
	return nil
}

// handleCAInstallCommand instala a CA local no trust store do SO — o passo que
// faz a página de bloqueio HTTPS abrir sem o aviso de "conexão não segura".
// Requer shell elevado (administrador/root): o certutil/update-ca-certificates
// falham sem ele. A CA é gerada se ainda não existir (idempotente).
func handleCAInstallCommand() {
	err := caInstallRuns(caDirPath())
	if errors.Is(err, errCARequiresElevation) {
		fmt.Println("⚠ Execute com privilégio de administrador/root para instalar a CA no trust store.")
		fmt.Println("  Windows: rode o prompt como administrador.  Linux: sudo.")
		osExit(1)
	}
	if err != nil {
		fmt.Printf("Erro: %v\n", err)
		osExit(1)
	}
	fmt.Println("✔ CA do FocusGuard instalada no trust store do sistema.")
	fmt.Println("  A partir de agora, a página de bloqueio HTTPS abre sem aviso de certificado")
	fmt.Println("  em Chrome/Edge. O Firefox usa trust store próprio: ative")
	fmt.Println("  'security.enterprise_roots.enabled' em about:config para herdar a confiança.")
}

// handleCAUninstallCommand remove a CA local do trust store do SO (o uninstall
// do FocusGuard deve ser acompanhado deste comando para não deixar a âncora de
// confiança órfã). Requer elevação — checada antes de qualquer efeito (um
// ca-uninstall não elevado nem carrega/gera a CA); a CA ausente é um no-op.
func handleCAUninstallCommand() {
	if !isElevatedCheck() {
		fmt.Println("⚠ Execute com privilégio de administrador/root para remover a CA do trust store.")
		osExit(1)
	}
	// Sem CA persistida, não há o que remover — e nem o que GERAR: o uninstall
	// nunca cria a âncora só para removê-la (mesmo espírito da elevação checada
	// antes no ca-install).
	if !tlsca.Exists(caDirPath()) {
		fmt.Println("ℹ CA local não encontrada — nada a remover.")
		return
	}
	ca, err := tlsca.LoadOrCreate(caDirPath())
	if err != nil {
		fmt.Printf("Erro: não foi possível obter a CA local: %v\n", err)
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
