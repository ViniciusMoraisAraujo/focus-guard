package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"focusguard/internal/domain/policy"
)

// ---------------------------------------------------------------------------
// deriveKey
// ---------------------------------------------------------------------------

func TestDeriveKey_Is32BytesAndDeterministic(t *testing.T) {
	key1, err := deriveKey([]byte("machine-secret"))
	if err != nil {
		t.Fatalf("deriveKey erro: %v", err)
	}
	key2, err := deriveKey([]byte("machine-secret"))
	if err != nil {
		t.Fatalf("deriveKey erro: %v", err)
	}
	if len(key1) != 32 {
		t.Fatalf("AES-256 exige chave de 32 bytes, obteve %d", len(key1))
	}
	if !bytes.Equal(key1[:], key2[:]) {
		t.Error("deriveKey deve ser determinístico para o mesmo segredo")
	}
}

func TestDeriveKey_DiffersBySecret(t *testing.T) {
	k1, err := deriveKey([]byte("secret-a"))
	if err != nil {
		t.Fatalf("deriveKey erro: %v", err)
	}
	k2, err := deriveKey([]byte("secret-b"))
	if err != nil {
		t.Fatalf("deriveKey erro: %v", err)
	}
	if bytes.Equal(k1[:], k2[:]) {
		t.Error("segredos diferentes devem gerar chaves diferentes")
	}
}

func TestDeriveKey_EmptySecret_ReturnsError(t *testing.T) {
	if _, err := deriveKey(nil); err == nil {
		t.Error("segredo vazio deve retornar erro")
	}
}

// ---------------------------------------------------------------------------
// encryptReplica / decryptReplica (AES-256-GCM — o tag GCM é a "assinatura")
// ---------------------------------------------------------------------------

func TestEncryptDecryptReplica_RoundTrip(t *testing.T) {
	key, err := deriveKey([]byte("hardware-bound-secret"))
	if err != nil {
		t.Fatalf("deriveKey erro: %v", err)
	}
	plain := []byte(`{"version":1,"blocks":{}}`)

	blob, err := encryptReplica(key, plain)
	if err != nil {
		t.Fatalf("encryptReplica erro: %v", err)
	}
	if bytes.Contains(blob, plain) {
		t.Error("ciphertext não pode conter o plaintext")
	}

	got, err := decryptReplica(key, blob)
	if err != nil {
		t.Fatalf("decryptReplica erro: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round-trip divergiu: %q != %q", got, plain)
	}
}

func TestDecryptReplica_WrongKey_Fails(t *testing.T) {
	k1, _ := deriveKey([]byte("key-a"))
	k2, _ := deriveKey([]byte("key-b"))
	blob, err := encryptReplica(k1, []byte(`{"version":1}`))
	if err != nil {
		t.Fatalf("encryptReplica erro: %v", err)
	}
	if _, err := decryptReplica(k2, blob); err == nil {
		t.Error("chave errada deve falhar na autenticação (tag GCM)")
	}
}

func TestDecryptReplica_TamperedCiphertext_Fails(t *testing.T) {
	key, _ := deriveKey([]byte("hardware-bound-secret"))
	blob, err := encryptReplica(key, []byte(`{"version":1,"blocks":{}}`))
	if err != nil {
		t.Fatalf("encryptReplica erro: %v", err)
	}
	// Corrompe um byte do ciphertext (depois do nonce) — o GCM deve rejeitar.
	blob[len(blob)-1] ^= 0xff
	if _, err := decryptReplica(key, blob); err == nil {
		t.Error("ciphertext adulterado deve falhar a autenticação")
	}
}

func TestDecryptReplica_TooShort_ReturnsError(t *testing.T) {
	key, _ := deriveKey([]byte("hardware-bound-secret"))
	if _, err := decryptReplica(key, []byte("tiny")); err == nil {
		t.Error("blob menor que nonce+tag deve retornar erro")
	}
}

// ---------------------------------------------------------------------------
// EnableReplica (chave atrelada ao hardware)
// ---------------------------------------------------------------------------

func TestEnableReplica_FromHardwareID(t *testing.T) {
	orig := hardwareID
	hardwareID = func() (string, error) { return "test-machine-id", nil }
	defer func() { hardwareID = orig }()

	tempDir := t.TempDir()
	s, err := NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}

	if err := s.EnableReplica(nil); err != nil {
		t.Fatalf("EnableReplica com ID de hardware deve funcionar, erro: %v", err)
	}
	if s.replicaKey == nil {
		t.Error("replicaKey deve ser derivada do ID de hardware")
	}
}

func TestEnableReplica_ExplicitSecret(t *testing.T) {
	tempDir := t.TempDir()
	s, err := NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("deterministic-test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}
	if s.replicaKey == nil {
		t.Error("replicaKey deve ser derivada do segredo explícito")
	}
}

func TestEnableReplica_NoHardwareID_ReturnsError(t *testing.T) {
	orig := hardwareID
	hardwareID = func() (string, error) { return "", os.ErrNotExist }
	defer func() { hardwareID = orig }()

	tempDir := t.TempDir()
	s, err := NewStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica(nil); err == nil {
		t.Error("EnableReplica(nil) sem ID de hardware deve retornar erro")
	}
	if s.replicaKey != nil {
		t.Error("replicaKey não deve ser definida em falha de hardware ID")
	}
}

// ---------------------------------------------------------------------------
// Save grava a réplica criptografada oculta
// ---------------------------------------------------------------------------

func TestSave_WritesHiddenEncryptedReplica(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}

	now := time.Now()
	state := &State{
		Version: 1,
		Blocks: map[string]policy.Block{
			"twitter.com": {
				Domain:      "twitter.com",
				StartedAt:   now,
				ExpiresAt:   now.Add(2 * time.Hour),
				ResolvedIPs: []string{"104.244.42.1"},
			},
		},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("Save erro: %v", err)
	}

	replicaPath := filepath.Join(tempDir, ".state.json.replica")
	blob, err := os.ReadFile(replicaPath)
	if err != nil {
		t.Fatalf("réplica oculta deve existir após Save: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(replicaPath), ".") {
		t.Errorf("réplica deve ser oculta (dotfile), base=%q", filepath.Base(replicaPath))
	}
	if bytes.Contains(blob, []byte("twitter.com")) {
		t.Error("réplica não pode conter o plaintext (deve estar criptografada)")
	}

	// Decripta com a mesma chave e confere o conteúdo.
	decrypted, err := decryptReplica(*s.replicaKey, blob)
	if err != nil {
		t.Fatalf("decryptReplica da réplica erro: %v", err)
	}
	var got State
	if err := json.Unmarshal(decrypted, &got); err != nil {
		t.Fatalf("réplica decriptada deve ser JSON válido: %v", err)
	}
	if _, ok := got.Blocks["twitter.com"]; !ok {
		t.Error("réplica decriptada deve conter o bloco twitter.com")
	}
}

func TestSave_ReplicaDisabled_DoesNotWriteReplica(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.Save(cleanState()); err != nil {
		t.Fatalf("Save erro: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".state.json.replica")); !os.IsNotExist(err) {
		t.Error("sem EnableReplica não deve existir réplica")
	}
}

// ---------------------------------------------------------------------------
// LoadAndHeal — auto-healing a partir da réplica
// ---------------------------------------------------------------------------

func TestLoadAndHeal_PrimaryValid_UsesPrimary(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}

	now := time.Now()
	state := &State{
		Version: 1,
		Blocks: map[string]policy.Block{
			"youtube.com": {Domain: "youtube.com", StartedAt: now, ExpiresAt: now.Add(2 * time.Hour)},
		},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("Save erro: %v", err)
	}

	got, err := s.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal erro: %v", err)
	}
	if _, ok := got.Blocks["youtube.com"]; !ok {
		t.Error("primary válido deve ser usado sem healing")
	}
}

func TestLoadAndHeal_CorruptedPrimary_HealsFromReplica(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}

	now := time.Now()
	state := &State{
		Version: 1,
		Blocks: map[string]policy.Block{
			"twitter.com": {Domain: "twitter.com", StartedAt: now, ExpiresAt: now.Add(2 * time.Hour)},
		},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("Save erro: %v", err)
	}

	// Corrompe o primary DEPOIS do último Save (a réplica guarda o bom estado).
	if err := os.WriteFile(dbPath, []byte(`{not valid json`), 0644); err != nil {
		t.Fatalf("corromper primary: %v", err)
	}

	got, err := s.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal erro: %v", err)
	}
	if _, ok := got.Blocks["twitter.com"]; !ok {
		t.Fatalf("LoadAndHeal deve recuperar o bloco da réplica, obteve %d blocos", len(got.Blocks))
	}

	// O primary deve ter sido restaurado para JSON válido com o bloco.
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ler primary restaurado: %v", err)
	}
	var restored State
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("primary deve ser restaurado para JSON válido: %v (%q)", err, string(data))
	}
	if _, ok := restored.Blocks["twitter.com"]; !ok {
		t.Error("primary restaurado deve conter o bloco da réplica")
	}
}

func TestLoadAndHeal_DeletedPrimary_HealsFromReplica(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}

	now := time.Now()
	state := &State{
		Version: 1,
		Blocks: map[string]policy.Block{
			"github.com": {Domain: "github.com", StartedAt: now, ExpiresAt: now.Add(2 * time.Hour)},
		},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("Save erro: %v", err)
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remover primary: %v", err)
	}

	got, err := s.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal erro: %v", err)
	}
	if _, ok := got.Blocks["github.com"]; !ok {
		t.Fatalf("LoadAndHeal deve recuperar o bloco da réplica após exclusão, obteve %d", len(got.Blocks))
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("primary deve ser recriado a partir da réplica: %v", err)
	}
}

func TestLoadAndHeal_NoReplica_FallsBackToClean(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte(`{corrompido`), 0644); err != nil {
		t.Fatalf("corromper primary: %v", err)
	}

	got, err := s.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal sem réplica não deve errar: %v", err)
	}
	if len(got.Blocks) != 0 {
		t.Errorf("sem réplica deve cair no estado limpo, obteve %d blocos", len(got.Blocks))
	}
}

func TestLoadAndHeal_TamperedReplica_FallsBackToClean(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s.EnableReplica([]byte("test-secret")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}
	if err := s.Save(cleanState()); err != nil {
		t.Fatalf("Save erro: %v", err)
	}

	// Corrompe primary E réplica — o GCM rejeita a réplica e cai no limpo.
	if err := os.WriteFile(dbPath, []byte(`{corrompido`), 0644); err != nil {
		t.Fatalf("corromper primary: %v", err)
	}
	replicaPath := filepath.Join(tempDir, ".state.json.replica")
	if err := os.WriteFile(replicaPath, []byte("garbage-not-gcm"), 0600); err != nil {
		t.Fatalf("adulterar réplica: %v", err)
	}

	got, err := s.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal com réplica adulterada não deve errar: %v", err)
	}
	if len(got.Blocks) != 0 {
		t.Errorf("réplica adulterada deve cair no estado limpo, obteve %d blocos", len(got.Blocks))
	}
}

func TestLoadAndHeal_WrongKeyReplica_FallsBackToClean(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.json")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	// Réplica gravada com uma chave (hardware A)...
	if err := s.EnableReplica([]byte("hardware-a")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}
	if err := s.Save(cleanState()); err != nil {
		t.Fatalf("Save erro: %v", err)
	}
	// ...mas recuperada em outra máquina (hardware B): a chave não bate.
	s2, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore erro: %v", err)
	}
	if err := s2.EnableReplica([]byte("hardware-b")); err != nil {
		t.Fatalf("EnableReplica erro: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte(`{corrompido`), 0644); err != nil {
		t.Fatalf("corromper primary: %v", err)
	}

	got, err := s2.LoadAndHeal()
	if err != nil {
		t.Fatalf("LoadAndHeal com chave errada não deve errar: %v", err)
	}
	if len(got.Blocks) != 0 {
		t.Errorf("chave errada não pode recuperar a réplica, obteve %d blocos", len(got.Blocks))
	}
}
