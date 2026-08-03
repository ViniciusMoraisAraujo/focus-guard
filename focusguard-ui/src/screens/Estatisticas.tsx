import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { LabelStat } from "../api/types";
import { Button, Card, Spinner } from "../components/ui";
import { useApp } from "../context";
import { formatMinutes } from "../hooks/useCountdown";

function download(filename: string, content: string, mime: string) {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}function fmtDay(day: string): string {
	// day é "YYYY-MM-DD"
	const [y, m, d] = day.split("-");
	return `${d}/${m}/${y}`;
}

// csvCell neutraliza injeção de fórmula (Excel/Sheets): valores iniciados com
// =, +, - ou @ viram fórmula quando abertos em planilhas. Prefixa com aspas.
function csvCell(v: string): string {
	return /^[=+\-@]/.test(v) ? `'${v}` : v;
}

export function Estatisticas() {
  const { stats, daemonUp } = useApp();
  const [missions, setMissions] = useState<LabelStat[] | null>(null);

  useEffect(() => {
    api
      .missions()
      .then((r) => setMissions(r.success ? (r.label_stats ?? []) : []))
      .catch(() => setMissions([]));
  }, []);

  const s = stats?.stats;

  const weeklyMs = useMemo(() => {
    if (!s) return 0;
    const week = s.per_day.slice(-7);
    return week.reduce((acc, d) => acc + d.duration, 0) / 1e6;
  }, [s]);

  const maxDay = useMemo(
    () => Math.max(1, ...(s?.per_day.slice(-14).map((d) => d.duration / 1e6) ?? [1])),
    [s],
  );

  const maxDomain = useMemo(
    () => Math.max(1, ...(s?.per_domain.map((d) => d.duration / 1e6) ?? [1])),
    [s],
  );

  const exportCsv = () => {
    if (!s) return;	const lines: string[] = ["dia,foco_min,sessoes"];
	for (const d of s.per_day) {
		lines.push(`${csvCell(d.day)},${(d.duration / 6e10).toFixed(1)},${d.sessions}`);
	}
	lines.push("");
	lines.push("dominio,foco_min");
	for (const d of s.per_domain) {
		lines.push(`${csvCell(d.domain)},${(d.duration / 6e10).toFixed(1)}`);
	}
    download("focusguard-stats.csv", lines.join("\n"), "text/csv;charset=utf-8");
  };

  const exportJson = () => {
    if (!s) return;
    download("focusguard-stats.json", JSON.stringify(s, null, 2), "application/json");
  };

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Estatísticas</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para ver as estatísticas.</p>
        </Card>
      </section>
    );
  }

  if (!s) {
    return (
      <section className="screen">
        <h2>Estatísticas</h2>
        <Card className="empty-card">
          <Spinner label="Carregando estatísticas…" />
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Estatísticas</h2>
        <div className="quick-actions">
          <Button variant="secondary" onClick={exportCsv}>
            ⬇ CSV
          </Button>
          <Button variant="secondary" onClick={exportJson}>
            ⬇ JSON
          </Button>
        </div>
      </header>

      <div className="stat-cards">
        <Card className="stat-card">
          <span className="stat-value">{s.total_sessions}</span>
          <span className="muted">sessões</span>
        </Card>
        <Card className="stat-card">
          <span className="stat-value">{formatMinutes(s.total_focus)}</span>
          <span className="muted">foco total</span>
        </Card>
        <Card className="stat-card">
          <span className="stat-value">{formatMinutes(weeklyMs * 1e6)}</span>
          <span className="muted">últimos 7 dias</span>
        </Card>
        <Card className="stat-card">
          <span className="stat-value">🔥 {s.streak}</span>
          <span className="muted">dias seguidos</span>
        </Card>
      </div>

      <Card>
        <h3>Foco por dia (14 dias)</h3>
        {s.per_day.length === 0 ? (
          <p className="muted">Sem dados ainda — faça sessões de foco para ver o gráfico.</p>
        ) : (
          <div className="bar-chart">
            {s.per_day.slice(-14).map((d) => (
              <div key={d.day} className="bar-col" title={`${fmtDay(d.day)}: ${formatMinutes(d.duration)}`}>
                <div className="bar-track">
                  <div
                    className="bar-fill"
                    style={{ height: `${Math.max(4, (d.duration / 1e6 / maxDay) * 100)}%` }}
                  />
                </div>
                <span className="bar-label">{d.day.slice(8)}</span>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <h3>Domínios mais bloqueados</h3>
        {s.per_domain.length === 0 ? (
          <p className="muted">Sem dados por domínio ainda.</p>
        ) : (
          <div className="domain-bars">
            {s.per_domain.slice(0, 8).map((d) => (
              <div key={d.domain} className="domain-row">
                <span className="domain-name">{d.domain}</span>
                <div className="domain-track">
                  <div
                    className="domain-fill"
                    style={{ width: `${Math.max(3, (d.duration / 1e6 / maxDomain) * 100)}%` }}
                  />
                </div>
                <span className="domain-min">{formatMinutes(d.duration)}</span>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <h3>Missões de foco</h3>
        {missions === null ? (
          <p className="muted">Carregando…</p>
        ) : missions.length === 0 ? (
          <p className="muted">
            Nenhuma missão nomeada — inicie um pomodoro com um rótulo (ex: "Estudar ENEM").
          </p>
        ) : (
          <ul className="info-list">
            {missions.map((m) => (
              <li key={m.label}>
                <span>🎯 {m.label}</span>
                <strong>
                  {formatMinutes(m.duration)} · {m.sessions} sessões
                </strong>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </section>
  );
}
