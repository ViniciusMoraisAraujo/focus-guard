# Features Plan — FocusGuard

> Planejamento técnico das features do `task.md` (roadmap executivo). Este
> documento é a **fonte de verdade da implementação**: fases, pacotes
> afetados, contratos (IPC/UI), riscos e critérios de saída por feature.
>
> Convenções do repo (AGENT.md) que se aplicam a todas as fases: **TDD**
> (testes no mesmo commit), **stdlib-first** (evitar dependências novas),
> **best-effort** para falhas de SO, `_windows.go`/`_linux.go` por plataforma,
> mudança no contrato IPC exige atualizar CLI + tray + web + `types.ts` no
> mesmo commit (`make contract`), e **nada de "manual unblock"** sem decisão
> de produto.

---

## Ordem proposta e dependências

```
Fase 1 (fundação) ──→ Fase 2 (anti-burla) ──→ Fase 3 (interceptor page)
   │ doctor                │ NTP/scheduler         │ httpapi + enforcer + dnsserver
   │ telemetria sinkhole   └───────────────────────┤ (telemetria alimenta a página)
   └────────────────────────────────────────────────┘
Fase 4 (dispositivos Server) ── usa telemetria (IP de origem) + scheduler.IsBlocked
Fase 5 (engajamento) ── relatório semanal + gamificação (usam analytics)
```

Cada fase é **entregável isolada** (release própria), não bloqueia a seguinte.

**Status:** Fases 1–5 **entregues** (v0.17.0); a v0.18.0 fechou a última
limitação da Fase 3 (HTTPS com aviso de certificado — resolvido pela CA local,
ver seção Fase 3). Itens em aberto: seção [Itens em aberto](#itens-em-aberto)
no fim do documento (seleção do IP LAN no modo Server + pausa responsável).

---

## Fase 1 — Fundação: `doctor` + telemetria do sinkhole

### 1.1 `focusguard doctor` (CLI)

**Objetivo:** diagnóstico de instalação que reporta problemas com exit code.

**Pacotes afetados:**
- `cmd/focusguard/doctor.go` (novo) + entrada em `commands.go`/`usageOrder`
- Reusa: `internal/transport/ipc` (ping/status), `internal/infrastructure/enforcer`
  (varredura de regras), `internal/infrastructure/autostart` (consulta serviço)

**Checagens (cada uma com pass/fail/warn + sugestão de fix):**
1. **Elevação** — daemon exige admin; reportar se o shell não for elevado (não é bug, é info)
2. **Serviços** — `FocusGuard` e `FocusGuardWatchdog` instalados e rodando (Windows SCM / systemd)
3. **IPC** — ping ao daemon (socket acessível; no Linux checa grupo `focusguard`)
4. **Estado** — `state.json` presente, válido e gravável; réplicas ativas
5. **Firewall** — regras órfãs (não pertencem a bloco ativo) na varredura do enforcer
6. **hosts** — integridade vs RAM do daemon (via IPC ou leitura direta + heurística)
7. **Versões** — todos os binários irmãos na mesma versão (suíte mista = aviso)
8. **DNS** — status do sinkhole (ouvindo? bind error?)

**Contrato:** saída em texto PT-BR legível + `--json` (opcional) + exit code
`0` (ok) / `1` (problemas) / `2` (erro de execução). Sem ação IPC nova —
somente leitura.

**Riscos:** checagem de regras órfãs pode exigir privilégio — degrau para
"não consegui verificar" (warn), nunca falha o comando inteiro.

**Critérios de saída:** testes TDD do parser de saída/exit code com fake
IPC/enforcer; `go build`/`vet`/`gofmt` limpos; doctor rodando em Windows e Linux.

### 1.2 Telemetria do sinkhole

**Objetivo:** log persistente de queries bloqueadas (domínio + IP de origem +
timestamp) exposto no painel.

**Pacotes afetados:**
- `internal/infrastructure/dnsserver/server.go` — captura `w.RemoteAddr()` +
  domínio bloqueado em cada resposta sinkholed
- `internal/domain/telemetry` (novo) — recorder JSONL (padrão do analytics:
  append-only, linha corrompida é pulada) + consultas agregadas
- `internal/transport/ipc` — nova ação `dns-telemetry` (spec + handler) +
  `types.ts` via `make contract`
- `internal/transport/httpapi` — proxy da ação (automático pelo registry)
- `focusguard-ui` — seção na tela Rede ("Atividade bloqueada": domínio ×
  contagem × últimos IPs)

**Decisões de design:**
- Log **só bloqueadas** (volume baixo) ou todas as queries (com amostragem)? →
  Default: bloqueadas + erro de upstream; configurável.
- Rotação: cap de linhas/bytes (ex.: 10k linhas), purga no boot (best-effort).
- Privacidade: IPs de origem são dados da rede local — documentar no painel.

**Riscos:** volume alto sob ataque/scan — o cap + purga mitigam; nunca
bloquear o caminho do DNS por falha de escrita (best-effort).

**Critérios de saída:** TDD do recorder (append/rotação/leitura com linha
corrompida), handler IPC com spec, tela Rede exibindo agregação, `contract-check`.

---

## Fase 2 — Clock Tamper Protection (anti-fuso)

**Objetivo:** impedir que mudar a data do SO burle a expiração de bloqueios.

**Pacotes afetados:**
- `internal/infrastructure/ntp` (novo) — cliente NTP mínimo (UDP :123, stdlib)
- `internal/domain/scheduler` — timers + persistência de `LastKnownTime`
- `internal/domain/policy` — `IsActive`/`CanUnblock` cientes do gap detectado
- `internal/infrastructure/store` — campo `last_known_time` no state.json
- `internal/infrastructure/tamper` — registro da burla
- `cmd/focusguard-daemon` — worker periódico de validação NTP

**Design (corrigindo a proposta original):**

| Camada | Mecanismo | Detecta |
|---|---|---|
| Em processo | timers já monotônicos (`time.Until` sobre `ExpiresAt` vindo de `now.Add(d)`); comparações de expiração usam monotônico | mudança de data durante a execução |
| Persistido | `LastKnownTime` gravado a cada save; no boot (e periodicamente), gap do wall clock **nos dois sentidos** (`|now − lastKnown| > tolerância`, janela de graça p/ NTP/DST) | relógio voltou **ou adiantou** entre restarts (adiantar + reiniciar fazia o bloqueio expirar cedo) |
| Validação | NTP público (best-effort, timeout curto) confirma o horário real | confirmação da burla |

> Nota técnica: `time.Until(ExpiresAt)` já é monotônico em processo; a
> exposição real é o caminho **persistido/restart** (JSON descarta a leitura
> monotônica → wall clock) e `IsActive`/`CanUnblock` via `time.Now().After`.
> É exatamente isso que o `LastKnownTime` cobre.

**Comportamento em suspeita confirmada:**
1. Registrar no tamper-log (quando, gap detectado)
2. **Bloquear tudo preventivamente** (sentinela all-internet) até NTP validar
3. Ajustar expirações pendentes pelo gap **em qualquer direção** (re-persistir com o tempo correto)

**Decisões:**
- Tolerância (5 min) e timeout NTP (3s) como constantes testáveis
- NTP falha (sem rede) → manter suspeita, não liberar
- Servidor NTP default + upstream configurável via env/flag? → default fixo, sem UI

**Riscos:** falso positivo com ajuste legítimo de horário (DST/NTP do SO) — a
tolerância + janela de graça cobrem; documentar no painel ("hora ajustada").

**Critérios de saída:** TDD do NTP (mock do socket UDP), do gap detection
(clock fake injetável), do bloqueio preventivo e da re-persistência; sem
mudança de schema quebrada (campo novo é aditivo — `state.json` compatível).

---

## Fase 3 — Focus Interceptor Page

**Objetivo:** ao acessar um site bloqueado, mostrar uma página explicando o
bloqueio (domínio, motivo, tempo restante).

**Pacotes afetados:**
- `internal/transport/httpapi` — rota `/blocked` (template embutido) no desktop
- `internal/infrastructure/enforcer` — hosts passa a apontar bloqueados para
  `127.0.0.1` (desktop) em vez de `0.0.0.0` (só quando a feature está ativa)
- `internal/infrastructure/dnsserver` — responder A/AAAA com o IP do servidor
  (Server) em vez de `0.0.0.0`
- `cmd/focusguard-daemon` — listener HTTP :80 (Server, best-effort como o DNS)
- `internal/transport/ipc` — expor tempo restante por domínio (reusa `status`)

**Design:**
- **Listener :80 é a base dos dois modos** — o navegador acessa
  `http://dominio:80`, não a 48902 do `httpapi`. O daemon sobe um listener
  HTTP na porta 80 (loopback no Desktop, `0.0.0.0` no Server) que consulta o
  scheduler (via IPC ao daemon) e renderiza a página. Fallback: porta 80
  ocupada → loga e segue (bloqueio continua valendo, só sem página).
- **Desktop é dual-stack loopback** (`127.0.0.1:80` + `[::1]:80`): o hosts do
  enforcer escreve as duas entradas (IPv4 e IPv6) e navegadores modernos
  tentam IPv6 primeiro — sem o `[::1]` a conexão seria recusada no stack
  IPv6 antes do fallback para o IPv4, e a página não apareceria. Cada bind é
  best-effort independente (porta 80 ocupada em um stack só desativa aquele
  listener).
- **Desktop:** `hosts` → `127.0.0.1 <domínio>`; o listener :80 do daemon
  atende. A rota `/blocked?domain=X` no `httpapi` (48902) fica como
  visualização administrativa — cuidado com a validação de Host
  (anti-DNS-rebinding): o domínio bloqueado chega como Host, então tratar a
  rota com permissão para Hosts externos **somente** no modo interceptor.
- **Server:** DNS responde com o IP LAN do servidor; mesmo listener :80 serve
  a página para a rede.
- **Limitações documentadas:** HTTPS puro originalmente não exibia a página
  (erro de certificado) — resolvido pela **CA local** (`internal/infrastructure/tlsca`):
  o daemon gera uma CA persistente, assina os leafs do listener TLS com ela e
  a instala no trust store do SO (best-effort, daemon roda como SYSTEM/root),
  então a página HTTPS abre **sem aviso** em Chrome/Edge. Fallback: sem CA no
  store, o listener serve cert auto-assinado por SNI (usuário continua pelo
  aviso). Firefox usa trust store próprio — passo extra documentado
  (`security.enterprise_roots.enabled` ou importação manual; comandos
  `ca-install`/`ca-uninstall` no CLI e checagem no doctor). Ainda limitado:
  IP LAN precisa de regra de seleção clara (ex.: IP da rota default,
  configurável) — multi-NIC/VPN quebram a página.

**Riscos:** mexer no `IsBlocked`/resposta DNS é mudança de comportamento
visível — feature **desligada por default** (flag/config), TDD da resposta DNS
com IP local vs `0.0.0.0`.

**Critérios de saída:** TDD do handler `/blocked` (template renderiza, 404
sem domínio), do enforcer (127.0.0.1 quando ativo), do dnsserver (IP local
quando ativo), listener :80 best-effort com teste de bind falho.

---

## Fase 4 — Regras por dispositivo (edição Server)

**Objetivo:** políticas de bloqueio diferentes por dispositivo na rede.

**Pacotes afetados:**
- `internal/domain/devices` (novo) — store persistido (JSON ao lado do
  state.json) com políticas por IP/MAC
- `internal/domain/scheduler` — `IsBlocked(domain, clientIP)` (assinatura
  estendida; o handler do DNS já tem o `RemoteAddr`)
- `internal/infrastructure/dnsserver` — passar IP de origem ao checker
- `internal/transport/ipc` — ações `devices-list`/`devices-add`/`devices-remove`
  (specs + handlers) + `types.ts`
- `focusguard-ui` — tela Rede: lista de dispositivos + política (bloquear
  tudo / permitir lista / herdar global)

**Design:**
- Identificação: por IP (fixo/reserva DHCP) e por MAC (via tabela ARP,
  plataforma-específica, best-effort)
- Modelo de política por dispositivo: `block` (tudo), `allow` (lista de
  domínios), `inherit` (padrão global)
- Prioridade: dispositivo específico > global; allowlist do sentinel continua
  valendo

**Riscos:** MAC muda com VPN/roteador → IP como fonte primária; ARP é
best-effort (pode não achar MAC de dispositivos fora da LAN). Mudança na
assinatura `IsBlocked` afeta o scheduler e o dnsserver — testes existentes
atualizados no mesmo commit.

**Critérios de saída:** TDD do store (persistência), da resolução de política
(IP/MAC/inherit), do `IsBlocked` com cliente, IPC + UI + `contract-check`.

---

## Fase 5 — Engajamento: relatório semanal + gamificação

### 5.1 Relatório semanal automático

**Objetivo:** gerar o relatório semanal (já existe em `analytics`) de forma
agendada.

**Pacotes afetados:**
- `internal/domain/analytics` — expor export pronto (reuso)
- `cmd/focusguard-daemon` — agendador (padrão `startScheduleWorker`) que
  dispara no dia/hora configurados
- `internal/transport/ipc` — ação `report-generate` (spec) + `types.ts`
- `focusguard-ui` — Configurações: ativar/desativar, dia/hora, caminho

**Design:** grava HTML autossuficiente + JSON na pasta configurável; notifica
via tray (reuso do padrão pomodoro); falha de escrita é best-effort (log).

### 5.2 Gamificação (conquistas)

**Objetivo:** badges derivados das stats existentes.

**Pacotes afetados:**
- `internal/domain/achievements` (novo) — catálogo puro: `Achievements(stats)`
  retorna as desbloqueadas (sem persistência de "já ganhou")
- `internal/transport/ipc` — ação `achievements` (spec) + `types.ts`
- `focusguard-ui` — seção na tela Estatísticas com badges

**Design:** condições puras (streak ≥ 7, foco total ≥ Xh, missões ≥ N, meta
batida N dias seguidos); calculadas na leitura → sem migração de estado.

---

## Itens em aberto

> Próxima versão.

| Item | Origem | Ação pendente |
|---|---|---|
| **Seleção do IP LAN no modo Server** | Fase 3 | Regra de seleção clara do IP que o DNS responde na rede (ex.: IP da rota default, configurável) — com multi-NIC/VPN a página quebra. |
| **Pausa responsável (cooldown)** | Backlog | **Não entra sem decisão de produto** (AGENT.md). |

> Decisões de design já tomadas nas fases ficam registradas nas próprias
> seções (ex.: interceptor desligado por default, telemetria só de bloqueadas,
> `IsBlocked` com `clientIP`, NTP offline mantém suspeita).

## Check-list por fase (Definition of Done)

**Status:** Fases 1–5 entregues (v0.17.0) — o check-list abaixo foi cumprido;
a v0.18.0 fechou a limitação HTTPS da Fase 3 (CA local).

- [x] `go build ./...` / `go vet ./...` / `gofmt -l` limpos
- [x] `go test ./... -count=1` verde (TDD cobrindo a mudança)
- [x] IPC mudou → CLI + tray + web + `types.ts` no mesmo commit + `contract-check`
- [x] `state.json` mudou → campo **aditivo**, compatível com estado em disco
- [x] UI mudou → `make ui` + `make contract`
- [x] Binários de plataforma novos → `_windows.go`/`_linux.go` com interface no base
