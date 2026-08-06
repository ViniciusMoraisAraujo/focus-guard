package update

import (
	"context"
	"errors"
	"testing"
)

// fakeChecker records the apply/channel inputs and returns canned status.
type fakeChecker struct {
	status      Status
	err         error
	lastApply   bool
	lastChannel string
}

func (f *fakeChecker) Check(_ context.Context, apply bool, channel string) (Status, error) {
	f.lastApply = apply
	f.lastChannel = channel
	if f.err != nil {
		return f.status, f.err
	}
	return f.status, nil
}

func TestRun_NoChecker_NotConfigured(t *testing.T) {
	svc := NewService(nil, false)
	_, err := svc.Run(context.Background(), "")
	if err == nil || err.Error() != "auto-update não configurado" {
		t.Fatalf("erro = %v, esperava 'auto-update não configurado'", err)
	}
}

func TestRun_CheckerError_Propagates(t *testing.T) {
	fc := &fakeChecker{err: errors.New("rede indisponível")}
	svc := NewService(fc, false)
	_, err := svc.Run(context.Background(), "")
	if err == nil || err.Error() != "rede indisponível" {
		t.Fatalf("erro = %v, esperava propagação do checker", err)
	}
}

func TestRun_NoUpdate(t *testing.T) {
	fc := &fakeChecker{status: Status{CurrentVersion: "1.0.0"}}
	svc := NewService(fc, false)

	res, err := svc.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if res.Status.CurrentVersion != "1.0.0" || res.Status.Available {
		t.Errorf("status = %+v, esperava 1.0.0 sem update", res.Status)
	}
	if res.Applied {
		t.Error("Applied deve ser false sem update")
	}
	if res.Message != "Nenhuma atualização disponível." {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestRun_Available(t *testing.T) {
	fc := &fakeChecker{status: Status{CurrentVersion: "1.0.0", NewVersion: "1.1.0", Available: true}}
	svc := NewService(fc, false)

	res, err := svc.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if !res.Status.Available || res.Status.NewVersion != "1.1.0" {
		t.Errorf("status = %+v, esperava disponível 1.1.0", res.Status)
	}
	if res.Message != "Atualização disponível: 1.0.0 → 1.1.0" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestRun_Applied_SignalsRestart(t *testing.T) {
	fc := &fakeChecker{status: Status{
		CurrentVersion: "1.0.0", NewVersion: "1.1.0", Available: true, Applied: true,
	}}
	svc := NewService(fc, true)

	res, err := svc.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if !res.Applied {
		t.Error("Applied deve ser true após aplicar")
	}
	if res.Message != "Atualização aplicada: 1.0.0 → 1.1.0. O daemon será reiniciado automaticamente." {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestRun_PendingReboot_NoRestart(t *testing.T) {
	fc := &fakeChecker{status: Status{
		CurrentVersion: "1.0.0", NewVersion: "1.1.0", Available: true, PendingReboot: true,
	}}
	svc := NewService(fc, true)

	res, err := svc.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run falhou: %v", err)
	}
	if res.Applied {
		t.Error("Applied deve ser false no fallback move-on-reboot")
	}
	if res.Message != "Atualização será concluída no próximo reinício do computador: 1.0.0 → 1.1.0" {
		t.Errorf("Message = %q", res.Message)
	}
}

func TestRun_PassesApplyAndChannel(t *testing.T) {
	fc := &fakeChecker{status: Status{CurrentVersion: "1.0.0"}}

	svc := NewService(fc, false)
	_, _ = svc.Run(context.Background(), "beta")
	if fc.lastApply || fc.lastChannel != "beta" {
		t.Errorf("update-check: Check(apply=%v, channel=%q), esperava false/beta", fc.lastApply, fc.lastChannel)
	}

	svc = NewService(fc, true)
	_, _ = svc.Run(context.Background(), "stable")
	if !fc.lastApply || fc.lastChannel != "stable" {
		t.Errorf("update: Check(apply=%v, channel=%q), esperava true/stable", fc.lastApply, fc.lastChannel)
	}
}
