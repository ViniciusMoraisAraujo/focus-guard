package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// InProgressFileName é a flag do Bug 2: escrita ANTES do UpdateToAll e removida
// apenas quando a NOVA versão do daemon conclui um boot saudável. Ela precisa
// sobreviver ao restart do próprio daemon, então não pode ser removida logo
// após o update — é o sinal que mantém o watchdog de fora durante a troca dos
// binários e o restart via SCM.
const InProgressFileName = "update.inprogress"

// MarkInProgress sinaliza ao watchdog que um update está em andamento e que a
// ausência do daemon é intencional. Best-effort: se a escrita falhar o update
// ainda prossegue (o watchdog apenas volta a interferir).
func MarkInProgress(installDir string) {
	p := filepath.Join(installDir, InProgressFileName)
	if err := os.WriteFile(p, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		log.Printf("[FocusGuard Daemon] Aviso: não foi possível criar %s: %v", p, err)
	}
}

// ClearInProgress remove a flag do Bug 2. Chamada no boot saudável da nova
// versão e no caminho de erro (update falhou → daemon segue rodando).
func ClearInProgress(installDir string) {
	_ = os.Remove(filepath.Join(installDir, InProgressFileName))
}

// UpdaterAPI é a superfície que a orquestração de update exige do updater
// (CheckForUpdate/UpdateToAll/SetChannel/CleanupStale). O daemon a satisfaz
// via *Updater ou um fake nos testes.
type UpdaterAPI interface {
	CheckForUpdate(ctx context.Context) (*UpdateResult, error)
	UpdateToAll(ctx context.Context, result *UpdateResult, binaries []string) ([]string, error)
	SetChannel(channel string)
	// CleanupStale varre a pasta de instalação de artefatos de updates
	// passados (.old/.trash órfãos e .bak antigos), mantendo só o .bak mais
	// novo por binário (o watchdog ainda precisa dele para o smart recovery).
	CleanupStale(installDir string)
}

// StopForSwap prepara a troca dos binários (para o watchdog/tray no Windows
// que segurariam o .exe) e devolve um restore que religa o que foi parado.
// Injetado (campo) para os testes não tocarem em serviços/processos reais.
type StopForSwap func(binaries []string) func()

// Orchestrator implementa a orquestração de update do daemon (B4): seleção de
// canal, checagem de release e — quando aplicável — a aplicação na SUITE
// inteira com a coreografia completa:
//
//   - Bug 1: atualiza o conjunto de binários (daemon + CLI + tray + watchdog +
//     web), não só o daemon — um update parcial quebraria o protocolo IPC. O
//     rollback é atômico dentro do UpdateToAll; qualquer falha mantém
//     Applied=false (o daemon não reinicia para um estado meio-atualizado).
//   - Bug 2: a flag update.inprogress é escrita ANTES da troca e só a nova
//     versão a remove após um boot saudável — sem ela o watchdog vê o daemon
//     fora do ar e o mata (ou desfaz o update pelo smart recovery) no meio da
//     operação.
//   - Bug 3: no Windows, para o watchdog e o tray ANTES da troca para liberar
//     os .exe (o erro "Acesso negado" da task.md); o restore religa o
//     watchdog se ele estava rodando.
//   - Fallback move-on-reboot: quando o exe do próprio daemon fica travado, o
//     UpdateToAll agenda a troca para o próximo boot e devolve
//     ErrScheduledOnReboot — o Check NÃO marca Applied, remove a flag e
//     reporta PendingReboot (o daemon segue rodando a versão antiga).
//
// Vive no pacote update (e não no main do daemon) para ser testável sem shell
// elevado.
type Orchestrator struct {
	Updater  UpdaterAPI
	Binaries []string
	// Version é a versão atual do daemon (ex.: injetada via ldflags) — vira o
	// CurrentVersion do Status.
	Version     string
	StopForSwap StopForSwap

	// markInProgress/clearInProgress são stubbable nos testes (default:
	// MarkInProgress/ClearInProgress).
	markInProgress  func(installDir string)
	clearInProgress func(installDir string)
}

// NewOrchestrator constrói a orquestração com os padrões de produção (flag em
// disco + preparo real do swap).
func NewOrchestrator(u UpdaterAPI, binaries []string, version string) *Orchestrator {
	return &Orchestrator{
		Updater:         u,
		Binaries:        binaries,
		Version:         version,
		StopForSwap:     StopForBinarySwap,
		markInProgress:  MarkInProgress,
		clearInProgress: ClearInProgress,
	}
}

// Check executa o fluxo completo de update (o antigo daemonUpdater.Check do
// cmd/focusguard-daemon, comportamento 1:1). Satisfaz update.Checker: o
// CurrentVersion vem do Version configurado.
func (o *Orchestrator) Check(ctx context.Context, apply bool, channel string) (Status, error) {
	if o == nil || o.Updater == nil {
		return Status{}, nil
	}
	st := Status{CurrentVersion: o.Version}

	// Canal de release por request: "beta" opta por prereleases; o padrão
	// ("" / "stable") as ignora. O updater é compartilhado entre as conexões
	// IPC (cada uma em sua goroutine): SetChannel é atômico e a checagem usa
	// um snapshot consistente — em flips rápidos prevalece o último canal
	// escrito (last-writer-wins), semântica benigna e livre de data race.
	o.Updater.SetChannel(channel)

	res, err := o.Updater.CheckForUpdate(ctx)
	if err != nil {
		return st, err
	}
	if res == nil {
		return st, nil
	}

	st.Available = true
	st.NewVersion = res.Version
	if !apply {
		return st, nil
	}

	// Bug 2: a flag é escrita ANTES de trocar os binários e só a nova versão
	// a remove após um boot saudável. Sem ela, o watchdog vê o daemon fora do
	// ar durante a troca/restart e o mata (ou desfaz o update pelo smart
	// recovery) no meio da operação.
	var installDir string
	if len(o.Binaries) > 0 {
		installDir = filepath.Dir(o.Binaries[0])
		o.markInProgress(installDir)
	}
	// Bug 3 (task.md): no Windows, para o watchdog e o tray ANTES da troca
	// para liberar os .exe — um binário em execução fica travado para rename
	// (o erro "Acesso negado" da task.md). O restore religa o watchdog se ele
	// estava rodando. No Linux é um no-op.
	restoreGuards := o.StopForSwap(o.Binaries)
	defer restoreGuards()

	if _, err := o.Updater.UpdateToAll(ctx, res, o.Binaries); err != nil {
		if errors.Is(err, ErrScheduledOnReboot) {
			// Fallback move-on-reboot: o daemon segue rodando a versão antiga
			// e a troca completa no próximo boot — a flag não pode ficar
			// (senão o watchdog ficaria mudo à toa) e o daemon NÃO reinicia.
			// O watchdog é religado pelo defer do restoreGuards.
			if installDir != "" {
				o.clearInProgress(installDir)
			}
			st.PendingReboot = true
			return st, nil
		}
		// Update falhou: o daemon segue rodando a versão antiga — a flag não
		// pode ficar para trás, senão o watchdog ficaria mudo à toa.
		if installDir != "" {
			o.clearInProgress(installDir)
		}
		return st, fmt.Errorf("falha ao aplicar atualização: %w", err)
	}
	st.Applied = true
	// Bug 1: o UpdateToAll devolve os backups criados, mas sem esta varredura
	// os .bak.<timestamp> se acumulavam para sempre (um .bak por binário a
	// cada update). CleanupStale mantém só o mais novo por binário — o
	// watchdog ainda precisa dele caso a nova versão crash-loope antes de
	// confirmar saúde.
	if installDir != "" {
		o.Updater.CleanupStale(installDir)
	}
	return st, nil
}
