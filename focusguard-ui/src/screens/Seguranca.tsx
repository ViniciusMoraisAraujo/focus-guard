import { useEffect, useMemo, useState } from "react";
import { RefreshCw, ShieldAlert, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import type { Block, TamperEvent } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useData } from "@/context";

const ALL_INTERNET = "*all-internet*";

/**
 * clockLockdownAtivo devolve o bloqueio preventivo do Clock Guard (Fase 2)
 * ATIVO neste momento: o sentinela all-internet com origem "clock-guard" no
 * status, ainda não expirado. O tamper-log só registra burlas CONFIRMADAS
 * por NTP — o lockdown de suspeita (NTP offline/falhou, aplicado sem evento)
 * só aparece por aqui. O status vive no data-context e é atualizado em tempo
 * real pelo SSE blocks-changed (o scheduler dispara no apply/release).
 */
function clockLockdownAtivo(blocks: Block[] | undefined): Block | null {
  const b = (blocks ?? []).find(
    (blk) => blk.domain === ALL_INTERNET && blk.source === "clock-guard",
  );
  if (!b) return null;
  const exp = new Date(b.expires_at).getTime();
  if (Number.isNaN(exp) || exp <= Date.now()) return null;
  return b;
}

function fmtDate(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return d.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function Seguranca() {
  const { daemonUp, status } = useData();
  const [events, setEvents] = useState<TamperEvent[] | null>(null);
  const lockdown = useMemo(() => clockLockdownAtivo(status?.blocks), [status?.blocks]);

  const load = () => {
    api
      .tamperLog()
      .then((r) => setEvents(r.success ? (r.tamper_log ?? []) : []))
      .catch(() => setEvents([]));
  };

  useEffect(() => {
    load();
  }, []);

  return (
    <Screen>
      <ScreenHeader
        title="Segurança"
        subtitle="Tentativas de adulteração dos arquivos de bloqueio (hosts/estado) e do relógio do sistema detectadas e neutralizadas pelo daemon."
        actions={
          <Button variant="outline" onClick={load}>
            <RefreshCw /> Recarregar
          </Button>
        }
      />

      {daemonUp === true && lockdown && (
        <Card
          role="alert"
          aria-label="Bloqueio preventivo do relógio ativo"
          className="border-destructive/40 bg-destructive/5 ring-destructive/30"
        >
          <CardContent className="flex items-start gap-3 px-5 py-4">
            <TriangleAlert className="mt-0.5 size-5 shrink-0 text-destructive" aria-hidden />
            <div className="min-w-0 flex-1">
              <h3 className="font-heading text-sm font-semibold text-destructive">
                Bloqueio preventivo do relógio ativo
              </h3>
              <p className="mt-1 text-sm text-muted-foreground">
                O relógio do sistema foi alterado além da tolerância e o horário ainda não foi
                validado online (NTP indisponível). Toda a internet está bloqueada até a
                sincronização validar o horário real, ou até{" "}
                <strong>{fmtDate(lockdown.expires_at)}</strong>.
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para ver o histórico.</p>
        </EmptyCard>
      ) : events === null ? (
        <div className="flex flex-col gap-3" aria-label="Carregando histórico">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16 w-full rounded-xl" />
          ))}
        </div>
      ) : events.length === 0 ? (
        <EmptyCard>
          <ShieldAlert className="mx-auto mb-2 size-6 text-muted-foreground" />
          <p>Nenhuma tentativa registrada. 👌</p>
          <p className="mt-1">O FocusGuard restaura automaticamente qualquer alteração externa.</p>
        </EmptyCard>
      ) : (
        <div className="flex flex-col gap-3">
          {events.map((e, i) => {
            const isClock = e.source === "clock";
            return (
              <Card key={`${e.at}-${i}`} size="sm">
                <CardContent className="flex flex-col gap-2 px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={isClock ? "destructive" : "default"}>
                      {e.source === "hosts" ? "hosts" : e.source === "clock" ? "relógio" : "estado"}
                    </Badge>
                    <Badge variant="secondary">
                      {isClock
                        ? "relógio fora da hora real"
                        : e.action === "lockdown"
                          ? "bloqueio preventivo"
                          : e.action === "restore"
                            ? "restaurado"
                            : "reconciliado"}
                    </Badge>
                    <span className="text-xs text-muted-foreground">{fmtDate(e.at)}</span>
                  </div>
                  {isClock && (
                    <p className="m-0 text-xs text-destructive/90">
                      Relógio do sistema fora da hora real (confirmado por NTP) — expirações
                      ajustadas para a hora real, sem bloqueio. Verifique o RTC/fuso (ex.: dual
                      boot).
                    </p>
                  )}
                  {e.detail && (
                    <p className="m-0 text-xs break-all text-muted-foreground">{e.detail}</p>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      )}
    </Screen>
  );
}
