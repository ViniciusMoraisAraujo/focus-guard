import { useEffect, useMemo, useState } from "react";
import { Download, Flame } from "lucide-react";
import { api } from "@/api/client";
import type { LabelStat } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useApp } from "@/context";
import { formatMinutes } from "@/hooks/useCountdown";

function download(filename: string, content: string, mime: string) {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function fmtDay(day: string): string {
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
    if (!s) return;
    const lines: string[] = ["dia,foco_min,sessoes"];
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

  return (
    <Screen>
      <ScreenHeader
        title="Estatísticas"
        actions={
          <>
            <Button variant="outline" onClick={exportCsv} disabled={!s}>
              <Download /> CSV
            </Button>
            <Button variant="outline" onClick={exportJson} disabled={!s}>
              <Download /> JSON
            </Button>
          </>
        }
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para ver as estatísticas.</p>
        </EmptyCard>
      ) : !s ? (
        <div className="flex flex-col gap-5" aria-label="Carregando estatísticas">
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-20 rounded-xl" />
            ))}
          </div>
          <Skeleton className="h-64 w-full rounded-xl" />
          <Skeleton className="h-40 w-full rounded-xl" />
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatCard value={String(s.total_sessions)} label="sessões" />
            <StatCard value={formatMinutes(s.total_focus)} label="foco total" />
            <StatCard value={formatMinutes(weeklyMs * 1e6)} label="últimos 7 dias" />
            <StatCard value={`${s.streak}`} label="dias seguidos" flame />
          </div>

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <h3 className="font-heading text-base font-semibold">Foco por dia (14 dias)</h3>
              {s.per_day.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Sem dados ainda — faça sessões de foco para ver o gráfico.
                </p>
              ) : (
                <div className="flex h-40 items-end gap-1.5 pt-3">
                  {s.per_day.slice(-14).map((d) => (
                    <div
                      key={d.day}
                      title={`${fmtDay(d.day)}: ${formatMinutes(d.duration)}`}
                      className="flex min-w-0 flex-1 flex-col items-center gap-1.5"
                    >
                      <div className="flex w-full flex-1 items-end rounded-md bg-muted/60 overflow-hidden">
                        <div
                          className="w-full rounded-md bg-primary/80 transition-all"
                          style={{ height: `${Math.max(4, (d.duration / 1e6 / maxDay) * 100)}%` }}
                        />
                      </div>
                      <span className="text-[11px] text-muted-foreground tabular-nums">
                        {d.day.slice(8)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <h3 className="font-heading text-base font-semibold">Domínios mais bloqueados</h3>
              {s.per_domain.length === 0 ? (
                <p className="text-sm text-muted-foreground">Sem dados por domínio ainda.</p>
              ) : (
                <div className="flex flex-col gap-2.5">
                  {s.per_domain.slice(0, 8).map((d) => (
                    <div key={d.domain} className="grid grid-cols-[minmax(100px,200px)_1fr_auto] items-center gap-3">
                      <span className="truncate text-sm">{d.domain}</span>
                      <div className="h-2 overflow-hidden rounded-full bg-muted">
                        <div
                          className="h-full rounded-full bg-primary transition-all"
                          style={{ width: `${Math.max(3, (d.duration / 1e6 / maxDomain) * 100)}%` }}
                        />
                      </div>
                      <span className="min-w-16 text-right text-xs text-muted-foreground tabular-nums">
                        {formatMinutes(d.duration)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <h3 className="font-heading text-base font-semibold">Missões de foco</h3>
              {missions === null ? (
                <div
                  className="flex flex-col gap-2.5"
                  aria-busy="true"
                  aria-label="Carregando missões"
                >
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className="h-10 w-full rounded-lg" />
                  ))}
                </div>
              ) : missions.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Nenhuma missão nomeada — inicie um pomodoro com um rótulo (ex: "Estudar ENEM").
                </p>
              ) : (
                <ul className="divide-y">
                  {missions.map((m) => (
                    <li key={m.label} className="flex items-center justify-between gap-3 py-2.5 text-sm">
                      <span className="font-medium">🎯 {m.label}</span>
                      <span className="text-muted-foreground">
                        {formatMinutes(m.duration)} · {m.sessions} sessões
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </Screen>
  );
}

function StatCard({ value, label, flame = false }: { value: string; label: string; flame?: boolean }) {
  return (
    <Card size="sm" className="transition-all duration-200 hover:-translate-y-0.5 hover:ring-foreground/20">
      <CardContent className="flex flex-col gap-1 px-4 py-4">
        <span className="text-2xl font-bold tabular-nums text-primary">
          {flame && <Flame className="mr-1 inline size-5" />}
          {value}
        </span>
        <span className="text-xs text-muted-foreground">{label}</span>
      </CardContent>
    </Card>
  );
}
