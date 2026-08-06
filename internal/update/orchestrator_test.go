package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeUpdaterAPI é o fake do UpdaterAPI para a orquestração — não toca em
// rede nem em binários reais.
type fakeUpdaterAPI struct {
	result       *UpdateResult
	checkErr     error
	applyErr     error
	applyCalls   int32
	appliedTo    []string
	channel      string
	cleanupCalls int32
}

func (f *fakeUpdaterAPI) CheckForUpdate(_ context.Context) (*UpdateResult, error) {
	return f.result, f.checkErr
}

func (f *fakeUpdaterAPI) SetChannel(channel string) { f.channel = channel }

func (f *fakeUpdaterAPI) CleanupStale(_ string) { atomic.AddInt32(&f.cleanupCalls, 1) }

func (f *fakeUpdaterAPI) UpdateToAll(_ context.Context, _ *UpdateResult, binaries []string) ([]string, error) {
	atomic.AddInt32(&f.applyCalls, 1)
	f.appliedTo = append([]string(nil), binaries...)
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	backups := make([]string, 0, len(binaries))
	for _, b := range binaries {
		backups = append(backups, b+".bak")
	}
	return backups, nil
}

// newTestOrchestrator monta um Orchestrator com o preparo do swap stubbado
// (contador) e a flag em disco (a mesma que o daemon usa).
func newTestOrchestrator(t *testing.T, u UpdaterAPI, binaries []string) (*Orchestrator, *int32) {
	t.Helper()
	var swapCalls int32
	o := NewOrchestrator(u, binaries, "1.0.0")
	o.StopForSwap = func(_ []string) func() {
		atomic.AddInt32(&swapCalls, 1)
		return func() {}
	}
	return o, &swapCalls
}

func TestOrchestrator_Check_NoUpdate(t *testing.T) {
	o, _ := newTestOrchestrator(t, &fakeUpdaterAPI{}, []string{"/tmp/focusguard-daemon"})

	st, err := o.Check(context.Background(), false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Available || st.Applied || st.PendingReboot {
		t.Errorf("esperava status vazio, got %+v", st)
	}
	if st.CurrentVersion != "1.0.0" {
		t.Errorf("CurrentVersion deve vir do Version configurado, got %q", st.CurrentVersion)
	}
}

func TestOrchestrator_Check_UpdateAvailable(t *testing.T) {
	fake := &fakeUpdaterAPI{result: &UpdateResult{Version: "1.1.0"}}
	o, _ := newTestOrchestrator(t, fake, []string{"/tmp/focusguard-daemon"})

	st, err := o.Check(context.Background(), false, "beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Available || st.NewVersion != "1.1.0" {
		t.Errorf("esperava update 1.1.0, got %+v", st)
	}
	if st.Applied {
		t.Error("Applied deve ser false sem apply")
	}
	if fake.channel != "beta" {
		t.Errorf("canal não repassado ao updater: %q", fake.channel)
	}
	if atomic.LoadInt32(&fake.applyCalls) != 0 {
		t.Error("UpdateToAll não deveria rodar sem apply")
	}
}

func TestOrchestrator_Check_CheckError(t *testing.T) {
	o, _ := newTestOrchestrator(t, &fakeUpdaterAPI{checkErr: errors.New("network down")}, nil)

	_, err := o.Check(context.Background(), false, "")
	if err == nil || err.Error() != "network down" {
		t.Fatalf("esperava erro de rede, got %v", err)
	}
}

func TestOrchestrator_Check_NilUpdater(t *testing.T) {
	o, _ := newTestOrchestrator(t, nil, []string{"/tmp/focusguard-daemon"})

	st, err := o.Check(context.Background(), false, "")
	if err != nil {
		t.Fatalf("updater nil não é erro: %v", err)
	}
	if st.Available {
		t.Error("sem updater não há update")
	}
}

// TestOrchestrator_Check_Apply verifica o caminho feliz do apply: flag escrita
// ANTES da troca (e MANTIDA — só o boot saudável da nova versão a remove),
// preparo do swap chamado, UpdateToAll + CleanupStale rodados e Applied=true.
func TestOrchestrator_Check_Apply(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{result: &UpdateResult{Version: "1.1.0"}}
	o, swapCalls := newTestOrchestrator(t, fake, []string{daemon})

	st, err := o.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Applied {
		t.Error("esperava Applied=true")
	}
	if atomic.LoadInt32(&fake.applyCalls) != 1 {
		t.Errorf("esperava 1 UpdateToAll, got %d", fake.applyCalls)
	}
	if atomic.LoadInt32(&fake.cleanupCalls) != 1 {
		t.Errorf("esperava 1 CleanupStale após apply, got %d", fake.cleanupCalls)
	}
	if atomic.LoadInt32(swapCalls) != 1 {
		t.Errorf("esperava 1 preparo do swap, got %d", atomic.LoadInt32(swapCalls))
	}
	// Bug 2: a flag permanece até o boot saudável da nova versão.
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); err != nil {
		t.Error("update.inprogress deve existir após o apply (só o boot saudável a remove)")
	}
	if len(fake.appliedTo) != 1 || fake.appliedTo[0] != daemon {
		t.Errorf("UpdateToAll recebeu %v, esperava [%s]", fake.appliedTo, daemon)
	}
}

// TestOrchestrator_Check_ApplyError verifica o caminho de erro: Applied=false,
// sem CleanupStale, e a flag REMOVIDA (o daemon segue rodando a versão antiga
// — a flag não pode ficar para trás senão o watchdog ficaria mudo à toa).
func TestOrchestrator_Check_ApplyError(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{result: &UpdateResult{Version: "1.1.0"}, applyErr: errors.New("file locked")}
	o, _ := newTestOrchestrator(t, fake, []string{daemon})

	_, err := o.Check(context.Background(), true, "")
	if err == nil || err.Error() != "falha ao aplicar atualização: file locked" {
		t.Fatalf("esperava erro de apply, got %v", err)
	}
	if atomic.LoadInt32(&fake.cleanupCalls) != 0 {
		t.Errorf("sem apply não há varredura, got %d", fake.cleanupCalls)
	}
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); !os.IsNotExist(err) {
		t.Error("update.inprogress deve ser removida quando o apply falha")
	}
}

// TestOrchestrator_Check_PendingReboot verifica o fallback move-on-reboot:
// Applied=false, flag removida, PendingReboot=true, sem CleanupStale, e o
// preparo do swap ainda roda antes do UpdateToAll.
func TestOrchestrator_Check_PendingReboot(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "focusguard-daemon")
	fake := &fakeUpdaterAPI{
		result:   &UpdateResult{Version: "1.1.0"},
		applyErr: ErrScheduledOnReboot,
	}
	o, swapCalls := newTestOrchestrator(t, fake, []string{daemon})

	st, err := o.Check(context.Background(), true, "")
	if err != nil {
		t.Fatalf("ErrScheduledOnReboot não é erro para o caller: %v", err)
	}
	if st.Applied {
		t.Error("Applied deve ser false no fallback move-on-reboot")
	}
	if !st.PendingReboot {
		t.Error("PendingReboot deve ser true")
	}
	if atomic.LoadInt32(&fake.cleanupCalls) != 0 {
		t.Errorf("sem apply não há varredura, got %d", fake.cleanupCalls)
	}
	if atomic.LoadInt32(swapCalls) != 1 {
		t.Errorf("preparo do swap deve rodar antes do swap, got %d", atomic.LoadInt32(swapCalls))
	}
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); !os.IsNotExist(err) {
		t.Error("update.inprogress deve ser removida no caminho PendingReboot")
	}
}

// TestOrchestrator_Apply_FlagHelpers verifica os helpers de flag diretamente
// (semântica do Bug 2: remover flag inexistente é no-op).
func TestOrchestrator_Apply_FlagHelpers(t *testing.T) {
	dir := t.TempDir()

	MarkInProgress(dir)
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); err != nil {
		t.Fatalf("flag deve existir após MarkInProgress: %v", err)
	}

	ClearInProgress(dir)
	ClearInProgress(dir) // no-op
	if _, err := os.Stat(filepath.Join(dir, InProgressFileName)); !os.IsNotExist(err) {
		t.Error("flag deve ser removida por ClearInProgress")
	}
}
