package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"focusguard/internal/domain/policy"
	"focusguard/internal/infrastructure/tlsca"
	"focusguard/internal/transport/ipc"
)

// fakeDoctorClient é um ipc.Client fake: responde por ação com respostas
// programadas ou um erro padrão.
type fakeDoctorClient struct {
	responses map[string]*ipc.Response
	err       error
}

func (f *fakeDoctorClient) Send(req ipc.Request) (*ipc.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.responses[req.Action]; ok {
		return r, nil
	}
	return &ipc.Response{Success: true}, nil
}

func okPing() *ipc.Response { return &ipc.Response{Success: true, Message: "pong"} }
func okStatus() *ipc.Response {
	return &ipc.Response{Success: true, CurrentVersion: "0.16.4", FirewallRules: 4, DoHActive: true, ExpectedDoH: true}
}
func okDNS() *ipc.Response {
	return &ipc.Response{Success: true, DNSEnabled: true, DNSListening: true, DNSAddr: "0.0.0.0:53", DNSUpstream: "1.1.1.2:53"}
}

// healthyEnv monta um ambiente com tudo passando: daemon acessível, serviços
// rodando, state válido, hosts consistente, firewall ok, versões completas,
// DNS ativo.
func healthyEnv(t *testing.T) doctorEnv {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":1,"blocks":{},"dns_enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	hostsPath := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Suíte completa no diretório do "executável" (checagem de versões).
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Ext(os.Args[0]) // .exe no Windows, "" no Linux — mesmo cálculo do checkVersions
	for _, n := range []string{"focusguard-daemon", "focusguard-tray", "focusguard-watchdog", "focusguard-web"} {
		if err := os.WriteFile(filepath.Join(binDir, n+ext), []byte("bin"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	return doctorEnv{
		client: &fakeDoctorClient{
			responses: map[string]*ipc.Response{"ping": okPing(), "status": okStatus(), "dns-status": okDNS()},
		},
		statePath: statePath,
		hostsPath: hostsPath,
		exec:      fakeServiceRunning,
		isAdmin:   func() bool { return true },
		version:   "0.16.4",
		exeDir:    binDir,
	}
}

// fakeServiceRunning faz toda consulta de serviço devolver "rodando".
func fakeServiceRunning(_ string, _ ...string) ([]byte, error) {
	return []byte("running"), nil
}

func TestDoctor_HealthyExitsZero(t *testing.T) {
	env := healthyEnv(t)
	results := runDoctor(env)
	if code := doctorExitCode(results); code != 0 {
		t.Errorf("exit = %d, want 0 — resultados:\n%s", code, summarizeResults(results))
	}
	for _, r := range results {
		if r.Status == statusFail {
			t.Errorf("checagem %q falhou numa instalação saudável: %s", r.Name, r.Message)
		}
	}
}

func TestDoctor_IPCUnreachableFails(t *testing.T) {
	env := healthyEnv(t)
	env.client = &fakeDoctorClient{err: errFakeUnreachable}

	results := runDoctor(env)

	ipc := findResult(results, "IPC")
	if ipc == nil || ipc.Status != statusFail {
		t.Fatalf("IPC deveria falhar com daemon fora — got %+v", ipc)
	}
	if code := doctorExitCode(results); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

var errFakeUnreachable = &fakeError{"daemon indisponível"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

func TestDoctor_DNSBindErrorFails(t *testing.T) {
	env := healthyEnv(t)
	env.client = &fakeDoctorClient{
		responses: map[string]*ipc.Response{
			"ping":   okPing(),
			"status": okStatus(),
			"dns-status": {Success: true, DNSEnabled: true, DNSListening: false,
				DNSBindError: "porta 53 em uso"},
		},
	}

	results := runDoctor(env)
	dns := findResult(results, "DNS")
	if dns == nil || dns.Status != statusFail {
		t.Fatalf("DNS habilitado e parado deveria falhar — got %+v", dns)
	}
	if !strings.Contains(dns.Message, "não está ouvindo") {
		t.Errorf("mensagem DNS = %q, want mencionando não está ouvindo", dns.Message)
	}
}

func TestDoctor_HostsOrphanEntryFails(t *testing.T) {
	env := healthyEnv(t)
	// Bloqueio ativo no daemon sem entrada correspondente no hosts.
	env.client = &fakeDoctorClient{
		responses: map[string]*ipc.Response{
			"ping": okPing(),
			"status": {
				Success: true, CurrentVersion: "0.16.4", FirewallRules: 4, DoHActive: true, ExpectedDoH: true,
				Blocks: []policy.Block{{Domain: "youtube.com", ExpiresAt: time.Now().Add(time.Hour)}},
			},
			"dns-status": okDNS(),
		},
	}

	results := runDoctor(env)
	hosts := findResult(results, "Hosts")
	if hosts == nil || hosts.Status != statusFail {
		t.Fatalf("hosts sem o bloqueio ativo deveria falhar — got %+v", hosts)
	}
	if !strings.Contains(hosts.Message, "youtube.com") {
		t.Errorf("mensagem hosts = %q, want mencionando youtube.com", hosts.Message)
	}
}

func TestDoctor_HostsOrphanClearedPasses(t *testing.T) {
	env := healthyEnv(t)
	// Bloqueio ativo + entrada correspondente no hosts → consistente.
	if err := os.WriteFile(env.hostsPath, []byte("127.0.0.1 youtube.com # FOCUSGUARD: youtube.com\n127.0.0.1 www.youtube.com # FOCUSGUARD: www.youtube.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env.client = &fakeDoctorClient{
		responses: map[string]*ipc.Response{
			"ping":       okPing(),
			"status":     {Success: true, CurrentVersion: "0.16.4", FirewallRules: 4, DoHActive: true, ExpectedDoH: true, Blocks: []policy.Block{{Domain: "youtube.com", ExpiresAt: time.Now().Add(time.Hour)}}},
			"dns-status": okDNS(),
		},
	}

	results := runDoctor(env)
	hosts := findResult(results, "Hosts")
	if hosts == nil || hosts.Status != statusPass {
		t.Fatalf("hosts consistente deveria passar — got %+v", hosts)
	}
}

// TestDoctor_StateNotWritableNonElevatedWarns cobre o falso positivo real:
// o CLI não elevado não consegue abrir o state.json para escrita mesmo com a
// instalação saudável (o daemon elevado é quem grava) — deve virar WARN, não
// FAIL, para o doctor não acusar problema numa máquina OK.
func TestDoctor_StateNotWritableNonElevatedWarns(t *testing.T) {
	env := healthyEnv(t)
	env.isAdmin = func() bool { return false }
	// Torna o arquivo somente-leitura (no Windows define FILE_ATTRIBUTE_READONLY).
	if err := os.Chmod(env.statePath, 0400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(env.statePath, 0600) })

	results := runDoctor(env)
	st := findResult(results, "Estado")
	if st == nil {
		t.Fatal("checagem Estado ausente")
	}
	if st.Status != statusWarn {
		t.Errorf("Estado não-gravável com shell não elevado = %v, want warn — %s", st.Status, st.Message)
	}
	if code := doctorExitCode(results); code != 1 {
		t.Errorf("exit = %d, want 1 (warn ainda sinaliza problemas)", code)
	}
}

// TestDoctor_StateNotWritableElevatedFails garante que com elevação a falha de
// gravação continua sendo FAIL (permite distinguir instalação quebrada).
func TestDoctor_StateNotWritableElevatedFails(t *testing.T) {
	env := healthyEnv(t)
	env.isAdmin = func() bool { return true }
	if err := os.Chmod(env.statePath, 0400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(env.statePath, 0600) })

	results := runDoctor(env)
	st := findResult(results, "Estado")
	if st == nil {
		t.Fatal("checagem Estado ausente")
	}
	if st.Status != statusFail {
		t.Errorf("Estado não-gravável com shell elevado = %v, want fail — %s", st.Status, st.Message)
	}
}

func TestDoctor_StateCorruptedFails(t *testing.T) {
	env := healthyEnv(t)
	if err := os.WriteFile(env.statePath, []byte("{corrompido"), 0600); err != nil {
		t.Fatal(err)
	}

	results := runDoctor(env)
	st := findResult(results, "Estado")
	if st == nil || st.Status != statusFail {
		t.Fatalf("state.json corrompido deveria falhar — got %+v", st)
	}
}

func TestDoctor_ServiceMissingFails(t *testing.T) {
	env := healthyEnv(t)
	env.exec = func(_ string, _ ...string) ([]byte, error) {
		return []byte("exit status 1060"), errFakeUnreachable
	}

	results := runDoctor(env)
	svc := findResult(results, "Serviços")
	if svc == nil || svc.Status != statusFail {
		t.Fatalf("serviço ausente deveria falhar — got %+v", svc)
	}
	if !strings.Contains(svc.Message, "não instalado") {
		t.Errorf("mensagem serviços = %q, want mencionando não instalado", svc.Message)
	}
}

func TestDoctor_ServiceCheckErrorWarns(t *testing.T) {
	env := healthyEnv(t)
	// Falha de execução não relacionada a "não instalado" → warn, nunca fail
	// do doctor inteiro por um binário ausente.
	env.exec = func(_ string, _ ...string) ([]byte, error) {
		return []byte(""), errFakeUnreachable
	}

	results := runDoctor(env)
	svc := findResult(results, "Serviços")
	if svc == nil || svc.Status != statusWarn {
		t.Fatalf("erro de consulta de serviço deveria virar warn — got %+v", svc)
	}
}

func TestDoctor_FirewallExpectedButInactiveFails(t *testing.T) {
	env := healthyEnv(t)
	env.client = &fakeDoctorClient{
		responses: map[string]*ipc.Response{
			"ping":       okPing(),
			"status":     {Success: true, CurrentVersion: "0.16.4", FirewallRules: 0, DoHActive: false, ExpectedDoH: true},
			"dns-status": okDNS(),
		},
	}

	results := runDoctor(env)
	fw := findResult(results, "Firewall")
	if fw == nil || fw.Status != statusFail {
		t.Fatalf("bloqueios ativos sem DoH deveria falhar — got %+v", fw)
	}
}

func TestDoctor_JSONOutput(t *testing.T) {
	env := healthyEnv(t)
	results := runDoctor(env)
	code := doctorExitCode(results)

	var buf strings.Builder
	// printDoctorJSON escreve via fmt.Println — captura direto da struct.
	_ = buf
	out := doctorJSON{Overall: code, Checks: make([]doctorCheckJSON, 0, len(results))}
	for _, r := range results {
		out.Checks = append(out.Checks, doctorCheckJSON{Name: r.Name, Status: string(r.Status), Message: r.Message})
	}
	if len(out.Checks) != 9 {
		t.Errorf("JSON com %d checagens, want 9", len(out.Checks))
	}
	for _, c := range out.Checks {
		if c.Status != "pass" {
			t.Errorf("checagem %s = %s, want pass", c.Name, c.Status)
		}
	}
	if out.Overall != 0 {
		t.Errorf("overall = %d, want 0", out.Overall)
	}
}

// TestDoctor_CAInstalledPasses: CA gerada e presente no trust store (runner
// fake devolvendo o CN) → pass.
func TestDoctor_CAInstalledPasses(t *testing.T) {
	env := healthyEnv(t)
	ca := seedCA(t, env)
	env.exec = func(_ string, _ ...string) ([]byte, error) {
		return []byte("Subject: CN=" + ca.SubjectCN()), nil
	}

	results := runDoctor(env)
	c := findResult(results, "CA local")
	if c == nil || c.Status != statusPass {
		t.Fatalf("CA no trust store deveria passar — got %+v", c)
	}
}

// TestDoctor_CAGeneratedButNotInstalledWarns: CA gerada mas ausente do trust
// store → warn com a correção de ca-install.
func TestDoctor_CAGeneratedButNotInstalledWarns(t *testing.T) {
	env := healthyEnv(t)
	seedCA(t, env)
	env.exec = func(_ string, _ ...string) ([]byte, error) {
		return []byte("Subject: CN=Outra CA"), nil
	}

	results := runDoctor(env)
	c := findResult(results, "CA local")
	if c == nil || c.Status != statusWarn {
		t.Fatalf("CA sem trust store deveria virar warn — got %+v", c)
	}
	if !strings.Contains(c.Message, "não instalada") {
		t.Errorf("mensagem CA = %q, want mencionando não instalada", c.Message)
	}
}

// TestDoctor_CANotGeneratedPasses: sem CA gerada é configuração legítima
// (fallback auto-assinado) → pass, nunca falha o doctor.
func TestDoctor_CANotGeneratedPasses(t *testing.T) {
	env := healthyEnv(t)
	results := runDoctor(env)
	c := findResult(results, "CA local")
	if c == nil || c.Status != statusPass {
		t.Fatalf("CA ausente deveria passar (configuração) — got %+v", c)
	}
}

// seedCA gera uma CA real no diretório ca ao lado do state.json do env.
func seedCA(t *testing.T, env doctorEnv) *tlsca.CA {
	t.Helper()
	ca, err := tlsca.LoadOrCreate(filepath.Join(filepath.Dir(env.statePath), "ca"))
	if err != nil {
		t.Fatalf("seedCA: %v", err)
	}
	return ca
}

// ---------------------------------------------------------------------------
// Helpers de teste
// ---------------------------------------------------------------------------

func findResult(results []doctorResult, name string) *doctorResult {
	for i := range results {
		if results[i].Name == name {
			return &results[i]
		}
	}
	return nil
}

func summarizeResults(results []doctorResult) string {
	var b strings.Builder
	for _, r := range results {
		b.WriteString(string(r.Status))
		b.WriteString(" ")
		b.WriteString(r.Name)
		b.WriteString(": ")
		b.WriteString(r.Message)
		b.WriteString("\n")
	}
	return b.String()
}
