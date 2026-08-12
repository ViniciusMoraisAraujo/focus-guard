#!/usr/bin/env bash
# check-session-log.sh — validação do docs/session-log/ (handoff diário entre
# sessões de agentes; regra do AGENT.md raiz §4.15).
#
# Uso:
#   scripts/check-session-log.sh              # CI: valida a ESTRUTURA de todos
#                                             # os resumos existentes (nunca
#                                             # quebra por "dia sem resumo")
#   scripts/check-session-log.sh --today      # local (make session-check): falha
#                                             # se o resumo de HOJE não existir
#
# Exit 0 = ok; 1 = problema. As mensagens são PT-BR (padrão dos scripts do repo).
#
# Dependências: bash + GNU coreutils (date -d) — Linux CI e Git Bash no Windows.
set -euo pipefail

# ---------------------------------------------------------------- helpers ----

SESSION_LOG_DIR="docs/session-log"
TODAY_FILE="$SESSION_LOG_DIR/$(date +%F).md"

err() { echo "❌ $*" >&2; }

# file_ok <arquivo> — valida a estrutura de UM resumo do dia: nome YYYY-MM-DD.md,
# primeira linha "# Sessão — YYYY-MM-DD" casando com o nome e as seções do
# template (O que foi feito / Em andamento e pendências / Validação). Um
# resumo que não segue o template não serve de handoff — falha aqui no CI.
file_ok() {
	local f="$1" base expected
	base="$(basename "$f" .md)"
	expected="# Sessão — $base"

	if ! [[ "$base" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
		err "$f: nome não é YYYY-MM-DD.md (só arquivos de data são resumos de sessão)"
		return 1
	fi
	if ! date -d "$base" >/dev/null 2>&1; then
		err "$f: data inválida no nome ($base)"
		return 1
	fi
	# Data futura (além de hoje) é engano de nome — um resumo é do dia em que a
	# sessão aconteceu. Folga de 1 dia para diferença de fuso entre o autor e o CI.
	if [ "$(date -d "$base" +%s)" -gt "$(( $(date +%s) + 86400 ))" ]; then
		err "$f: data futura no nome ($base)"
		return 1
	fi
	# Captura a primeira linha numa variável (em vez de pipe com grep -q):
	# determinístico sob set -o pipefail (sem a corrida SIGPIPE do head).
	first_line="$(head -n1 "$f")"
	if [ "$first_line" != "$expected" ]; then
		err "$f: primeira linha deveria ser '$expected'"
		return 1
	fi
	for section in "## O que foi feito" "## Em andamento / pendências" "## Validação"; do
		if ! grep -qF -- "$section" "$f"; then
			err "$f: seção '$section' ausente (ver template no README da pasta)"
			return 1
		fi
	done
	return 0
}

# ------------------------------------------------------------------ modes ----

if [ "${1:-}" = "--today" ]; then
	# Modo local: o resumo de hoje existe? (Definition of Done do AGENT.md §5)
	if [ ! -f "$TODAY_FILE" ]; then
		err "Resumo da sessão de hoje não existe: $TODAY_FILE"
		err "  Crie-o seguindo o template de $SESSION_LOG_DIR/README.md (regra do AGENT.md raiz §4.15)."
		exit 1
	fi
	# Um resumo que não segue o template não vale — valida a estrutura também.
	if ! file_ok "$TODAY_FILE"; then
		err "$TODAY_FILE existe, mas não segue o template do session-log."
		exit 1
	fi
	echo "✔ Resumo da sessão de hoje OK: $TODAY_FILE"
	exit 0
fi

# Modo CI (padrão): valida a estrutura de TODOS os resumos existentes. Não
# exige o arquivo de hoje (o push pode acontecer em dia diferente do trabalho)
# — o Makefile (make session-check) é quem cobre o resumo do dia, localmente.
if [ ! -d "$SESSION_LOG_DIR" ]; then
	err "$SESSION_LOG_DIR não existe — a convenção de session-log está desligada?"
	exit 1
fi

failed=0
for f in "$SESSION_LOG_DIR"/*.md; do
	[ -f "$f" ] || continue
	case "$(basename "$f")" in
		README.md) continue ;; # índice/template — não é um resumo de dia
	esac
	if ! file_ok "$f"; then
		failed=1
	fi
done

if [ "$failed" -ne 0 ]; then
	err "docs/session-log/ com resumos fora do template (ver acima)."
	exit 1
fi

count=$(find "$SESSION_LOG_DIR" -maxdepth 1 -name '*.md' ! -name 'README.md' | wc -l | tr -d ' ')
if [ "$count" -eq 1 ]; then
	label="1 resumo válido"
else
	label="$count resumos válidos"
fi
echo "✔ docs/session-log/ OK ($label)"
