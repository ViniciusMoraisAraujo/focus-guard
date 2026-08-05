import { useMemo } from "react";
import type { ScheduleRule } from "@/api/types";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const DAYS = ["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"];

/** Cores por regra: índices do array de rules escolhem a cor (círculo invariante por regra). */
const PALETTE = [
  "bg-emerald-500/80 hover:bg-emerald-500",
  "bg-sky-500/80 hover:bg-sky-500",
  "bg-violet-500/80 hover:bg-violet-500",
  "bg-amber-500/80 hover:bg-amber-500",
  "bg-rose-500/80 hover:bg-rose-500",
  "bg-teal-500/80 hover:bg-teal-500",
];

/** Linhas de hora mostradas no eixo (a cada 4 h, menos poluído). */
const HOUR_LINES = [0, 4, 8, 12, 16, 20, 24];

interface Seg {
  start: number; // minutos desde 00:00
  end: number; // minutos desde 00:00 (exclusivo)
}

function toMin(hhmm: string): number {
  const [h, m] = hhmm.split(":").map(Number);
  if (Number.isNaN(h) || Number.isNaN(m)) return 0;
  return h * 60 + m;
}

/** Janelas de uma regra: preferem `windows`; senão usam start/end. Janelas
 * overnight (fim após a meia-noite, ex: 22:00-06:00) são válidas no daemon e
 * viram dois segmentos: até 24:00 e depois 00:00. */
function ruleSegments(r: ScheduleRule): Seg[] {
  const split = (start: number, end: number): Seg[] => {
    if (end <= start) {
      return [
        { start, end: 24 * 60 },
        { start: 0, end },
      ];
    }
    return [{ start, end }];
  };
  if (r.windows && r.windows.length > 0) {
    return r.windows.flatMap((w) => {
      const [s, e] = w.split("-").map(toMin);
      return split(s, e);
    });
  }
  return split(toMin(r.start), toMin(r.end));
}

function hm(min: number): string {
  const h = Math.floor(min / 60);
  const m = min % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
}

/**
 * WeeklyGrid renderiza a "grade semanal": 7 colunas (dom..sáb) com eixo de
 * horário e blocos coloridos posicionados pelas janelas de cada regra.
 * Janelas sobrepostas no mesmo dia são empilhadas lado a lado.
 */
export function WeeklyGrid({ rules }: { rules: ScheduleRule[] }) {
  const now = new Date();
  const todayIdx = now.getDay();
  const nowMin = now.getHours() * 60 + now.getMinutes();

  const colored = useMemo(
    () =>
      rules.map((r, i) => ({
        rule: r,
        color: PALETTE[i % PALETTE.length],
        segments: ruleSegments(r),
      })),
    [rules],
  );

  const COL_H = 560;

  return (
    <Card>
      <CardContent className="flex flex-col gap-3 px-4 py-4">
        <div className="flex items-baseline gap-2">
          <h3 className="font-heading text-sm font-semibold text-muted-foreground">
            Grade semanal
          </h3>
        </div>

        <div className="overflow-x-auto pb-1">
        <div
          className="grid min-w-[640px] gap-px overflow-hidden rounded-xl border bg-border"
          style={{ gridTemplateColumns: "44px repeat(7, minmax(0, 1fr))" }}
        >
          {/* Cabeçalho dos dias */}
          <div />
          {DAYS.map((d, i) => (
            <div
              key={d}
              className={cn(
                "bg-card px-1 py-1.5 text-center text-[11px] font-medium text-muted-foreground",
                i === todayIdx && "bg-card text-primary",
              )}
            >
              {d}
              {i === todayIdx && <span className="ml-1 text-primary">•</span>}
            </div>
          ))}

          {/* Eixo de horas */}
          <div className="relative bg-card" style={{ height: COL_H }}>
            {HOUR_LINES.map((h) => (
              <span
                key={h}
                className="absolute right-1 -translate-y-1/2 text-[9px] text-muted-foreground/60 tabular-nums"
                style={{ top: `${(h / 24) * 100}%` }}
              >
                {String(h).padStart(2, "0")}h
              </span>
            ))}
          </div>

          {/* Colunas de cada dia */}
          {DAYS.map((_, dayIdx) => {
            // Segments do dia; lanes via coloração gulosa de intervalos
            // (ótima para intervalos): cada item entra na 1ª lane livre.
            const items = colored.flatMap((c) =>
              c.rule.days.includes(dayIdx)
                ? c.segments.map((seg) => ({ color: c.color, seg, rule: c.rule }))
                : [],
            );
            items.sort((a, b) => a.seg.start - b.seg.start);

            const lanes: { end: number }[] = [];
            const laid = items.map((it) => {
              let lane = lanes.findIndex((l) => l.end <= it.seg.start);
              if (lane === -1) {
                lane = lanes.length;
                lanes.push({ end: it.seg.end });
              } else {
                lanes[lane].end = it.seg.end;
              }
              return { ...it, lane };
            });
            const laneCount = Math.max(1, lanes.length);

            return (
              <div
                key={dayIdx}
                className={cn(
                  "relative bg-card",
                  dayIdx === todayIdx && "bg-card ring-1 ring-primary/20 ring-inset",
                )}
                style={{ height: COL_H }}
              >
                {/* linhas de hora */}
                {HOUR_LINES.map((h) => (
                  <div
                    key={h}
                    className="absolute right-0 left-0 border-t border-border/40"
                    style={{ top: `${(h / 24) * 100}%` }}
                  />
                ))}

                {/* marcador "agora" no dia atual */}
                {dayIdx === todayIdx && (
                  <div
                    className="absolute right-0 left-0 z-20 flex items-center"
                    style={{ top: `${(nowMin / 1440) * 100}%` }}
                  >
                    <div className="h-px flex-1 bg-destructive" />
                    <span className="-translate-y-1/2 rounded-sm bg-destructive px-1 text-[9px] font-bold text-white">
                      agora
                    </span>
                  </div>
                )}

                {/* blocos das regras */}
                {laid.map((it, idx) => {
                  const topPct = (it.seg.start / 1440) * 100;
                  const hPct = ((it.seg.end - it.seg.start) / 1440) * 100;
                  const px = (it.seg.end - it.seg.start) / 1440 * COL_H;
                  return (
                    <div
                      key={`${it.rule.id}-${idx}`}
                      title={`${it.rule.label || it.rule.preset} · ${hm(it.seg.start)}–${hm(it.seg.end)}`}
                      className={cn(
                        "absolute z-10 overflow-hidden rounded px-1 py-0.5 text-[10px] leading-tight font-medium text-white shadow-sm transition-all duration-150 hover:z-30 hover:shadow-md",
                        it.color,
                        !it.rule.enabled && "opacity-40",
                      )}
                      style={{
                        top: `${topPct}%`,
                        height: `${hPct}%`,
                        left: `${(it.lane / laneCount) * 100}%`,
                        width: `${(1 / laneCount) * 100}%`,
                        minHeight: 14,
                      }}
                    >
                      <span className="block truncate">
                        {it.rule.label || it.rule.preset}
                      </span>
                      {px >= 22 && (
                        <span className="block truncate font-normal opacity-90">
                          {hm(it.seg.start)}–{hm(it.seg.end)}
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
        </div>

        {/* Legenda */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
          {colored.map((c, i) => (
            <span
              key={`${c.rule.id}-${i}`}
              className="flex items-center gap-1.5 text-xs text-muted-foreground"
            >
              <span className={cn("size-2.5 rounded-full", c.color)} />
              {c.rule.label || c.rule.preset}
              {!c.rule.enabled && " (desativada)"}
            </span>
          ))}
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="h-px w-4 bg-destructive" /> agora
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
