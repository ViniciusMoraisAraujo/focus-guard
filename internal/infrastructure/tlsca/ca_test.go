package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// newTestCA cria uma CA num diretório temporário (a fonte da verdade dos
// testes: LoadOrCreate idempotente, leafs assinados, persistência).
func newTestCA(t *testing.T) *CA {
	t.Helper()
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	return ca
}

func TestLoadOrCreate_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if ca.SubjectCN() != commonName {
		t.Errorf("SubjectCN = %q, want %q", ca.SubjectCN(), commonName)
	}
	for _, f := range []string{caCertFile, caKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("artefato %s não persistido: %v", f, err)
		}
	}
	// Chave com 0600 no Linux (âncora de confiança — não pode ser legível por
	// outros). No Windows o POSIX mode é fictício (sempre 666) — o controle de
	// acesso real é via ACLs do NTFS.
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, caKeyFile))
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissão da chave = %o, want 600", perm)
		}
	}
}

func TestLoadOrCreate_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ca1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	ca2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ca1.CertPEM(), ca2.CertPEM(); string(got) != string(want) {
		t.Error("LoadOrCreate regenerou a CA existente (cert diferente)")
	}
}

func TestLoadOrCreate_EmptyDirFails(t *testing.T) {
	if _, err := LoadOrCreate(""); err == nil {
		t.Error("LoadOrCreate(\"\") deveria falhar")
	}
}

func TestLeafFor_SignsWithCASAN(t *testing.T) {
	ca := newTestCA(t)
	cert, err := ca.LeafFor("youtube.com")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "youtube.com" {
		t.Errorf("SAN DNS = %v, want [youtube.com]", leaf.DNSNames)
	}
	if leaf.IsCA {
		t.Error("leaf não deveria ser CA")
	}

	// O leaf é assinado PELA CA (não auto-assinado): verifica a assinatura
	// com a chave pública da CA.
	if err := leaf.CheckSignatureFrom(ca.crt); err != nil {
		t.Errorf("leaf não assinado pela CA: %v", err)
	}

	// A chain carrega leaf + CA — o navegador fecha o caminho sem
	// intermediates.
	if len(cert.Certificate) != 2 {
		t.Errorf("chain com %d certs, want 2 (leaf + CA)", len(cert.Certificate))
	}
}

func TestLeafFor_IPHostGetsIPSAN(t *testing.T) {
	ca := newTestCA(t)
	cert, err := ca.LeafFor("192.168.0.5")
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "192.168.0.5" {
		t.Errorf("SAN IP = %v, want [192.168.0.5]", leaf.IPAddresses)
	}
	if len(leaf.DNSNames) != 0 {
		t.Errorf("host IP não deveria ter SAN DNS: %v", leaf.DNSNames)
	}
}

func TestLeafFor_IsCached(t *testing.T) {
	ca := newTestCA(t)
	a, err := ca.LeafFor("instagram.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ca.LeafFor("instagram.com")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("LeafFor deveria cachear o mesmo *tls.Certificate por host")
	}
}

// TestLeafFor_ChainValidatesAgainstCA prova o contrato do navegador: com a CA
// no trust store, um cliente que confia nela valida o leaf sem warning — a
// validação completa (x509.Verify com a CA como raiz) precisa passar.
func TestLeafFor_ChainValidatesAgainstCA(t *testing.T) {
	ca := newTestCA(t)
	leafCert, err := ca.LeafFor("youtube.com")
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(leafCert.Certificate[0])

	roots := x509.NewCertPool()
	roots.AddCert(ca.crt)
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName: "youtube.com",
		Roots:   roots,
		KeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}); err != nil {
		t.Errorf("leaf não valida contra a CA no trust store: %v", err)
	}
}

// TestCleanupTempCER_RemovesOrphans: .cer órfão (crash do certutil no boot
// anterior) é removido; os artefatos reais da CA (.crt/.key) ficam intactos.
func TestCleanupTempCER_RemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simula o órfão: um .cer ao lado dos artefatos (o installIntoStore escreve
	// CertPath()+".cer" antes do certutil).
	orphan := ca.CertPath() + ".cer"
	if err := os.WriteFile(orphan, ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}

	CleanupTempCER(dir)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("arquivo .cer órfão deveria ter sido removido")
	}
	for _, f := range []string{caCertFile, caKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("artefato real %s não deveria ser tocado: %v", f, err)
		}
	}
}

// TestCleanupTempCER_NoCERNoOp: diretório sem .cer (Linux, por exemplo) é um
// no-op silencioso.
func TestCleanupTempCER_NoCERNoOp(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	CleanupTempCER(dir) // não deve panico nem apagar os artefatos
	if !Exists(dir) {
		t.Error("CleanupTempCER não deveria afetar a CA persistida")
	}
}

// TestCleanupTempCER_EmptyDirNoOp: diretório vazio/vazio string é no-op.
func TestCleanupTempCER_EmptyDirNoOp(t *testing.T) {
	CleanupTempCER("") // não deve panico
}

func TestCertPEM_RoundTrips(t *testing.T) {
	ca := newTestCA(t)
	block, rest := pem.Decode(ca.CertPEM())
	if block == nil || len(rest) != 0 {
		t.Fatal("CertPEM não é um único bloco PEM")
	}
	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if crt.Subject.CommonName != commonName || !crt.IsCA {
		t.Errorf("cert parseado = CN %q IsCA %v", crt.Subject.CommonName, crt.IsCA)
	}
}

func TestLeafFor_NotExpired(t *testing.T) {
	ca := newTestCA(t)
	cert, err := ca.LeafFor("x.com")
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	if now := time.Now(); leaf.NotBefore.After(now) || leaf.NotAfter.Before(now.AddDate(0, 11, 0)) {
		t.Errorf("validade do leaf fora do esperado: %v → %v", leaf.NotBefore, leaf.NotAfter)
	}
}

// TestLoadOrCreate_OrphanCertWithoutKey_Regenerates: um cert sem a key (a
// escrita da key falhou no 1º boot — a CA nunca chegou a ser instalada no
// trust store) não pode travar o boot para sempre com um erro duro: o cert
// órfão é descartado e a CA é regenerada (TDD do fix de robustez do
// v0.18.1).
func TestLoadOrCreate_OrphanCertWithoutKey_Regenerates(t *testing.T) {
	dir := t.TempDir()
	orig, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, caKeyFile)); err != nil {
		t.Fatal(err)
	}

	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate com cert órfão deveria regenerar, não errar: %v", err)
	}
	for _, f := range []string{caCertFile, caKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("artefato %s ausente após a regeneração: %v", f, err)
		}
	}
	if string(ca.CertPEM()) == string(orig.CertPEM()) {
		t.Error("LoadOrCreate reutilizou o cert órfão em vez de regenerar")
	}
	if _, err := ca.LeafFor("youtube.com"); err != nil {
		t.Errorf("CA regenerada não assina leafs: %v", err)
	}
}

// TestLoadOrCreate_CorruptKeyPair_Regenerates: key presente mas ilegível
// (PEM inválido — arquivo corrompido) também se auto-cura.
func TestLoadOrCreate_CorruptKeyPair_Regenerates(t *testing.T) {
	dir := t.TempDir()
	orig, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), []byte("lixo que não é PEM"), 0o600); err != nil {
		t.Fatal(err)
	}

	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate com key corrompida deveria regenerar: %v", err)
	}
	if string(ca.CertPEM()) == string(orig.CertPEM()) {
		t.Error("LoadOrCreate reutilizou o par corrompido em vez de regenerar")
	}
}

// TestLoadOrCreate_MismatchedKey_Regenerates: key válida mas de OUTRA chave
// (par descasado — leafs assinados que nenhum cliente valida contra a CA,
// chain quebrada) também se auto-cura.
func TestLoadOrCreate_MismatchedKey_Regenerates(t *testing.T) {
	dir := t.TempDir()
	orig, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(other)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, caKeyFile), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate com key descasada deveria regenerar: %v", err)
	}
	if string(ca.CertPEM()) == string(orig.CertPEM()) {
		t.Error("LoadOrCreate reutilizou o par descasado em vez de regenerar")
	}
	// A regeneração precisa produzir um par coerente (key ↔ cert): um leaf
	// assinado valida contra a CA do próprio par.
	if !ca.key.Public().(*ecdsa.PublicKey).Equal(ca.crt.PublicKey.(*ecdsa.PublicKey)) {
		t.Error("par regenerado continua descasado (key não corresponde ao cert)")
	}
}

func TestTLSHandshakeServesCASignedLeaf(t *testing.T) {
	ca := newTestCA(t)
	// Exercita o par como o tls.Config do interceptor faria: o leaf é o
	// primeiro da chain e casa com o SNI.
	pair, err := ca.LeafFor("blocked.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Certificate) == 0 {
		t.Fatal("par sem chain")
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "blocked.example" {
		t.Errorf("leaf do handshake = %v, want [blocked.example]", leaf.DNSNames)
	}
}
