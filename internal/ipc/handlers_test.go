package ipc

import (
	"testing"
)

// TestServer_RegisteredHandlersHaveSpecs é o fechamento registry→specs do
// servidor real: todo handler registrado pelo NewServer precisa de ActionSpec
// (drift viraria 403 silencioso no web). A direção inversa (todo spec tem
// handler) é encerrada quando o switch legado esvaziar (Fase 4).
func TestServer_RegisteredHandlersHaveSpecs(t *testing.T) {
	s := setupTestServer(t)
	if err := s.registry.ValidateSpecs(); err != nil {
		t.Fatalf("registry do servidor fora do fechamento: %v", err)
	}
	if len(s.registry.Actions()) == 0 {
		t.Fatal("esperava handlers registrados no servidor")
	}
}

// TestServer_RegistryDispatchesMigratedActions garante que as ações migradas
// são atendidas pelo registry (roteador) com o mesmo comportamento — o
// dispatchLegacy não deve mais vê-las. Cobertura pontual sobre a suíte
// existente de preset/apps/tamper/goal (que exercita o caminho completo).
func TestServer_RegistryDispatchesMigratedActions(t *testing.T) {
	s := setupTestServer(t)

	migrated := []string{
		"ping", "presets", "preset-add", "preset-remove",
		"apps-list", "apps-add", "apps-remove",
		"tamper-log", "goal-get", "goal-set",
	}
	for _, action := range migrated {
		if _, ok := s.registry.Get(action); !ok {
			t.Errorf("ação migrada %q não está no registry (o switch legado ainda a atende?)", action)
		}
	}
}
