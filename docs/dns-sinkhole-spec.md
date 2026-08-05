```markdown
# 🛡️ FocusGuard Server — Especificação Técnica (DNS Sinkhole)

## 📌 1. Ideia Geral
[cite_start]O **FocusGuard Server** evolui a arquitetura do projeto de um bloqueador local (*Desktop Self-Blocker*) para um **DNS Sinkhole de rede inteira**[cite: 3, 2228]. [cite_start]Inspirado em soluções consagradas de infraestrutura como Pi-hole e AdGuard Home [cite: 5, 2229][cite_start], o sistema passa a atuar como o **Servidor DNS primário da rede local**[cite: 5, 2229].

[cite_start]Em vez de focar apenas na modificação do arquivo `C:\Windows\System32\drivers\etc\hosts` ou regras de firewall máquina a máquina (`netsh`) [cite: 6, 2230][cite_start], o FocusGuard Server escuta requisições na porta **53 (UDP/TCP)** em todas as interfaces da máquina (`0.0.0.0:53`)[cite: 6, 2230]. [cite_start]Qualquer dispositivo conectado ao Wi-Fi ou cabo (celulares, Smart TVs, tablets, notebooks corporativos) passa a ter suas consultas de resolução de nomes filtradas centralizadamente[cite: 7, 2231].

---

## 🏗️ 2. Arquitetura Proposta

### 🔄 Fluxo de Resolução e Interceptação


```

[Dispositivo Cliente]
(Celular / TV / PC)
│
│ 1. Consulta DNS (ex: instagram.com)
▼
┌────────────────────────────────────────────────────────┐
│               FocusGuard Server (Go)                   │
│                    (0.0.0.0:53)                        │
└───────────────────────┬────────────────────────────────┘
│
▼
2. Checagem na RAM (RAM Map)
(Política ativa / Scheduler)
│
┌───────────────┴───────────────┐
▼                               ▼
[Domínio BLOQUEADO]          [Domínio PERMITIDO]
│                               │
│ 3a. Resposta Imediata         │ 3b. Repassa Consulta
▼                               ▼
[0.0.0.0 / ::]             [Upstream DNS Seguro]
(Acesso Negado - Sinkhole)    (Cloudflare 1.1.1.2 / Quad9 9.9.9.9)
│                               │
└───────────────┬───────────────┘
│
▼
4. Resposta ao Cliente (Success / Status: OK)

```

### 🧩 Estrutura de Módulos no Ecossistema Go

* [cite_start]**`internal/dnsserver`**: Responsável por subir o listener na porta 53 (UDP/TCP) utilizando a biblioteca `miekg/dns`[cite: 9, 2232].
* [cite_start]**`internal/policy`**: Motor de regras puras em memória RAM que avalia a política de bloqueio e expiração dos prazos[cite: 10, 2233].
* [cite_start]**`internal/store`**: Gerencia a gravação e leitura atômica em disco do arquivo `state.json`[cite: 11, 2234].
* [cite_start]**`internal/scheduler`**: Gerencia o ciclo de vida dos bloqueios temporizados, workers periódicos de CDN e o mapa de blocos mantido na memória RAM[cite: 12, 2235].

---

## 🛠️ 3. Implementação Técnica

### 3.1. Módulo Servidor DNS em Go (`internal/dnsserver`)
[cite_start]Utiliza a biblioteca padrão da comunidade `miekg/dns` para manipular pacotes nativos do protocolo DNS[cite: 13, 2236].

```go
package dnsserver

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"[github.com/miekg/dns](https://github.com/miekg/dns)"
)

type PolicyChecker interface {
	IsBlocked(domain string) bool
}

type Server struct {
	udpServer *dns.Server
	tcpServer *dns.Server
	upstream  string
	checker   PolicyChecker
	mu        sync.RWMutex
}

func New(bindAddr string, upstreamDNS string, checker PolicyChecker) *Server {
	return &Server{
		upstream: upstreamDNS,
		checker:  checker,
	}
}

func (s *Server) Start(port int) error {
	dns.HandleFunc(".", s.handleDNSRequest)

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	s.udpServer = &dns.Server{Addr: addr, Net: "udp"}
	s.tcpServer = &dns.Server{Addr: addr, Net: "tcp"}

	go func() {
		if err := s.udpServer.ListenAndServe(); err != nil {
			fmt.Printf("Falha no servidor UDP DNS: %v\n", err)
		}
	}()

	return s.tcpServer.ListenAndServe()
}

func (s *Server) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	if r.Opcode != dns.OpcodeQuery {
		_ = w.WriteMsg(m)
		return
	}

	for _, q := range r.Question {
		domain := strings.TrimSuffix(q.Name, ".")

		// 1. Checagem de Bloqueio em RAM (Sinkhole)
		if s.checker.IsBlocked(domain) {
			if q.Qtype == dns.TypeA {
				rr, _ := dns.NewRR(fmt.Sprintf("%s 60 IN A 0.0.0.0", q.Name))
				m.Answer = append(m.Answer, rr)
			} else if q.Qtype == dns.TypeAAAA {
				rr, _ := dns.NewRR(fmt.Sprintf("%s 60 IN AAAA ::", q.Name))
				m.Answer = append(m.Answer, rr)
			}
			_ = w.WriteMsg(m)
			return
		}

		// 2. Encaminhamento para Upstream (Se permitido)
		resp, err := dns.Exchange(r, s.upstream)
		if err == nil {
			_ = w.WriteMsg(resp)
			return
		}
	}

	_ = w.WriteMsg(m)
}

```

### 3.2. Estratégia de Configuração "Rei da Rede" no Windows / Roteador

Para proteger toda a rede local **sem precisar passar o IP manual em cada celular/dispositivo cliente**:

1. 
**IP Estático no PC Windows (DHCP Static Lease)**: Acesse o painel do roteador/modem e fixe o IP local do computador Windows que executa o FocusGuard (ex: `192.168.1.100`).


2. 
**DNS Primário no DHCP do Roteador**: Altere o DNS distribuído pelo servidor DHCP do roteador para o IP do seu PC (`192.168.1.100`).


3. 
**DNS Secundário (Failover de Alta Disponibilidade)**: Configure um DNS público de confiança (ex: Cloudflare `1.1.1.1` ou Google `8.8.8.8`) como o **DNS Secundário** no roteador.


* 
*Funcionamento*: Quando o PC Windows estiver ligado com o FocusGuard, ele responde às requisições em $<1\text{ms}$. Se o PC for desligado, os dispositivos da casa passam a responder pelo DNS Secundário automaticamente sem interromper a navegação da rede.





---

## 🐛 4. Possíveis Bugs e Armadilhas Conhecidas (Foco no Windows)

### 1. A Armadilha do DNS Secundário (Vazamento do Bloqueio)

* 
**O Bug**: Se o FocusGuard devolver erro de conexão (`SERVFAIL`, `REFUSED`) ou simplesmente ignorar o pacote (`DROP`) para um site bloqueado, os sistemas operacionais dos celulares (Android/iOS) tentarão consultar o **DNS Secundário** cadastrado no roteador imediatamente, furando o bloqueio.


* 
**A Solução**: O FocusGuard **nunca deve retornar erro** em domínios bloqueados. Ele DEVE responder com **Status: OK / Success**, entregando ativamente o IP inválido `0.0.0.0` (para IPv4) ou `::` (para IPv6). O dispositivo cliente interpreta que a resposta é legítima e não aciona o DNS secundário.



### 2. Conflito de Porta 53 no Windows (`dnscache` e `Internet Connection Sharing`)

* 
**O Bug**: Ao tentar executar o FocusGuard Server na porta 53 no Windows, o Go pode retornar o erro `bind: An attempt was made to access a socket in a way forbidden by its access permissions`.


* 
**A Causa**: Serviços do Windows como o *Internet Connection Sharing (ICS)* ou instâncias do serviço DNS nativo do Windows Server (`dns.exe`) podem estar ocupando a porta 53 em modo exclusivo.


* 
**A Solução**: Garantir que o serviço ICS esteja desativado e configurar o socket em Go com a opção `SO_REUSEADDR` usando chamadas nativas do Windows (`sys/windows`).



### 3. Cache DNS Local dos Dispositivos Móveis (TTL)

* 
**O Bug**: Ao ativar um bloqueio, celulares conectados ao Wi-Fi podem demorar de 30 segundos a 2 minutos para interromper o acesso.


* 
**A Causa**: Os sistemas operacionais móbiles mantêm uma tabela de cache local baseada no campo TTL (*Time To Live*) retornado pelas consultas anteriores.


* 
**Mitigação**: O FocusGuard Server deve injetar um **TTL curto (ex: 60 segundos)** em todas as respostas de autoridade DNS.



### 4. Bypass por DNS-over-HTTPS (DoH), DoT e QUIC (HTTP/3) no Navegador

* 
**O Bug**: Navegadores como Firefox e Chrome tentam ignorar o servidor DNS da rede local enviando consultas criptografadas diretamente para resolvedores externos.


* 
**A Solução**: O módulo `enforcer` do FocusGuard no Windows deve injetar regras de firewall (`netsh`) para:


* Bloquear tráfego de saída na porta **TCP/UDP 853** (DNS-over-TLS).


* Bloquear tráfego de saída na porta **UDP 443** (Protocolo QUIC/HTTP/3).


* Bloquear conexões de saída para os IPs públicos conhecidos de provedores de DoH (ex: Cloudflare `1.1.1.1`, Google `8.8.8.8`).





---

## ⚠️ 5. Pontos de Atenção Críticos

> 🚨 **Disponibilidade e Deadlocks no Windows Service**:
> Ao atuar como DNS da rede inteira, se o processo em Go entrar em *deadlock* ou sofrer um *panic*, **a internet de toda a casa/empresa cai**.
> * **Requisito Obrigatório**: O FocusGuard Server no Windows deve ser configurado via Service Control Manager (`sc.exe failure FocusGuard reset= 86400 actions= restart/1000/restart/1000/restart/1000`) para ressuscitar em no máximo 1 segundo caso o processo feche. Além disso, deve possuir rotinas de `panic recovery` (*recover handlers*) em cada goroutine de escuta da porta 53.
> 
> 

> 🔒 **Privilégios de Administrador no Windows**:
> * A escuta em portas baixas (como a porta 53) e a modificação de regras de firewall via `netsh` no Windows exigem que o executável seja rodado com privilégios de **Administrador / SYSTEM**.
> * Se o serviço for instalado sob a conta `NT AUTHORITY\SYSTEM`, ele ganhará imunidade contra tentativas simples de encerramento via gerenciador de tarefas ou terminal sem elevação.
> 
> 

> 🌐 **Domain Bundling Automático**:
> Bloquear apenas o domínio principal informado (ex: `youtube.com`) falhará em barrar a navegação na rede. O servidor DNS precisa expandir a regra dinamicamente para os servidores de mídia, CDN e APIs associados (ex: `googlevideo.com`, `ytimg.com`, `youtubei.googleapis.com`).
> 
> 

---

## 🚀 6. Roadmap de Recursos Avançados (B2B / Security)

1. 
**Upstreams Filtrados de Segurança**: Encaminhar requisições permitidas para provedores com filtro nativo contra ameaças, como Cloudflare Security (`1.1.1.2`) ou Quad9 (`9.9.9.9`), bloqueando vírus, malware e *phishing* na origem.


2. 
**Wildcards e Bloqueio por TLD**: Capacidade de barrar extensões de domínios inteiras de uma vez (ex: `*.bet`, `*.casino`) diretamente na camada de resolução de nomes em memória RAM.


3. 
**Assinatura de Feeds Públicos de Risco**: Download e higienização diária automatizada de listas de ameaças mantidas pela comunidade (ex: StevenBlack, URLHaus/PhishTank) mantidas em mapas otimizados na RAM.



```

```