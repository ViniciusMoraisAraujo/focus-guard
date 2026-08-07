package ipc

import (
	"testing"
)

// TestServer_RegisteredHandlersHaveSpecs é o fechamento registry→specs do
// servidor real: todo handler registrado pelo NewServer precisa de ActionSpec
// (drift viraria 403 silencioso no web). user-verify é web-only (isenta), o
// resto não.
func TestServer_RegisteredHandlersHaveSpecs(t *testing.T) {
	s := setupTestServer(t)
	if err := s.registry.ValidateSpecs("user-verify"); err != nil {
		t.Fatalf("registry do servidor fora do fechamento: %v", err)
	}
	if len(s.registry.Actions()) == 0 {
		t.Fatal("esperava handlers registrados no servidor")
	}
}

// TestServer_SpecsAllHaveHandlers fecha a direção inversa do contrato
// registry↔specs: todo spec tem handler. Com o switch legado eliminado (Fase
// 4) a ausência de handler viraria 404 de roteador em vez de execução — um
// spec órfão é bug de boot, não surpresa em runtime.
func TestServer_SpecsAllHaveHandlers(t *testing.T) {
	s := setupTestServer(t)
	for _, action := range SpecActions() {
		if _, ok := s.registry.Get(action); !ok {
			t.Errorf("spec sem handler: %q (registre em NewServer/handlers.go)", action)
		}
	}
}

// TestServer_RegistryDispatchesMigratedActions garante que as ações conhecidas
// são atendidas pelo registry (roteador) com o mesmo comportamento — o switch
// legado foi eliminado e o registry é o único caminho. Cobertura pontual sobre
// a suíte existente de preset/apps/tamper/goal (que exercita o caminho
// completo).
func TestServer_RegistryDispatchesMigratedActions(t *testing.T) {
	s := setupTestServer(t)

	migrated := []string{
		"ping", "presets", "preset-add", "preset-remove",
		"apps-list", "apps-add", "apps-remove",
		"tamper-log", "goal-get", "goal-set",
		"stats", "missions", "sessions",
		"schedule-list", "schedule-add", "schedule-import", "schedule-remove",
		"pomodoro", "pomodoro-defaults", "pomodoro-stop",
		"update", "update-check",
		"block", "block-all", "status",
		"user-list", "user-verify", "user-add", "user-remove", "user-set-password",
		"dns-start", "dns-stop", "dns-status", "dns-set-upstream",
	}
	for _, action := range migrated {
		if _, ok := s.registry.Get(action); !ok {
			t.Errorf("ação migrada %q não está no registry (o switch legado ainda a atende?)", action)
		}
	}
}
