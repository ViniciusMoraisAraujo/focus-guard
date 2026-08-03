import { useMemo } from "react";
import { Button, Card } from "../components/ui";
import { useApp, type Screen } from "../context";
import { formatClock, formatMinutes, formatMs, useCountdown } from "../hooks/useCountdown";
import type { Block } from "../api/types";

const ALL_INTERNET = "*all-internet*";

export function Dashboard({ onNavigate }: { onNavigate: (s: Screen) => void }) {
  const { daemonUp, status, stats } = useApp();

  const blocks = useMemo(() => {
    const list = (status?.blocks ?? []).filter((b) => b.domain !== ALL_INTERNET);
    return [...list].sort((a, b) => a.expires_at.localeCompare(b.expires_at));
  }, [status?.blocks]);

  const panic = useMemo(
    () => (status?.blocks ?? []).some((b) => b.domain === ALL_INTERNET),
    [status?.blocks],
  );

  const pomo = status?.pomodoro?.active ? status.pomodoro : null;
  const nearest = blocks[0]?.expires_at ?? null;
  const nearestMs = useCountdown(nearest);

  const goalNs = status?.goal ?? 0;
  const goalMin = goalNs / 6e10;
  const todayFocusNs = useMemo(() => {
    let ns = stats?.stats?.per_day.at(-1)?.duration ?? 0;
    if (pomo?.started_at) {
      const elapsed = Math.max(0, Date.now() - new Date(pomo.started_at).getTime());
      ns += elapsed * 1e6; // ms → ns
    }
    return ns;
  }, [stats, pomo?.started_at, pomo?.active]);
  const todayFocusMs = todayFocusNs / 1e6;
  const progress = goalMin > 0 ? Math.min(1, todayFocusMs / (goalMin * 60_000)) : 0;

  if (daemonUp === null) {
    return (
      <section className="screen">
        <h2>Painel</h2>
        <Card className="hero-card">
          <p className="muted">Verificando o daemon…</p>
        </Card>
      </section>
    );
  }

  const statusKind = panic ? "panic" : blocks.length > 0 || pomo ? "focus" : "idle";
  const statusTitle = panic
    ? "Modo pânico ativo"
    : pomo
      ? `Pomodoro ativo — ${pomo.phase === "rest" ? "descanso" : "foco"} (ciclo ${pomo.cycle}/${pomo.cycles})`
      : blocks.length > 0
        ? `Foco ativo — ${blocks.length} bloqueio${blocks.length > 1 ? "s" : ""}`
        : "Sem bloqueios ativos";
  const statusSub = panic
    ? "Toda a internet está bloqueada até o fim do período."
    : pomo
      ? `Sessão sobre o preset ${pomo.preset ?? "—"}`
      : blocks.length > 0
        ? "A distração está fora do alcance. Bons estudos! 🎯"
        : "Ótimo momento para iniciar um foco.";

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Painel</h2>
        <div className="quick-actions">
          <Button variant="primary" onClick={() => onNavigate("bloquear")}>
            🔒 Bloquear site
          </Button>
          <Button variant="danger" onClick={() => onNavigate("panico")}>
            🚨 Modo pânico
          </Button>
        </div>
      </header>

      <Card className={`hero-card status-${statusKind}`}>
        <div className="hero-main">
          <span className="hero-pill" aria-hidden="true">
            {panic ? "🚨" : pomo ? "🍅" : blocks.length > 0 ? "🛡️" : "🌿"}
          </span>
          <div>
            <h3>{statusTitle}</h3>
            <p className="muted">{statusSub}</p>
          </div>
        </div>
        {nearest && blocks.length > 0 && (
          <div className="hero-countdown">
            <span className="countdown-label">Próximo fim em</span>
            <span className="countdown">{formatMs(nearestMs)}</span>
            <span className="muted">{blocks[0].domain}</span>
          </div>
        )}
      </Card>

      {goalMin > 0 && (
        <Card className="goal-card">
          <div className="goal-head">
            <span>🎯 Meta do dia: {formatMinutes(goalNs)}</span>
            <span className="muted">
              {formatMinutes(todayFocusMs * 1e6)} acumulado
              {pomo ? " (sessão ativa)" : ""}
            </span>
          </div>
          <div className="goal-track">
            <div className="goal-fill" style={{ width: `${Math.max(3, progress * 100)}%` }} />
          </div>
        </Card>
      )}

      <div className="section-title">
        <h3>Bloqueios ativos</h3>
        <span className="muted">{blocks.length}</span>
      </div>
      {blocks.length === 0 ? (
        <Card className="empty-card">
          <p>Nenhum bloqueio ativo no momento.</p>
          <p className="muted">Bloqueie um site ou um preset para começar a focar.</p>
        </Card>
      ) : (
        <div className="blocks-grid">
          {blocks.map((b) => (
            <BlockCard key={b.domain} block={b} />
          ))}
        </div>
      )}
    </section>
  );
}

function BlockCard({ block }: { block: Block }) {
  const ms = useCountdown(block.expires_at);
  return (
    <Card className="block-card">
      <div className="block-head">
        <span className="block-domain">{block.domain}</span>
        <span className="badge badge-green">ativo</span>
      </div>
      <div className="block-countdown">{formatMs(ms)}</div>
      <div className="block-meta muted">
        <span>início {formatClock(block.started_at)}</span>
        <span>fim {formatClock(block.expires_at)}</span>
      </div>
    </Card>
  );
}
