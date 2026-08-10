# FocusGuard — Roadmap de features

> Visão geral das próximas features. O detalhamento técnico (pacotes afetados,
> fases, critérios de saída) vive em **`docs/features-plan.md`** — este arquivo
> é o resumo executivo. Regras do repo: TDD, stdlib-first, best-effort para
> falhas de SO, e **nada de "manual unblock"** sem decisão de produto explícita.

## Em andamento / próximas

### 1. 🕐 Clock Tamper Protection (anti-fuso)

**Problema:** mudar a data/hora do computador para "enganar" a expiração dos
timers de bloqueio.

**Correção técnica da proposta original:** o relógio **monotônico** do Go
(`time.Since`/`time.Now().Sub`) só existe *em processo* — ele não pode ser
persistido. `time.Now()` é o **wall clock**. O design correto é duplo:

- **Em processo:** medir a expiração dos timers com tempo monotônico (imune a
  mudança de data durante a execução). O `time.Until(ExpiresAt)` do
  `setupTimerLocked` já é monotônico quando o `ExpiresAt` vem de `now.Add(d)`;
  a exposição real é o caminho **persistido/restart** (JSON descarta a leitura
  monotônica → volta ao wall clock) e as comparações `time.Now().After` em
  `IsActive`/`CanUnblock`.
- **Persistido:** salvar `LastKnownTime` no `state.json` a cada mutação; no
  boot (e periodicamente), detectar o gap do wall clock **nos dois sentidos**
  (`|now − lastKnown|` além da tolerância, ex.: 5 min, com janela de graça
  para ajuste legítimo de NTP/DST) — cobrindo tanto "voltar no tempo" quanto
  **adiantar a data para o bloqueio expirar cedo antes de reiniciar o daemon**.
- **Validação:** consultar um servidor NTP público (stdlib/UDP) para confirmar
  o horário real. Se confirmado tamper → bloquear tudo preventivamente até
  validação bem-sucedida, re-ajustar expirações pendentes pelo gap e
  registrar no tamper-log.

**Onde:** `internal/domain/scheduler` (timers + estado), `internal/domain/policy`
(expiração), `internal/infrastructure/store` (LastKnownTime), novo pacote de
NTP (`internal/infrastructure/ntp`), daemon (boot + worker periódico).

### 2. 🚧 Focus Interceptor Page

**Problema:** ao acessar um site bloqueado, o usuário vê "conexão recusada" /
página em branco — sem contexto de *por que* e *até quando*.

**Correção técnica da proposta original:** o servidor web real é
`internal/transport/httpapi` na **`127.0.0.1:48902`** (`DefaultAddr`), não
"internal/web :9090".

- **Modo Desktop (localhost):** o navegador acessa `http://dominio:80`, então o
  **daemon precisa de um listener HTTP na porta 80** (loopback) servindo a
  página de interceptação (domínio, tempo restante, botão de volta); o hosts
  aponta o domínio bloqueado para `127.0.0.1`. A rota `/blocked` no `httpapi`
  (48902) serve como visualização administrativa da página.
- **Modo Server / DNS sinkhole (rede):** o DNS responde domínios bloqueados com
  o **IP do próprio servidor** (ex.: `192.168.1.100`) em vez de `0.0.0.0`, e o
  mesmo listener HTTP na **porta 80** serve a página para a rede.
  ⚠️ Limitações documentadas: interceptação real só em **HTTP puro** — HTTPS
  mostra erro de certificado (fallback a definir no plano); e a escolha do IP
  LAN precisa de regra clara (ex.: IP da rota default, configurável) —
  multi-NIC/VPN quebram a página.

**Onde:** `internal/transport/httpapi` (rota + template), `internal/infrastructure/enforcer`
(hosts: `127.0.0.1` em vez de `0.0.0.0`), `internal/infrastructure/dnsserver`
(responder IP local), novo listener HTTP no daemon (Server).

### 3. 🩺 `focusguard doctor`

**Problema:** problemas de instalação/permissão só aparecem como bug reportado.

**Solução:** comando de diagnóstico que roda checagens completas e reporta
antes de virar incidente:

- Permissões (elevação, escrita na pasta de estado, grupo do socket no Linux)
- Serviço instalado e rodando (daemon + watchdog), IPC acessível (ping)
- Regras de firewall órfãs (via sweep do enforcer) e integridade do `hosts`
- Estado persistido válido, réplicas, DNS sinkhole, versões dos binários
  coerentes (uma suíte "mista" é problema)

Saída legível + **exit code ≠ 0** quando há problemas (fácil de usar em scripts).

**Onde:** `cmd/focusguard/doctor.go` (novo comando na tabela `commands.go`),
reusa `internal/infrastructure/enforcer`, `internal/transport/ipc` (ping),
`internal/infrastructure/autostart` (consulta serviço).

### 4. 📡 Telemetria do sinkhole no painel

**Problema:** a edição Server bloqueia a rede inteira, mas ninguém vê *o que*
foi cortado e *de onde*.

**Solução:** o `dnsserver` já conta `queries`/`blocked`; evoluir para log por
domínio + IP de origem (JSONL, mesmo padrão do analytics), exposto via IPC e
exibido no painel web (tela Rede): "domínio X bloqueado N vezes, por IP Y".

**Onde:** `internal/infrastructure/dnsserver` (captura por request),
`internal/infrastructure/filelog` ou JSONL próprio, nova ação IPC + `types.ts`,
tela Rede no `focusguard-ui`.

### 5. 📱 Regras por dispositivo (Server)

**Problema:** o sinkhole é tudo-ou-nada — console do filho e notebook do
trabalho recebem a mesma política.

**Solução:** políticas por dispositivo na edição Server:

- Por **IP** (fixo/DHCP reservation) ou **MAC** (via tabela ARP)
- Permitir domínios para um dispositivo enquanto o resto da rede fica
  bloqueado (e vice-versa)
- Persistido ao lado do state.json; o `IsBlocked` do scheduler ganha o
  contexto do IP de origem (já disponível no `RemoteAddr` do DNS)

**Onde:** `internal/infrastructure/dnsserver` (IP de origem já chega no
handler), `internal/domain/scheduler` (`IsBlocked(domain, clientIP)`), novo
`internal/domain/devices` (store + handlers), IPC + `types.ts` + UI (tela Rede).

### 6. 📊 Relatório semanal automático

**Problema:** o `focusguard report`/stats existem, mas dependem de rodar
manualmente.

**Solução:** geração **agendada** do relatório semanal (ex.: domingo à noite):

- Reusa o que já existe (`internal/domain/analytics` — sessions/stats/report)
- Salva em pasta configurável (HTML autossuficiente + JSON) e/ou notifica no
  tray/painel quando pronto
- Flag no painel (Configurações): ativar/desativar + caminho + dia/hora

**Onde:** `internal/domain/analytics` (export existente), novo agendador no
daemon (padrão do `startScheduleWorker`), notificação (reusa o padrão do
pomodoro), UI (Configurações).

### 7. 🏆 Gamificação (conquistas)

**Problema:** manter o hábito de foco no longo prazo — stats existem, mas não
engajam.

**Solução:** conquistas/badges derivados **das stats existentes** (sem estado
novo complexo):

- Ex.: "7 dias de streak", "10h de foco na semana", "10 missões concluídas",
  "1ª semana com meta batida todo dia"
- Catálogo de conquistas com condição pura (função `Achievements(stats)`),
  desbloqueio calculado na leitura — sem persistência de "já ganhou" (evita
  estado duplicado)
- Exibição no painel (tela Estatísticas) com badges

**Onde:** novo `internal/domain/achievements` (lógica pura + testes),
`internal/domain/analytics` (fonte), IPC + `types.ts` + UI (Estatísticas).

---

## Backlog (avaliar depois)

> Itens com **⚠️ alto esforço / ⚠️ depende de externo** são sinalizados — o
> restante é factível com a arquitetura atual (stdlib-first).

### Produtividade & hábito

- Limite diário de uso por domínio/categoria (teto de tempo por dia)
- Resumo diário (end-of-day report) — extensão do relatório semanal agendado
- Meta de foco semanal/mensal (hoje o `goal` é só diário)
- Lembrete de pausa/descanso de olhos fora do pomodoro (bem-estar)
- Modo "desintoxicação" progressiva — reduzir gradualmente o teto de um
  domínio ao longo das semanas (ex.: 2h → 1h30 → 1h)
- Relatório de impacto — estimativa de tempo recuperado/desperdiçado por
  bloqueio (engajamento)
- Snooze limitado de janelas de agenda

### Agenda & integrações

- Import de calendário **por URL/ICS remoto** (hoje só por arquivo) + bloquear
  automaticamente durante reuniões/eventos — ⚠️ depende de rede externa
- Webhooks/API para automação externa (ex.: Home Assistant dispara bloqueio
  quando entra em reunião) — base sobre o `httpapi` + auth existentes
- Sincronização de política entre máquinas (self-hosted) — casa e trabalho com
  a mesma agenda/presets — **⚠️ alto esforço** (estado distribuído)

### Rede (edição Server)

- Detecção de contorno — identificar proxy/VPN/DoH além da porta 853 já
  bloqueada e registrar/alertar (estende a telemetria do sinkhole)
- Curfew de rede — janela "nada conecta" por dispositivo/faixa de horário
  (reusa schedule + regras por dispositivo)

### Anti-burla / privacidade

- Timezones de viagem — distinguir mudança de fuso **legítima** de tamper
  (estende o Clock Tamper Protection); a agenda segue o fuso de casa ou o
  local, configurável
- Perfis de foco por usuário do SO — políticas diferentes por conta do
  Windows/Linux (além do multiusuário da web)

### Navegador (avaliar)

- Extensão de navegador — bloqueio **por URL/path** (hoje é só por domínio, o
  que hosts/DNS não alcançam) + página de interceptação rica — **⚠️ alto
  esforço e muda a arquitetura** (um componente novo no navegador, fora do
  daemon)

### Ainda em decisão

- Pausa responsável com cooldown (desbloqueio temporário com justificativa +
  espera + log) — **depende de decisão de produto** (AGENT.md proíbe "manual
  unblock" sem ela)
- Import/export de configuração completa (presets, agendas, apps, metas)
- Orçamento diário de apps (process guard com teto de tempo)
- Perfis por rede (SSID) — políticas diferentes casa vs trabalho
