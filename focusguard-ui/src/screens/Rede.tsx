import { useState } from "react";
import { Loader2, Network, Play, Power, RefreshCw, ServerCog } from "lucide-react";
import { execAction } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Screen, ScreenHeader } from "@/components/screen";
import { useApp } from "@/context";
import { cn } from "@/lib/utils";

// Rede.tsx — servidor DNS sinkhole ("Rei da Rede"). Controla o dns-start/stop
// do daemon e mostra o estado ao vivo (listening, upstream, consultas,
// bloqueios e diagnóstico de porta 53).
export function Rede() {
  const { daemonUp, status, refresh, toast } = useApp();
  const [busy, setBusy] = useState<null | "start" | "stop">(null);

  const dns = status;
  const enabled = dns?.dns_enabled === true;
  const listening = dns?.dns_listening === true;
  const bindError = dns?.dns_bind_error;

  const act = async (action: "start" | "stop") => {
    setBusy(action);
    try {
      const { ok, message } = await execAction(
        action === "start" ? { action: "dns-start" } : { action: "dns-stop" },
      );
      toast(message || (action === "start" ? "Servidor DNS iniciado" : "Servidor DNS desligado"), ok ? "ok" : "err");
      await refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Falha ao executar a ação.", "err");
    } finally {
      setBusy(null);
    }
  };

  const counters = [
    { label: "Consultas", value: dns?.dns_queries ?? 0 },
    { label: "Bloqueios", value: dns?.dns_blocked ?? 0 },
  ];

  return (
    <Screen>
      <ScreenHeader
        title="Rede"
        subtitle="Servidor DNS sinkhole — bloqueia domínios para a rede inteira, sem depender de sessão de foco nem do arquivo hosts."
        actions={
          <Button variant="outline" onClick={() => void refresh()} disabled={busy !== null}>
            <RefreshCw /> Atualizar
          </Button>
        }
      />

      {daemonUp === false ? (
        <Card>
          <CardContent className="px-5 py-4 text-sm text-muted-foreground">
            O daemon está desligado — inicie o serviço para gerenciar o servidor DNS.
          </CardContent>
        </Card>
      ) : (
        <>
          <Card className={cn(enabled && listening && "ring-emerald-500/30")}>
            <CardContent className="flex flex-col gap-4 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-center gap-4">
                <div
                  className={cn(
                    "grid size-12 shrink-0 place-items-center rounded-xl bg-muted ring-1 ring-border",
                    listening && "bg-emerald-500/10 text-emerald-500 ring-emerald-500/30",
                  )}
                >
                  {listening ? <Network className="size-6" /> : <ServerCog className="size-6" />}
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="font-heading text-lg font-semibold">
                      {listening ? "Sinkhole ativo" : enabled ? "Habilitado, mas parado" : "Desativado"}
                    </h3>
                    <Badge
                      variant={listening ? "secondary" : "outline"}
                      className={cn(listening && "bg-emerald-500/10 text-emerald-500")}
                    >
                      {enabled ? "dns ligado" : "dns desligado"}
                    </Badge>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    {listening
                      ? `Ouvindo em ${dns?.dns_addr ?? "—"} (upstream ${dns?.dns_upstream ?? "—"})`
                      : enabled
                        ? "O servidor não subiu — veja o diagnóstico abaixo."
                        : "Ligue o sinkhole para proteger a rede inteira."}
                  </p>
                </div>
              </div>

              <div className="flex shrink-0 gap-2">
                {!enabled && (
                  <Button onClick={() => void act("start")} disabled={busy !== null}>
                    {busy === "start" ? <Loader2 className="animate-spin" /> : <Play />} Ligar
                  </Button>
                )}
                {enabled && (
                  <Button
                    variant="destructive"
                    onClick={() => void act("stop")}
                    disabled={busy !== null}
                  >
                    {busy === "stop" ? <Loader2 className="animate-spin" /> : <Power />} Desligar
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>

          {bindError && (
            <Card className="border-destructive/40 bg-destructive/5">
              <CardContent className="px-5 py-4">
                <p className="text-sm font-medium text-destructive">Porta 53 em uso</p>
                <p className="mt-1 text-sm break-all text-destructive/80">{bindError}</p>
                <p className="mt-2 text-sm text-muted-foreground">
                  A causa mais comum é o ICS do Windows:{" "}
                  <code className="rounded bg-muted px-1">
                    sc config SharedAccess start= disabled
                  </code>{" "}
                  e <code className="rounded bg-muted px-1">net stop SharedAccess</code>.
                </p>
              </CardContent>
            </Card>
          )}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {counters.map((c) => (
              <Card key={c.label}>
                <CardContent className="flex items-baseline justify-between px-5 py-4">
                  <span className="text-sm text-muted-foreground">{c.label}</span>
                  <span className="font-mono text-3xl font-bold tabular-nums text-primary">
                    {c.value.toLocaleString("pt-BR")}
                  </span>
                </CardContent>
              </Card>
            ))}
          </div>

          <Card>
            <CardContent className="flex flex-col gap-3 px-5 py-4">
              <h3 className="font-heading text-sm font-semibold text-muted-foreground">
                Configuração "Rei da Rede" no roteador
              </h3>
              <ol className="flex list-decimal flex-col gap-2 pl-5 text-sm text-muted-foreground">
                <li>
                  Fixe o IP do PC que roda o FocusGuard no DHCP do roteador
                  (ex.: <code className="rounded bg-muted px-1">192.168.1.100</code>).
                </li>
                <li>
                  Aponte o <strong className="text-foreground">DNS primário</strong> do DHCP para
                  o IP do PC.
                </li>
                <li>
                  Configure um DNS público de confiança (ex.:{" "}
                  <code className="rounded bg-muted px-1">1.1.1.1</code>) como{" "}
                  <strong className="text-foreground">DNS secundário</strong> — se o PC cair, a
                  rede continua navegando.
                </li>
              </ol>
            </CardContent>
          </Card>
        </>
      )}
    </Screen>
  );
}
