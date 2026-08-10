package scheduler

// Bug-hunt ao vivo ("o YouTube não voltou"): um bloqueio que expira pelo timer
// (time.AfterFunc → onExpire) precisa limpar TODOS os mecanismos — não só a
// RAM. Este teste trava a cadeia completa de limpeza para o último bloco:
//   1. hosts + firewall por IP → UnblockDomain com os MESMOS IPs aplicados;
//   2. regras DoH → UnblockDoH quando era o último bloco;
//   3. estado → state.json sem o domínio;
//   4. sinkhole DNS → IsBlocked(domain) false (voltaria a responder).

import (
	"slices"
	"testing"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/infrastructure/store"
)

func TestScheduler_TimerExpiration_CleansAllMechanisms(t *testing.T) {
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) {
		return []string{"1.2.3.4", "5.6.7.8"}, nil
	}
	t.Cleanup(func() { resolveFunc = origResolve })

	sched, enf, st := setupTestScheduler(t)

	domain := "youtube.com"
	// Janela generosa o bastante para o assert imediato de IsBlocked nunca
	// competir com o timer (o AfterFunc dispara só em 500ms).
	if _, err := sched.Block(domain, 500*time.Millisecond); err != nil {
		t.Fatalf("Block: %v", err)
	}

	// Enquanto ativo: o sinkhole DNS responderia "bloqueado" para o domínio.
	if !sched.IsBlocked(domain) {
		t.Fatal("IsBlocked deveria ser true com o bloco ativo")
	}
	// O primeiro bloco liga o DoH (regras de bloqueio de DNS-over-HTTPS).
	enf.mu.Lock()
	blockDoH := enf.blockDoHCalls
	enf.mu.Unlock()
	if blockDoH != 1 {
		t.Errorf("BlockDoH chamado %d vezes no block, want 1", blockDoH)
	}

	// Sinal de conclusão: o UnblockDoH é a ÚLTIMA operação do onExpire (depois
	// do UnblockDomain e da gravação do state.json) — esperá-lo garante que a
	// cadeia inteira terminou antes das asserções.
	done := waitForCondition(2*time.Second, func() bool {
		enf.mu.Lock()
		defer enf.mu.Unlock()
		return enf.unblockDoHCalls >= 1
	})
	if !done {
		t.Fatal("limpeza do último bloco não completou em 2s")
	}

	// 1. hosts + firewall por IP: os MESMOS IPs aplicados no Block.
	enf.mu.Lock()
	gotIPs := enf.unblockedDomains[domain]
	unblockDoH := enf.unblockDoHCalls
	enf.mu.Unlock()
	if !slices.Equal(gotIPs, []string{"1.2.3.4", "5.6.7.8"}) {
		t.Errorf("UnblockDomain IPs = %v, want [1.2.3.4 5.6.7.8] (mesmos IPs do block)", gotIPs)
	}

	// 2. DoH removido (era o último bloco → regras DoH não ficam para trás).
	if unblockDoH != 1 {
		t.Errorf("UnblockDoH chamado %d vezes, want 1 (último bloco expirou)", unblockDoH)
	}

	// 3. state.json limpo.
	state, err := st.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("state.json ainda tem %d blocos, want 0", len(state.Blocks))
	}

	// 4. RAM e sinkhole: sem bloqueios ativos e o domínio deixou de ser
	// bloqueado (o DNS voltaria a responder).
	if sched.HasActiveBlocks() {
		t.Error("HasActiveBlocks deveria ser false após expiração")
	}
	if sched.IsBlocked(domain) {
		t.Error("IsBlocked deveria ser false após expiração (DNS voltaria a responder)")
	}
}

// TestScheduler_LastExpiry_SweepsOrphanRules trava o fix da raça do refresh
// periódico (bug-hunt ao vivo "o sistema esquece de desbloquear?"): quando o
// ÚLTIMO bloco expira pelo timer, o scheduler precisa disparar um Sync com o
// conjunto vazio — a varredura que remove qualquer regra de domínio órfã que
// o refresh possa ter aplicado na janela da expiração (IP novo resolvido) —
// antes de desligar o DoH. Sem o Sync, uma regra aplicada depois do
// UnblockDomain ficaria para sempre no firewall com o estado "limpo".
func TestScheduler_LastExpiry_SweepsOrphanRules(t *testing.T) {
	origResolve := resolveFunc
	resolveFunc = func(string) ([]string, error) {
		return []string{"1.2.3.4"}, nil
	}
	t.Cleanup(func() { resolveFunc = origResolve })

	sched, enf, _ := setupTestScheduler(t)

	if _, err := sched.Block("youtube.com", 500*time.Millisecond); err != nil {
		t.Fatalf("Block: %v", err)
	}

	enf.mu.Lock()
	syncBefore := enf.syncCalls
	enf.mu.Unlock()

	// Sinal de conclusão: o PRÓPRIO Sync de varredura (não um operação
	// vizinha) — se uma refatoração futura o mover, o teste continua exigindo
	// que ele tenha rodado.
	done := waitForCondition(2*time.Second, func() bool {
		enf.mu.Lock()
		defer enf.mu.Unlock()
		return enf.syncCalls > syncBefore
	})
	if !done {
		t.Fatal("varredura do último bloco não rodou em 2s")
	}

	enf.mu.Lock()
	defer enf.mu.Unlock()
	if enf.syncCalls <= syncBefore {
		t.Errorf("esperava Sync de varredura na expiração do último bloco (before=%d after=%d)", syncBefore, enf.syncCalls)
	}
	if len(enf.syncedBlocks) != 0 {
		t.Errorf("Sync de varredura deveria receber o conjunto vazio (nenhuma regra esperada), got %v", enf.syncedBlocks)
	}
}

// TestScheduler_Reconcile_LastExpiry_SweepsOrphanRules é o mesmo fix pelo
// caminho do Reconcile (boot/tamper): sem blocos ativos no estado, a
// reconciliação também varre regras órfãs antes de desligar o DoH — cobre o
// caso de um IP novo do refresh aplicado na janela e um restart logo depois.
func TestScheduler_Reconcile_LastExpiry_SweepsOrphanRules(t *testing.T) {
	sched, enf, st := setupTestScheduler(t)

	now := time.Now().UTC().Round(time.Second)
	if err := st.Save(&store.State{Blocks: map[string]policy.Block{
		"expired.com": {
			Domain:      "expired.com",
			StartedAt:   now.Add(-48 * time.Hour),
			ExpiresAt:   now.Add(-24 * time.Hour),
			ResolvedIPs: []string{"1.1.1.1"},
		},
	}}); err != nil {
		t.Fatalf("prepare state: %v", err)
	}

	if err := sched.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	enf.mu.Lock()
	defer enf.mu.Unlock()
	if enf.syncCalls < 1 {
		t.Errorf("reconcile sem blocos ativos deveria varrer regras órfãs (Sync), got %d chamadas", enf.syncCalls)
	}
	if len(enf.syncedBlocks) != 0 {
		t.Errorf("Sync de varredura deveria receber o conjunto vazio, got %v", enf.syncedBlocks)
	}
}
