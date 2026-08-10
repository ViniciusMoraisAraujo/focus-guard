package devices

import (
	"context"
	"os"
	"testing"
)

// writeFile é um helper local (o teste de arquivo corrompido precisa criar um
// arquivo inválido antes do NewStore).
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func TestIsBlocked_UnknownIP_Inherits(t *testing.T) {
	s := NewStore("")
	if blocked, decided := s.IsBlocked("youtube.com", "192.168.1.50"); decided {
		t.Fatalf("IP desconhecido não deveria decidir (decided=true, blocked=%v)", blocked)
	}
}

func TestIsBlocked_InheritPolicy_FallsThrough(t *testing.T) {
	s := NewStore("")
	_ = s.Upsert(Device{IP: "192.168.1.10", Name: "TV", Policy: PolicyInherit})
	if blocked, decided := s.IsBlocked("youtube.com", "192.168.1.10"); decided {
		t.Fatalf("política inherit não deveria decidir (blocked=%v)", blocked)
	}
}

func TestIsBlocked_BlockAll_Decides(t *testing.T) {
	s := NewStore("")
	_ = s.Upsert(Device{IP: "192.168.1.20", Policy: PolicyBlockAll})
	blocked, decided := s.IsBlocked("youtube.com", "192.168.1.20")
	if !decided || !blocked {
		t.Fatalf("block_all deveria bloquear tudo: decided=%v blocked=%v", decided, blocked)
	}
	// Outro IP não é afetado.
	if _, decided := s.IsBlocked("youtube.com", "192.168.1.21"); decided {
		t.Fatal("IP sem regra não deveria decidir")
	}
}

func TestIsBlocked_AllowList_BlocksEverythingElse(t *testing.T) {
	s := NewStore("")
	_ = s.Upsert(Device{IP: "192.168.1.30", Policy: PolicyAllowList, AllowedDomains: []string{"example.com"}})
	blocked, decided := s.IsBlocked("example.com", "192.168.1.30")
	if !decided || blocked {
		t.Fatalf("domínio permitido não deveria ser bloqueado: decided=%v blocked=%v", decided, blocked)
	}
	blocked, _ = s.IsBlocked("www.example.com", "192.168.1.30")
	if blocked {
		t.Fatal("subdomínio do permitido não deveria ser bloqueado")
	}
	blocked, _ = s.IsBlocked("youtube.com", "192.168.1.30")
	if !blocked {
		t.Fatal("domínio fora da allowlist deveria ser bloqueado")
	}
}

func TestStore_Upsert_ReplaceAndRemove(t *testing.T) {
	s := NewStore("")
	if err := s.Upsert(Device{IP: "192.168.1.10", Name: "TV", Policy: PolicyBlockAll}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Device{IP: "192.168.1.10", Name: "TV 2", Policy: PolicyInherit}); err != nil {
		t.Fatal(err)
	}
	d, ok := s.Get("192.168.1.10")
	if !ok || d.Name != "TV 2" || d.Policy != PolicyInherit {
		t.Fatalf("upsert não substituiu: %+v ok=%v", d, ok)
	}
	if err := s.Remove("192.168.1.10"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("192.168.1.10"); ok {
		t.Fatal("dispositivo deveria ter sido removido")
	}
}

func TestStore_Upsert_Validation(t *testing.T) {
	s := NewStore("")
	if err := s.Upsert(Device{IP: "", Policy: PolicyBlockAll}); err == nil {
		t.Fatal("IP vazio deveria ser rejeitado")
	}
	if err := s.Upsert(Device{IP: "192.168.1.10", Policy: Policy("fake")}); err == nil {
		t.Fatal("política desconhecida deveria ser rejeitada")
	}
	if err := s.Upsert(Device{IP: "192.168.1.10", Policy: PolicyAllowList}); err == nil {
		t.Fatal("allow_list sem domínios deveria ser rejeitada")
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/devices.json"
	s := NewStore(path)
	_ = s.Upsert(Device{IP: "192.168.1.10", Name: "TV", Policy: PolicyBlockAll})
	_ = s.Upsert(Device{IP: "192.168.1.30", Policy: PolicyAllowList, AllowedDomains: []string{"example.com"}})

	re := NewStore(path)
	list := re.List()
	if len(list) != 2 {
		t.Fatalf("persistência perdeu dispositivos: %v", list)
	}
	blocked, decided := re.IsBlocked("youtube.com", "192.168.1.10")
	if !decided || !blocked {
		t.Fatalf("block_all não sobreviveu à persistência: decided=%v blocked=%v", decided, blocked)
	}
}

func TestStore_CorruptFile_DegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/devices.json"
	if err := writeFile(path, []byte("{corrupt")); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if len(s.List()) != 0 {
		t.Fatalf("arquivo corrompido deveria degradar para catálogo vazio, got %v", s.List())
	}
	// Upsert ainda funciona e reescreve o arquivo.
	if err := s.Upsert(Device{IP: "192.168.1.10", Policy: PolicyBlockAll}); err != nil {
		t.Fatal(err)
	}
}

func TestHandler_ListAndUpsert(t *testing.T) {
	s := NewStore(t.TempDir() + "/devices.json")
	hList := NewList(s)
	res, err := hList.Handle(context.Background(), &NoInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 0 {
		t.Fatalf("catálogo novo deveria estar vazio, got %v", res.Devices)
	}

	hUp := NewUpsert(s)
	up, err := hUp.Handle(context.Background(), &UpsertInput{Device: Device{IP: "192.168.1.10", Name: "TV", Policy: PolicyBlockAll}})
	if err != nil {
		t.Fatal(err)
	}
	if up.Message == "" {
		t.Fatal("mensagem de sucesso vazia")
	}
	res, err = hList.Handle(context.Background(), &NoInput{})
	if err != nil || len(res.Devices) != 1 {
		t.Fatalf("upsert não refletiu na listagem: %v %v", res, err)
	}
}

func TestHandler_NoStore_NotConfigured(t *testing.T) {
	h := NewList(nil)
	if _, err := h.Handle(context.Background(), &NoInput{}); err == nil {
		t.Fatal("devices-list sem store deveria falhar")
	}
}
