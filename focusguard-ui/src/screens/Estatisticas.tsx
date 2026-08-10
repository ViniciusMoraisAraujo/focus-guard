import { useEffect, useMemo, useState } from "react";
import { Download, Flame, Trophy } from "lucide-react";
import { api } from "@/api/client";
import type { Achievement, FocusSession, LabelStat } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useData } from "@/context";
import { formatClock, formatMinutes } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

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
  const { stats, daemonUp } = useData();
  const [missions, setMissions] = useState<LabelStat[] | null>(null);
  const [sessions, setSessions] = useState<FocusSession[] | null>(null);
  const [achievements, setAchievements] = useState<Achievement[] | null>(null);
  // Dispara a animação de crescimento das barras após o primeiro paint.
  const [grown, setGrown] = useState(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => setGrown(true));
    return () => cancelAnimationFrame(id);
  }, []);

  useEffect(() => {
    api
      .missions()
      .then((r) => setMissions(r.success ? (r.label_stats ?? []) : []))
      .catch(() => setMissions([]));
  }, []);

  useEffect(() => {
    api
      .sessions()
      .then((r) => setSessions(r.success ? (r.sessions ?? []) : []))
      .catch(() => setSessions([]));
  }, []);

  useEffect(() => {
    api
      .achievements()
      .then((r) => setAchievements(r.success ? (r.achievements ?? []) : []))
      .catch(() => setAchievements([]));
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
                  {s.per_day.slice(-14).map((d) => {
                    const pct = (d.duration / 1e6 / maxDay) * 100;
                    const isToday = d.day === s.per_day.at(-1)?.day;
                    return (
                      <div
                        key={d.day}
                        title={`${fmtDay(d.day)}: ${formatMinutes(d.duration)}`}
                        className="group flex min-w-0 flex-1 flex-col items-center gap-1.5"
                      >
                        <div className="relative flex w-full flex-1 items-end rounded-md bg-muted/60 overflow-hidden">
                          <div
                            className={cn(
                              "w-full rounded-md bg-gradient-to-t from-primary/60 to-primary transition-[height] duration-500 ease-out group-hover:from-primary/80 group-hover:to-primary group-hover:brightness-110",
                              isToday && "from-emerald-500/70 to-emerald-500",
                            )}
                            style={{ height: grown ? `${Math.max(4, pct)}%` : "4%" }}
                          />
                        </div>
                        <span
                          className={cn(
                            "text-[11px] tabular-nums",
                            isToday ? "font-semibold text-emerald-500" : "text-muted-foreground",
                          )}
                        >
                          {d.day.slice(8)}
                        </span>
                      </div>
                    );
                  })}
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
                  {s.per_domain.slice(0, 8).map((d) => {
                    const pct = Math.max(3, (d.duration / 1e6 / maxDomain) * 100);
                    return (
                      <div
                        key={d.domain}
                        title={`${d.domain}: ${formatMinutes(d.duration)}`}
                        className="group grid grid-cols-[minmax(100px,200px)_1fr_auto] items-center gap-3"
                      >
                        <span className="truncate text-sm">{d.domain}</span>
                        <div className="h-2 overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-gradient-to-r from-primary/60 to-primary transition-[width] duration-500 ease-out group-hover:brightness-110"
                            style={{ width: grown ? `${pct}%` : "3%" }}
                          />
                        </div>
                        <span className="min-w-16 text-right text-xs text-muted-foreground tabular-nums">
                          {formatMinutes(d.duration)}
                        </span>
                      </div>
                    );
                  })}
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

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <div className="flex flex-wrap items-center gap-2">
                <Trophy className="size-4 text-muted-foreground" />
                <h3 className="font-heading text-base font-semibold">Conquistas</h3>
                {achievements && (
                  <Badge variant="secondary" className="ml-auto">
                    {achievements.filter((a) => a.unlocked).length}/{achievements.length} desbloqueadas
                  </Badge>
                )}
              </div>
              {achievements === null ? (
                <div
                  className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
                  aria-busy="true"
                  aria-label="Carregando conquistas"
                >
                  {[0, 1, 2, 3].map((i) => (
                    <Skeleton key={i} className="h-28 w-full rounded-xl" />
                  ))}
                </div>
              ) : achievements.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Nenhuma conquista disponível — complete sessões de foco para desbloquear badges.
                </p>
              ) : (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {achievements.map((a) => (
                    <div
                      key={a.id}
                      className={cn(
                        "flex flex-col gap-2 rounded-xl border p-3.5 transition-all duration-200",
                        a.unlocked
                          ? "border-primary/30 bg-primary/5 hover:-translate-y-0.5 hover:shadow-md"
                          : "border-border bg-muted/30 opacity-60",
                      )}
                      title={a.description}
                    >
                      <div className="flex items-center gap-2">
                        <span className="text-xl" aria-hidden>
                          {a.unlocked ? a.icon || "🏅" : "🔒"}
                        </span>
                        <div className="min-w-0">
                          <p className="truncate text-sm font-semibold">{a.name}</p>
                          <p className="truncate text-[11px] text-muted-foreground">
                            {a.unlocked ? "desbloqueada" : `${a.progress}% para desbloquear`}
                          </p>
                        </div>
                      </div>
                      <p className="line-clamp-2 text-xs text-muted-foreground">{a.description}</p>
                      {!a.unlocked && <Progress value={a.progress} className="h-1.5" />}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <h3 className="font-heading text-base font-semibold">Sessões recentes</h3>
              {sessions === null ? (
                <div
                  className="flex flex-col gap-2.5"
                  aria-busy="true"
                  aria-label="Carregando sessões"
                >
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className="h-14 w-full rounded-lg" />
                  ))}
                </div>
              ) : sessions.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Nenhuma sessão concluída ainda — complete um pomodoro para registrar seu foco.
                </p>
              ) : (
                <ul className="divide-y">
                  {sessions.slice(0, 15).map((s) => (
                    <SessionRow key={`${s.start}-${s.preset}`} session={s} />
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

function SessionRow({ session: s }: { session: FocusSession }) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-2.5 text-sm">
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex items-center gap-2">
          <Badge variant="secondary" className="capitalize">
            {s.preset}
          </Badge>
          {s.strict && <Badge variant="destructive">estrita</Badge>}
          {s.label && (
            <span className="truncate font-medium" title={s.label}>
              🎯 {s.label}
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 text-xs text-muted-foreground">
          <span>
            {fmtDate(s.start)} · {formatClock(s.start)}–{formatClock(s.end)}
          </span>
          {s.cycles > 0 && <span>{s.cycles} ciclos</span>}
        </div>
        {s.domains.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {s.domains.slice(0, 4).map((d) => (
              <span
                key={d}
                className="max-w-40 truncate rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"
              >
                {d}
              </span>
            ))}
            {s.domains.length > 4 && (
              <span className="text-[11px] text-muted-foreground">
                +{s.domains.length - 4}
              </span>
            )}
          </div>
        )}
      </div>
      <span className="shrink-0 text-sm font-semibold tabular-nums text-primary">
        {formatMinutes(s.focus)}
      </span>
    </li>
  );
}

function fmtDate(rfc3339: string): string {
  const d = new Date(rfc3339);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric" });
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
