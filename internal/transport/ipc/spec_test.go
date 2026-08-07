package ipc

import (
	"testing"
	"time"
)

func TestSpecFor_KnownAction(t *testing.T) {
	spec, ok := SpecFor("block")
	if !ok {
		t.Fatal("esperava spec para block")
	}
	if spec.Action != "block" {
		t.Errorf("Action = %q, want block", spec.Action)
	}
}

func TestSpecFor_UnknownAction(t *testing.T) {
	if _, ok := SpecFor("nao-existe"); ok {
		t.Fatal("ação desconhecida não deveria ter spec")
	}
}

// TestSpecFor_UserVerifyAbsent trava a fronteira de segurança: user-verify só
// é legítimo pelo /api/login; ausente do spec, o proxy web o 403 (allowlist
// por spec) em vez de encaminhá-lo.
func TestSpecFor_UserVerifyAbsent(t *testing.T) {
	if _, ok := SpecFor("user-verify"); ok {
		t.Fatal("user-verify não pode ter spec — é web-only (via /api/login)")
	}
}

func TestSpec_Permissions(t *testing.T) {
	cases := []struct {
		action string
		perm   Permission
		self   string
	}{
		{action: "user-list", perm: PermAdmin},
		{action: "user-add", perm: PermAdmin},
		{action: "user-remove", perm: PermAdmin},
		{action: "user-set-password", perm: PermSelf, self: "user_name"},
		{action: "status", perm: PermAuthenticated},
		{action: "block", perm: PermAuthenticated},
		{action: "update", perm: PermAuthenticated},
	}
	for _, tc := range cases {
		spec, ok := SpecFor(tc.action)
		if !ok {
			t.Fatalf("%s: sem spec", tc.action)
		}
		if spec.Permission != tc.perm {
			t.Errorf("%s: Permission = %v, want %v", tc.action, spec.Permission, tc.perm)
		}
		if spec.SelfField != tc.self {
			t.Errorf("%s: SelfField = %q, want %q", tc.action, spec.SelfField, tc.self)
		}
	}
}

// TestSpec_Timeouts congela os orçamentos do proxy (B7) — a fonte única é a
// tabela specs, consumida pelo httpapi. O timeout do proxy deve ser ≥ o
// orçamento interno do daemon para a mesma ação.
func TestSpec_Timeouts(t *testing.T) {
	cases := []struct {
		action string
		want   time.Duration
	}{
		{action: "update", want: 150 * time.Second},
		{action: "update-check", want: 150 * time.Second},
		{action: "status", want: 15 * time.Second},
		{action: "block", want: 30 * time.Second},
		{action: "block-all", want: 30 * time.Second},
		{action: "pomodoro", want: 30 * time.Second},
		{action: "pomodoro-stop", want: 30 * time.Second},
		{action: "presets", want: 5 * time.Second},
		{action: "apps-list", want: 5 * time.Second},
		{action: "stats", want: 5 * time.Second},
		{action: "ping", want: 5 * time.Second},
		{action: "event-subscribe", want: 30 * time.Second},
	}
	for _, tc := range cases {
		spec, ok := SpecFor(tc.action)
		if !ok {
			t.Fatalf("%s: sem spec", tc.action)
		}
		if spec.Timeout != tc.want {
			t.Errorf("%s: Timeout = %v, want %v", tc.action, spec.Timeout, tc.want)
		}
	}
}

// TestSpecActions_CoversProxyableActions garante que toda ação encaminhável
// pelo web tem spec (e que as web-only não entram). O conjunto é o fechamento
// do switch atual menos user-verify.
func TestSpecActions_CoversProxyableActions(t *testing.T) {
	got := SpecActions()
	want := map[string]bool{
		"block": true, "block-all": true, "status": true, "ping": true,
		"update": true, "update-check": true,
		"presets": true, "preset-add": true, "preset-remove": true,
		"tamper-log": true,
		"apps-list":  true, "apps-add": true, "apps-remove": true,
		"user-list": true, "user-add": true, "user-remove": true, "user-set-password": true,
		"schedule-list": true, "schedule-add": true, "schedule-import": true, "schedule-remove": true,
		"pomodoro": true, "pomodoro-stop": true, "pomodoro-defaults": true,
		"goal-get": true, "goal-set": true,
		"stats": true, "missions": true, "sessions": true,
		"dns-start": true, "dns-stop": true, "dns-status": true, "dns-set-upstream": true,
		"event-subscribe": true,
		"metrics":         true,
	}
	if len(got) != len(want) {
		t.Fatalf("SpecActions = %d ações (%v), want %d", len(got), got, len(want))
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("spec inesperado: %q", a)
		}
	}
	if _, ok := SpecFor("user-verify"); ok {
		t.Error("user-verify não pode estar no spec")
	}
}

// TestSpec_ProxyBudgetAtLeastDaemonInternal trava o invariante de timeout do
// update (B7): o orçamento do proxy (spec) deve ser ≥ o orçamento interno do
// daemon para a mesma ação (internal/transport/ipc.UpdateTimeout) — senão um
// update lento-mas-bem-sucedido viraria "daemon indisponível" falso.
func TestSpec_ProxyBudgetAtLeastDaemonInternal(t *testing.T) {
	if spec, ok := SpecFor("update"); ok {
		if spec.Timeout < UpdateTimeout {
			t.Errorf("spec.update.Timeout=%v deve ser >= orçamento interno do daemon (%v)", spec.Timeout, UpdateTimeout)
		}
	}
	// O mesmo invariante vale para o long-poll de eventos (Fase 7): o proxy
	// espera até eventSubscribeTimeout por um ciclo quieto; um spec menor
	// viraria "daemon indisponível" falso a cada heartbeat.
	if spec, ok := SpecFor("event-subscribe"); ok {
		if spec.Timeout < eventSubscribeTimeout {
			t.Errorf("spec.event-subscribe.Timeout=%v deve ser >= orçamento interno (%v)", spec.Timeout, eventSubscribeTimeout)
		}
	}
	if spec, ok := SpecFor("update-check"); ok {
		if spec.Timeout < UpdateTimeout {
			t.Errorf("spec.update-check.Timeout=%v deve ser >= orçamento interno do daemon (%v)", spec.Timeout, UpdateTimeout)
		}
	}
}

func TestSpecActions_Sorted(t *testing.T) {
	got := SpecActions()
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("SpecActions fora de ordem: %v", got)
		}
	}
}
