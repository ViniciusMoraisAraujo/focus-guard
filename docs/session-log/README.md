# Registro de sessões — `docs/session-log/`

> **Handoff diário entre sessões de trabalho (humanos e agentes de IA).** Um
> arquivo por dia (`YYYY-MM-DD.md`) com o resumo **breve** do que foi feito
> naquele dia, as decisões tomadas e o que ficou em andamento — para o agente
> (ou pessoa) do dia seguinte começar sem re-descobrir o contexto.

## Regras

1. **Quando:** no final de toda sessão de trabalho que tocou o repo (código,
   testes, docs, CI). É regra do AGENT.md raiz (§4.15) e item do Definition
   of Done (§5) — **uma sessão sem resumo não está terminada**. Escreva o
   resumo como **última etapa** da sessão (depois de commits/testes), para o
   estado descrito ser o estado real.
2. **Onde:** `docs/session-log/YYYY-MM-DD.md` (data do dia em que a sessão
   aconteceu). Se o arquivo do dia já existir, **atualize-o** (não crie
   duplicado): ajuste os itens que mudaram e o status. Ao criar um arquivo
   novo, **adicione a entrada no Índice** deste README (no topo).
3. **Idioma:** PT-BR (padrão dos docs); identificadores, comandos e hashes de
   commit em inglês.
4. **Breve:** o leitor do dia seguinte precisa de contexto, não de
   verbosidade. Use bullets, não parágrafos; alvo de ~30 linhas de conteúdo
   (sessões com working tree grande podem passar um pouco).
5. **Fatos, não promessas:** o que foi feito (com pacotes/commits), decisões
   (com o "porquê" em uma linha), pendências reais. Não liste "próximo passo"
   vago — só o que o próximo agente realmente precisa saber.
6. **CHANGELOG é separado:** item relevante para release vira entrada no
   `CHANGELOG.md` (`[Unreleased]`); o session-log é o registro de trabalho do
   dia, não um changelog de produto.

## Template

Copie e preencha (o [`2026-08-12.md`](2026-08-12.md) é um exemplo real):

```markdown
# Sessão — YYYY-MM-DD

> **Status:** 🟢/🟡/🔴 + uma linha: o que a sessão deixou pronto e o que
> ficou aberto. (1–3 linhas)

## O que foi feito

- **Área/feature** — o que mudou, arquivos/pacotes principais, hash do
  commit quando houver.

## Decisões importantes

- **Decisão** — porquê em uma linha (trade-off relevante).

## Em andamento / pendências

- Item não commitado ou etapa pela metade — o que o próximo agente precisa
  saber (ex.: testes que exigem shell elevado, verificação manual pendente).

## Validação

- `go build ./...` / `go vet ./...` / `go test ./...` / `tsc` / `vitest` /
  `make contract-check` … com o resultado (✅/❌ + exceções ambientais
  conhecidas).
```

> 💡 Depois de escrever o resumo, confira os demais itens do Definition of
> Done (AGENT.md §5) antes de encerrar a sessão.

## Validação automática

A convenção é cobrada por máquina em dois níveis (`scripts/check-session-log.sh`):

| Onde | O que valida | Falha quando |
|---|---|---|
| **Local — `make session-check`** | O resumo de **hoje** existe e segue o template | Você termina a sessão sem escrever o resumo (exit 1 com instrução) |
| **CI — `test.yml` (job `session-log`)** | A **estrutura** de todos os resumos existentes (nome `YYYY-MM-DD.md`, data válida, título `# Sessão — <data>`, seções obrigatórias) | Um resumo existente está fora do template — **não** exige o arquivo de hoje (um push pode acontecer em dia diferente do trabalho) |

Rode `make session-check` antes de commitar (item do Definition of Done do
AGENT.md §5).

## Índice

Ordem cronológica, o mais recente no topo:

- [2026-08-14](2026-08-14.md) — Sinkhole de rede completo (dual-stack
  [::]:53, firewall inbound, flush de cache, logs, recovery ~1s), manual na
  UI e release v0.19.0 tagada localmente (push pendente).
- [2026-08-13](2026-08-13.md) — Plano de validação Linux criado + Etapa 0/1
  no CI (jobs linux-full-suite, race ./..., windows-compile-check) — aguardando
  execução do CI.
- [2026-08-12](2026-08-12.md) — Convenção de session-log criada (este
  arquivo é a primeira entrada, exemplo real).
