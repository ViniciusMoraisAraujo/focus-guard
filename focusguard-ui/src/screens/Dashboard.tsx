import { useEffect, useMemo, useState } from "react";
import {
  BarChart3,
  CalendarDays,
  Leaf,
  Lock,
  MoreHorizontal,
  Network,
  Settings,
  ShieldCheck,
  Siren,
  Timer,
} from "lucide-react";
import type { Block } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyCard, Screen, ScreenHeader, SectionTitle } from "@/components/screen";
import { useData, type Screen as ScreenId } from "@/context";
import { formatClock, formatMinutes, formatMs, useCountdown } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

const ALL_INTERNET = "*all-internet*";

export function Dashboard({ onNavigate }: { onNavigate: (s: ScreenId) => void }) {
  const { daemonUp, status, stats } = useData();

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

  // Tick de 1s apenas durante sessão ativa, para o acumulado andar em tempo
  // real (o status/statísticas só atualizam a cada 10s/60s).
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!pomo?.started_at) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [pomo?.started_at, pomo?.active]);

  const todayFocusNs = useMemo(() => {
    let ns = stats?.stats?.per_day.at(-1)?.duration ?? 0;
    if (pomo?.started_at) {
      const elapsed = Math.max(0, now - new Date(pomo.started_at).getTime());
      ns += elapsed * 1e6; // ms → ns
    }
    return ns;
  }, [stats, pomo?.started_at, pomo?.active, now]);
  const todayFocusMs = todayFocusNs / 1e6;
  const progress = goalMin > 0 ? Math.min(1, todayFocusMs / (goalMin * 60_000)) : 0;

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

  const HeroIcon = panic ? Siren : pomo ? Timer : blocks.length > 0 ? ShieldCheck : Leaf;

  return (
    <Screen>
      <ScreenHeader
        title="Painel"
        actions={
          <>
            <Button onClick={() => onNavigate("bloquear")}>
              <Lock /> Bloquear site
            </Button>
            <Button variant="destructive" onClick={() => onNavigate("panico")}>
              <Siren /> Modo pânico
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" aria-label="Navegação rápida">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel>Navegação rápida</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => onNavigate("pomodoro")}>
                  <Timer /> Pomodoro
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onNavigate("agenda")}>
                  <CalendarDays /> Agenda
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onNavigate("stats")}>
                  <BarChart3 /> Estatísticas
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => onNavigate("config")}>
                  <Settings /> Configurações
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />

      {daemonUp === null && (
        <div className="flex flex-col gap-5" aria-label="Carregando painel">
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-4 w-44" />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-24 w-full rounded-xl" />
            ))}
          </div>
        </div>
      )}

      {daemonUp !== null && (
        <>
          <Card
            className={cn(
              "flex-row items-center justify-between gap-4",
              statusKind === "focus" && "ring-emerald-500/30",
              statusKind === "panic" && "ring-destructive/40",
            )}
          >
            <CardContent className="flex flex-1 flex-wrap items-center gap-4 px-5 py-4">
              <div
                className={cn(
                  "grid size-12 shrink-0 place-items-center rounded-xl bg-muted ring-1 ring-border",
                  statusKind === "panic" && "bg-destructive/10 text-destructive ring-destructive/30",
                  statusKind === "focus" && "text-emerald-500 ring-emerald-500/30",
                )}
              >
                <HeroIcon className="size-6" />
              </div>
              <div className="min-w-0 flex-1">
                <h3 className="font-heading text-lg font-semibold">{statusTitle}</h3>
                <p className="text-sm text-muted-foreground">{statusSub}</p>
              </div>
              {nearest && blocks.length > 0 && (
                <div className="text-right">
                  <span className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
                    Próximo fim em
                  </span>
                  <div className="font-mono text-3xl font-bold tabular-nums text-primary">
                    {formatMs(nearestMs)}
                  </div>
                  <div className="text-xs text-muted-foreground">{blocks[0].domain}</div>
                </div>
              )}
            </CardContent>
          </Card>

          {goalMin > 0 && (
            <Card>
              <CardContent className="px-5 py-4">
                <div className="flex flex-wrap justify-between gap-2 text-sm">
                  <span className="font-medium">🎯 Meta do dia: {formatMinutes(goalNs)}</span>
                  <span className="text-muted-foreground">
                    {formatMinutes(todayFocusMs * 1e6)} acumulado{pomo ? " (sessão ativa)" : ""}
                  </span>
                </div>
                <Progress value={Math.max(3, progress * 100)} className="mt-3 h-2" />
              </CardContent>
            </Card>
          )}

          <DnsCard onNavigate={onNavigate} />

          <SectionTitle count={blocks.length}>Bloqueios ativos</SectionTitle>
          {blocks.length === 0 ? (
            <EmptyCard>
              <p>Nenhum bloqueio ativo no momento.</p>
              <p className="mt-1">Bloqueie um site ou um preset para começar a focar.</p>
            </EmptyCard>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {blocks.map((b) => (
                <BlockCard key={b.domain} block={b} />
              ))}
            </div>
          )}
        </>
      )}
    </Screen>
  );
}

function BlockCard({ block }: { block: Block }) {
  const ms = useCountdown(block.expires_at);
  return (
    <Card className="gap-2 transition-all duration-200 hover:-translate-y-0.5 hover:ring-emerald-500/40">
      <CardContent className="flex flex-col gap-2 px-4 py-4">
        <div className="flex items-center justify-between gap-2">
          <span className="text-sm font-semibold break-all">{block.domain}</span>
          <Badge variant="secondary" className="bg-emerald-500/10 text-emerald-500">
            ativo
          </Badge>
        </div>
        <div className="font-mono text-2xl font-bold tabular-nums text-primary">{formatMs(ms)}</div>
        <div className="flex justify-between text-xs text-muted-foreground">
          <span>início {formatClock(block.started_at)}</span>
          <span>fim {formatClock(block.expires_at)}</span>
        </div>
      </CardContent>
    </Card>
  );
}

/** Card do servidor DNS: status resumido + atalho para a tela Rede. */
function DnsCard({ onNavigate }: { onNavigate: (s: ScreenId) => void }) {
  const { status } = useData();
  const enabled = status?.dns_enabled === true;
  const listening = status?.dns_listening === true;
  const queries = status?.dns_queries ?? 0;
  const blocked = status?.dns_blocked ?? 0;

  return (
    <Card className="flex-row items-center justify-between gap-4">
      <CardContent className="flex flex-1 flex-wrap items-center gap-3 px-5 py-4">
        <div
          className={cn(
            "grid size-10 shrink-0 place-items-center rounded-xl bg-muted ring-1 ring-border",
            listening && "bg-emerald-500/10 text-emerald-500 ring-emerald-500/30",
          )}
        >
          <Network className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">Servidor DNS</p>
          <p className="text-xs text-muted-foreground">
            {listening
              ? `Ativo — ${queries.toLocaleString("pt-BR")} consultas, ${blocked.toLocaleString("pt-BR")} bloqueios`
              : enabled
                ? "Habilitado, mas parado"
                : "Desativado"}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => onNavigate("rede")}>
          Gerenciar
        </Button>
      </CardContent>
    </Card>
  );
}
