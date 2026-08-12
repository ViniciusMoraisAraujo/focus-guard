// Package tlsca mantém a CA local do FocusGuard: uma âncora de confiança
// gerada uma única vez e persistida ao lado do state.json. O interceptor usa
// essa CA para assinar os certificados da página de bloqueio (em vez de
// certificados auto-assinados por SNI), e — quando a CA é instalada no trust
// store do sistema — o navegador abre a página HTTPS **sem** o aviso de
// "conexão não segura".
//
// Segurança: a chave privada da CA é uma âncora de confiança da máquina —
// quem a possuir consegue se passar por qualquer site. Por isso o LoadOrCreate
// restringe as permissões do arquivo da chave (0600 no Linux; no Windows o
// mode POSIX é fictício e a proteção real vem das ACLs do diretório
// %PROGRAMDATA%\FocusGuard — restrito a SYSTEM/Administradores, onde o daemon
// roda), a instalação no trust store é opt-in/best-effort e documentada, e a
// remoção (uninstall) a tira do store.
package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"focusguard/internal/infrastructure/lru"
)

const (
	// caCertFile e caKeyFile são os nomes dos artefatos persistidos no
	// diretório da CA (ao lado do state.json).
	caCertFile = "focusguard-ca.crt"
	caKeyFile  = "focusguard-ca.key"

	// commonName é o assunto da CA (e o rótulo usado na detecção/remoção do
	// trust store).
	commonName = "FocusGuard Local CA"

	// caValidity é a validade da CA (10 anos) — geração é única e persistida;
	// a renovação antes do vencimento fica para um futuro ciclo.
	caValidity = 10 * 365 * 24 * time.Hour
)

// leafCacheMax é o teto do cache de leafs por hostname (LRU). Leafs são
// regeneráveis a qualquer momento (signLeaf é barato) — o teto só impede que
// SNIs arbitrários de um listener Server (0.0.0.0:443) encham a memória
// (pendência INFO do docs/verification-plan.md). Var (não const) para os
// testes baixarem o teto sem gerar centenas de leafs.
var leafCacheMax = 1024

// CA é a autoridade certificadora local: chave + certificado persistidos e um
// cache de leafs assinados por hostname (mesmo cache por-SNI do interceptor,
// agora assinado pela CA). O cache é um LRU com teto (leafCacheMax) — leafs
// são regeneráveis, o teto só protege a memória contra SNIs arbitrários.
// Zero-value não é utilizável; construa com LoadOrCreate.
type CA struct {
	dir string
	key *ecdsa.PrivateKey
	crt *x509.Certificate

	mu    sync.Mutex
	leafs *lru.Cache[*tls.Certificate]
}

// LoadOrCreate carrega a CA persistida em dir ou a gera na primeira execução.
// Idempotente e seguro para chamar em todo boot do daemon: uma CA existente e
// SADIA é reutilizada (nunca regenerada — regenerar invalidaria os
// certificados já instalados no trust store). Uma CA persistida mas
// inutilizável (cert sem key — escrita da key falhou no 1º boot; PEM
// corrompido; ou par descasado key↔cert) é tratada como lixo e REGENERADA:
// uma âncora quebrada não pode travar o boot para sempre nem impedir a
// página HTTPS de voltar a funcionar. A chave é gravada com 0600.
func LoadOrCreate(dir string) (*CA, error) {
	if dir == "" {
		return nil, fmt.Errorf("tlsca: diretório da CA vazio")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tlsca: criar diretório %s: %w", dir, err)
	}

	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	certPEM, err := os.ReadFile(certPath)
	if err == nil {
		if keyPEM, kerr := os.ReadFile(keyPath); kerr == nil {
			ca, lerr := loadCA(dir, certPEM, keyPEM)
			if lerr == nil {
				return ca, nil
			}
			// CA persistida, mas ilegível ou descasada: descarta os artefatos
			// e regenera (ver loadCA — o par key↔cert é verificado).
			removeCAArtifacts(certPath, keyPath)
			return generateCA(dir, certPath, keyPath)
		}
		// Cert órfão (sem key): sobra de uma geração que falhou no meio. A CA
		// nunca chegou a ser instalada no trust store — descarta o cert e
		// regenera.
		_ = os.Remove(certPath)
		return generateCA(dir, certPath, keyPath)
	}

	// Primeira execução (ou geração nunca concluída).
	ca, gerr := generateCA(dir, certPath, keyPath)
	if gerr != nil {
		return nil, gerr
	}
	return ca, nil
}

// removeCAArtifacts apaga os artefatos da CA antes de uma regeneração
// (best-effort: um Remove que falhe não impede o generateCA, que sobrescreve
// os arquivos de qualquer forma).
func removeCAArtifacts(certPath, keyPath string) {
	_ = os.Remove(certPath)
	_ = os.Remove(keyPath)
}

// generateCA gera uma CA ECDSA P-256 nova, grava os artefatos e devolve o CA
// carregado. Os tempos usam time.Now (testes de expiração usam o relógio real
// com folga de validade de 10 anos).
func generateCA(dir, certPath, keyPath string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsca: gerar chave da CA: %w", err)
	}

	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("tlsca: serial da CA: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"FocusGuard"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("tlsca: criar certificado da CA: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("tlsca: serializar chave da CA: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("tlsca: gravar %s: %w", certPath, err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("tlsca: gravar %s: %w", keyPath, err)
	}
	return loadCA(dir, certPEM, keyPEM)
}

// loadCA interpreta os PEMs persistidos e devolve o CA carregado.
func loadCA(dir string, certPEM, keyPEM []byte) (*CA, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("tlsca: %s não é um certificado PEM válido", filepath.Join(dir, caCertFile))
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("tlsca: %s não é uma chave PEM válida", filepath.Join(dir, caKeyFile))
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("tlsca: chave da CA inválida: %w", err)
	}
	crt, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("tlsca: certificado da CA inválido: %w", err)
	}
	// A chave precisa corresponder ao certificado: um par descasado assinaria
	// leafs que nenhum cliente valida contra a CA (chain quebrada). O
	// LoadOrCreate trata esse caso como CA corrompida e regenera.
	pub, ok := crt.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(key.Public()) {
		return nil, fmt.Errorf("tlsca: chave não corresponde ao certificado da CA")
	}
	return &CA{dir: dir, key: key, crt: crt, leafs: lru.New[*tls.Certificate](leafCacheMax)}, nil
}

// Exists reporta se uma CA já está persistida em dir — sem efeitos colaterais
// (não gera nada). Usado pelo doctor para diagnosticar sem criar artefatos.
func Exists(dir string) bool {
	if dir == "" {
		return false
	}
	_, err1 := os.Stat(filepath.Join(dir, caCertFile))
	_, err2 := os.Stat(filepath.Join(dir, caKeyFile))
	return err1 == nil && err2 == nil
}

// CleanupTempCER remove o arquivo .cer temporário órfão do diretório da CA.
// O installIntoStore (Windows) grava o cert como <caCertFile>.cer ao lado do
// PEM para o certutil consumir e o remove com defer — se o processo morrer
// entre a escrita e o defer (crash/kill no meio da instalação), o .cer fica
// para trás. Este passo é best-effort e cirúrgico: remove SOMENTE o nome
// exato do temp (caCertFile+".cer"), nunca outros arquivos do diretório
// (nem um .cer legítimo que um dia venha a existir, ex.: export para o
// Firefox). O daemon o chama no boot (ensureCA); no Linux o arquivo nunca
// existe e o Remove é no-op.
func CleanupTempCER(dir string) {
	if dir == "" {
		return
	}
	_ = os.Remove(filepath.Join(dir, caCertFile+".cer"))
}

// CertPEM devolve o certificado da CA em PEM (o artefato a instalar no trust
// store do sistema).
func (c *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.crt.Raw})
}

// SubjectCN devolve o nome comum da CA ("FocusGuard Local CA") — usado na
// detecção/remoção no trust store.
func (c *CA) SubjectCN() string {
	return c.crt.Subject.CommonName
}

// SerialHex devolve o serial da CA em hex (sem prefixos) — a identidade
// ESTÁVEL entre gerações usada na detecção do trust store no Windows
// (store_windows.go): o CN é constante entre CAs regeneradas, o serial não.
func (c *CA) SerialHex() string {
	return c.crt.SerialNumber.Text(16)
}

// LeafFor devolve (gerando e cacheando) um certificado de servidor assinado
// pela CA, com SAN cobrindo host (nome DNS ou IP). Cacheado por hostname em
// um LRU com teto — cada domínio bloqueado tem um cert estável, reutilizado
// entre conexões, e um flood de SNIs não cresce a memória (o LRU evicta os
// menos recentes; o próximo handshake regenera).
func (c *CA) LeafFor(host string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cert, ok := c.leafs.Get(host); ok {
		return cert, nil
	}
	cert, err := c.signLeaf(host, time.Now())
	if err != nil {
		return nil, err
	}
	c.leafs.Set(host, cert)
	return cert, nil
}

// signLeaf monta um leaf ECDSA P-256 assinado pela CA com validade de 1 ano —
// folga confortável para o interceptor (cert efêmero por domínio; renovação
// automática a cada geração/reboot do daemon). A SAN cobre o host exato (DNS
// name ou endereço IP).
func (c *CA) signLeaf(host string, now time.Time) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, err
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"FocusGuard"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, c.crt, &key.PublicKey, c.key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	// A chain inclui o leaf + a CA: o navegador valida o leaf pela âncora do
	// trust store sem precisar de intermediates.
	pair.Certificate = [][]byte{der, c.crt.Raw}
	return &pair, nil
}
