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
- **Limitações documentadas:** HTTPS puro não exibe a página (erro de
  certificado) — a página aparece para HTTP e para o fluxo
  hosts/sinkhole-tradicional; e o IP LAN precisa de regra de seleção clara
  (ex.: IP da rota default, configurável) — multi-NIC/VPN quebram a página.

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

## Riscos transversais e decisões pendentes

| Item | Decisão | Impacto |
|---|---|---|
| Interceptor page ligada por default? | **Não** (flag) — muda resposta do DNS/hosts | Fase 3 |
| Log de telemetria: só bloqueadas ou todas? | Só bloqueadas (+ erro upstream) | Fase 1.2 |
| Pausa responsável (cooldown) | **Não entra sem decisão de produto** (AGENT.md) | Backlog |
| `IsBlocked` ganha `clientIP` | Quebra de assinatura — testes existentes no mesmo commit | Fase 4 |
| NTP falha (offline) | Suspeita mantida, bloqueio preventivo permanece | Fase 2 |

## Check-list por fase (Definition of Done)

- [ ] `go build ./...` / `go vet ./...` / `gofmt -l` limpos
- [ ] `go test ./... -count=1` verde (TDD cobrindo a mudança)
- [ ] IPC mudou → CLI + tray + web + `types.ts` no mesmo commit + `contract-check`
- [ ] `state.json` mudou → campo **aditivo**, compatível com estado em disco
- [ ] UI mudou → `make ui` + `make contract`
- [ ] Binários de plataforma novos → `_windows.go`/`_linux.go` com interface no base



Fase 1 — Fundação: doctor + Telemetria
1.1 focusguard doctor (CLI)

Objetivo: Uma ferramenta de diagnóstico de linha de comando que verifica a saúde da instalação e reporta problemas.

Backend (Go - cmd/focusguard/doctor.go):

    Implementação: Criar uma cadeia de verificações (type Check func() Result).

    Verificações Específicas:

        Elevação: os.Getuid() == 0 (Linux) ou checagem de Token (Windows via _windows.go).

        Serviços: Usar os/exec para systemctl is-active focusguard (Linux) ou sc query focusguard (Windows).

        IPC Ping: Tentar conectar no socket IPC local.

        State.json: os.Stat e tentar os.OpenFile com permissão de escrita.

        Enforcer: Ler regras do firewall/hosts e comparar com regras em memória.

    Contrato de Saída:

        Texto padrão: [ PASS ], [ WARN ], [ FAIL ] com cores do terminal.

        --json: Saída estruturada {"checks": [{"name": "IPC", "status": "pass", "message": "..."}], "overall_status": 1}.

    TDD: Mockar o sistema de arquivos, chamadas de SO (usar interfaces OSExecuter) e cliente IPC para testar as falhas sem depender do ambiente real.

1.2 Telemetria do sinkhole

Objetivo: Registrar domínios bloqueados localmente para visualização no painel.

Backend (Go - internal/domain/telemetry & dnsserver):

    Armazenamento: Arquivo telemetry.jsonl (JSON Lines). Tamanho máximo de 1MB (~10k linhas). Ao exceder, rotaciona para telemetry.old.jsonl e limpa o atual (best-effort em concorrência).

    Interceptação: No server.go do DNS, se IsBlocked retornar true, criar struct BlockedQuery{Domain, ClientIP, Timestamp} e enviar para um canal não-bloqueante lido pelo worker de telemetria.

    IPC Contract (types.ts):
    TypeScript

    // Action: "dns-telemetry"
    type TelemetryRequest = { limit?: number };
    type TelemetryResponse = {
      entries: Array<{ domain: string, ip: string, timestamp: string }>;
      total_blocked: number;
    };

    Frontend (React/shadcn):

        Nova aba/seção em "Rede".

        Componentes: Card contendo um Table (shadcn) ou ScrollArea com a lista.

        Atualização a cada 5 segundos (polling silencioso via IPC) se a aba estiver aberta.

    TDD: Escrever no arquivo JSONL, simular uma linha truncada/corrompida no meio do arquivo e garantir que o leitor a ignore sem falhar a requisição IPC.

Fase 2 — Clock Tamper Protection (Anti-Fuso)

Objetivo: Evitar que usuários alterem a data/hora do sistema operacional para expirar bloqueios prematuramente.

Backend (Go - internal/infrastructure/ntp & scheduler):

    Cliente NTP Mínimo: Criar um cliente UDP puro na porta 123 buscando de pool.ntp.org. Timeout de 3 segundos.

    Lógica de Detecção:

        No state.json, adicionar last_known_time (timestamp Unix). Aditivo, não quebra versões antigas.

        No startup e a cada 10 minutos (worker), comparar time.Now() com last_known_time.

        Se math.Abs(time.Now().Sub(last_known_time)) > 5 * time.Minute, acionar Suspeita de Burla.

    Ação de Mitigação (Lockdown):

        Se suspeita: Fazer requisição NTP.

        Se NTP confirmar a burla: Gravar no log de tamper, recalcular o ExpiresAt dos timers ativos somando/subtraindo o delta, e atualizar last_known_time.

        Se NTP falhar (sem internet): Manter o estado de "bloqueio preventivo de segurança" (sentinela network-wide block) até a internet voltar e o NTP validar.

    Frontend:

        Se Lockdown ativo: Exibir um Alert destrutivo (shadcn) no topo do Dashboard: "Inconsistência de relógio detectada. Bloqueio preventivo ativado até sincronização online."

    TDD: Criar um mock TimeProvider interface. Testar o cenário onde o tempo recua 1 hora e avança 1 hora. Verificar se os timers internos (já baseados no relógio monotônico do Go) não quebram.

Fase 3 — Focus Interceptor Page

Objetivo: Servir uma página HTML local avisando que o site foi bloqueado, em vez de um erro genérico de DNS.

Backend (Go - internal/transport/httpapi & dnsserver):

    Listener HTTP (:80): O Daemon deve tentar fazer net.Listen("tcp", ":80").

        Se falhar (ex: porta ocupada pelo Apache/IIS), não dar panic. Fazer log de erro e seguir normalmente (Fallback).

    Lógica do Enforcer/DNS:

        Se Interceptor == true: Regras de hosts apontam para 127.0.0.1. Servidor DNS devolve o IP local da máquina (w.LocalAddr()).

        Se Interceptor == false: Mantém o padrão 0.0.0.0.

    Endpoint / (na porta 80): Ler o header Host. Se o Host estiver na lista de bloqueados, renderizar um template HTML estático html/template injetando o domínio e o tempo restante (IsBlocked(host) -> time_left).

    Contrato de Configuração (IPC): Adicionar propriedade enable_interceptor: boolean nas configurações gerais.

    Frontend (React/shadcn):

        Aba Configurações: Adicionar um Switch (shadcn) "Ativar página de bloqueio (Requer porta 80 livre)".

    TDD: Simular porta 80 ocupada no teste e garantir que o serviço continua rodando. Testar binding dinâmico no hosts generator.

Fase 4 — Regras por Dispositivo (Server Edition)

Objetivo: Permitir políticas flexíveis (Block, Allowlist, Inherit) baseadas em IP/MAC da rede local.

Backend (Go - internal/domain/devices & scheduler):

    Armazenamento: Novo arquivo devices.json ao lado do state.json.

    Refatoração de Assinatura:

        Mudar IsBlocked(domain string) bool para IsBlocked(domain string, clientIP string) bool.

        Esta mudança afeta os testes antigos, devendo ser refatorados no mesmo commit (usar 127.0.0.1 nos testes legados).

    Identificação (Best-effort MAC):

        Tentar resolver MAC a partir do IP executando arp -a (Linux/Windows) via exec.Command. Parsear a saída em cache (TTL de 5 min). O controle primário é por IP.

    IPC Contract (types.ts):
    TypeScript

    type Policy = "inherit" | "block_all" | "allow_list";

    // Actions: "devices-list", "devices-upsert", "devices-remove"
    type Device = {
      ip: string;
      mac?: string;
      name: string;
      policy: Policy;
      allowed_domains?: string[];
    };

    Frontend (React/shadcn):

        Nova tela "Dispositivos".

        Componente base: DataTable (shadcn) listando os IPs conhecidos (alimentado também pela telemetria da fase 1).

        Ações: Clicar num dispositivo abre um Sheet ou Dialog para definir Nome e Política.

        Formulário com Select para política. Se allow_list, exibir um Textarea ou lista de tags para domínios.

    TDD: Criar testes de precedência: Regra Específica do Dispositivo > Regra Global do Servidor.

Fase 5 — Engajamento: Relatório e Gamificação
5.1 Relatório Semanal Automático

Objetivo: Exportar os dados de analytics para HTML de forma automática.

Backend (Go):

    Agendador: Usar time.Ticker alinhado ao relógio. Calcular quanto tempo falta para o horário alvo (ex: Domingo, 23h59) e dar time.Sleep / time.AfterFunc no worker.

    Ação: Instanciar o gerador HTML já existente no módulo de analytics. Gravar no caminho configurado pelo usuário.

    IPC & Config:

        Adicionar ao state: report_schedule: { day_of_week: 0, hour: 23, minute: 59, export_path: "~/FocusGuardReports", enabled: true }.

    Frontend (React/shadcn):

        Configurações de Engajamento: Switch para habilitar, Select (dias da semana, horas) e um Input para o caminho de salvamento.

    TDD: Mockar o relógio (interface abstrata de tempo) para adiantar o relógio virtual para Domingo 23h59 e verificar se o callback de exportação foi chamado exatamente uma vez.

5.2 Gamificação (Conquistas)

Objetivo: Analisar os logs locais e premiar o usuário com badges sem adicionar carga de estado no sistema.

Backend (Go - internal/domain/achievements):

    Lógica Funcional (Sem banco de dados extra):

        Criar função pura: CalculateAchievements(stats AnalyticsData) []Achievement.

        Regras de exemplo:

            "Foco de Aço": stats.total_focus_hours >= 10

            "Imparável": stats.current_streak_days >= 7

            "Guardião da Madrugada": stats.focus_sessions_after_midnight >= 5

    IPC Contract (types.ts):
    TypeScript

    // Action: "achievements-get"
    type Achievement = {
      id: string;
      name: string;
      description: string;
      unlocked: boolean;
      progress: number; // 0 a 100
    };

    Frontend (React/shadcn):

        Tela "Estatísticas": Adicionar uma aba "Conquistas".

        Componentes: Grid de Cards pequenos. Usar lucide-react para os ícones (ex: 🏆, 🔥, 🌙).

        Se unlocked == false, usar opacidade reduzida e Progress bar (shadcn) para mostrar quão perto está.

    TDD: Injetar structs falsos de AnalyticsData na função de cálculo e assegurar que as badges corretas alternam o booleano unlocked e preenchem corretamente o progress.

Processo de Execução (Definition of Done do Agente)

Para cada fase e cada commit, o agente deve obrigatoriamente garantir que o ciclo abaixo foi cumprido:

    TDD First: O teste deve ser escrito antes (ou junto no mesmo commit) da implementação. Não fazer push de código de produção sem cobertura do caso de uso.

    Verificação IPC (make contract): Se qualquer pacote Go alterar os payloads, o types.ts deve ser atualizado no mesmo commit.

    Cross-Compilation Safety: Ao usar funções OS-specific (como sc query vs systemctl), implementar interfaces em os_windows.go e os_linux.go com build tags (//go:build windows).

    Graceful Degradation (Best-Effort): Se NTP falhar, rede cair, ou porta HTTP 80 estiver tomada, o daemon principal (bloqueio DNS/Hosts) nunca pode falhar (panic). Use logrus ou a lib de log padrão para Warn/Error e continue a execução.

    Schema do JSON: O state.json nunca pode quebrar no downgrade. Todos os novos campos (ex: last_known_time, políticas de devices) devem ter omitempty e a ausência deles deve instanciar o valor default seguro.