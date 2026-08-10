import { useEffect, useState } from "react";
import { RefreshCw, ShieldAlert } from "lucide-react";
import { api } from "@/api/client";
import type { TamperEvent } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useData } from "@/context";

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
  const { daemonUp } = useData();
  const [events, setEvents] = useState<TamperEvent[] | null>(null);

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
                      {e.action === "lockdown"
                        ? "bloqueio preventivo"
                        : e.action === "restore"
                          ? "restaurado"
                          : "reconciliado"}
                    </Badge>
                    <span className="text-xs text-muted-foreground">{fmtDate(e.at)}</span>
                  </div>
                  {isClock && (
                    <p className="m-0 text-xs text-destructive/90">
                      Relógio do sistema adulterado — bloqueio preventivo aplicado até a
                      sincronização online validar o horário real.
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
