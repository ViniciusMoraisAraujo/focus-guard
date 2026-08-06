import { useState } from "react";
import {
  ArrowRight,
  Check,
  Loader2,
  Network,
  Play,
  Power,
  RefreshCw,
  ServerCog,
} from "lucide-react";
import { execAction } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Screen, ScreenHeader } from "@/components/screen";
import { useData } from "@/context";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";

// Rede.tsx — servidor DNS sinkhole ("Rei da Rede"). Controla o dns-start/stop
// do daemon, o upstream (dns-set-upstream, persistido no state.json) e mostra
// o estado ao vivo (listening, upstream, consultas, bloqueios e diagnóstico
// de porta 53).
export function Rede() {
  const { daemonUp, status, refresh } = useData();
  const [busy, setBusy] = useState<null | "start" | "stop">(null);
  const [upstreamBusy, setUpstreamBusy] = useState<string | null>(null);
  const [customUpstream, setCustomUpstream] = useState("");

  const dns = status;
  const enabled = dns?.dns_enabled === true;
  const listening = dns?.dns_listening === true;
  const bindError = dns?.dns_bind_error;
  const activeUpstream = dns?.dns_upstream;

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

  const setUpstream = async (upstream: string) => {
    if (!upstream.trim() || upstreamBusy !== null) return;
    setUpstreamBusy(upstream);
    try {
      const { ok, message } = await execAction({
        action: "dns-set-upstream",
        upstream: upstream.trim(),
      });
      toast(message || (ok ? "Upstream atualizado." : "Falha ao alterar o upstream."), ok ? "ok" : "err");
      if (ok) setCustomUpstream("");
      await refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Falha ao alterar o upstream.", "err");
    } finally {
      setUpstreamBusy(null);
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
          <Button variant="outline" onClick={() => void refresh()} disabled={busy !== null || upstreamBusy !== null}>
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
                      ? `Ouvindo em ${dns?.dns_addr ?? "—"} (upstream ${activeUpstream ?? "—"})`
                      : enabled
                        ? "O servidor não subiu — veja o diagnóstico abaixo."
                        : "Ligue o sinkhole para proteger a rede inteira."}
                  </p>
                </div>
              </div>

              <div className="flex shrink-0 gap-2">
                {!enabled && (
                  <Button
                    onClick={() => void act("start")}
                    disabled={busy !== null || upstreamBusy !== null}
                  >
                    {busy === "start" ? <Loader2 className="animate-spin" /> : <Play />} Ligar
                  </Button>
                )}
                {enabled && (
                  <Button
                    variant="destructive"
                    onClick={() => void act("stop")}
                    disabled={busy !== null || upstreamBusy !== null}
                  >
                    {busy === "stop" ? <Loader2 className="animate-spin" /> : <Power />} Desligar
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>

          <UpstreamCard
            active={activeUpstream ?? ""}
            busy={upstreamBusy}
            custom={customUpstream}
            listening={listening}
            onCustomChange={setCustomUpstream}
            onApply={setUpstream}
          />

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

// Resolvers públicos pré-definidos. Os valores são enviados como host:porta
// (o daemon aceita host puro, mas a forma explícita casa com o dns_upstream
// normalizado que o status devolve).
const UPSTREAMS = [
  { label: "Cloudflare", host: "1.1.1.2", note: "bloqueia malware", value: "1.1.1.2:53" },
  { label: "Google", host: "8.8.8.8", note: "resolução clássica", value: "8.8.8.8:53" },
  { label: "Quad9", host: "9.9.9.9", note: "bloqueia malware", value: "9.9.9.9:53" },
  { label: "AdGuard", host: "94.140.14.14", note: "bloqueia anúncios", value: "94.140.14.14:53" },
];

// UpstreamCard — escolha do resolver usado para consultas legítimas. A troca
// persiste no state.json (scheduler) e reinicia o listener se estiver ligado.
function UpstreamCard({
  active,
  busy,
  custom,
  listening,
  onCustomChange,
  onApply,
}: {
  active: string;
  busy: string | null;
  custom: string;
  listening: boolean;
  onCustomChange: (v: string) => void;
  onApply: (upstream: string) => Promise<void>;
}) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-4 px-5 py-4">
        <div className="flex flex-wrap items-center gap-2">
          <ServerCog className="size-4 text-muted-foreground" />
          <h3 className="font-heading text-base font-semibold">Upstream DNS</h3>
          {active && (
            <Badge variant="secondary" className="ml-auto font-mono">
              atual: {active}
            </Badge>
          )}
        </div>
        <p className="-mt-3 text-sm text-muted-foreground">
          O resolver que responde pelas consultas liberadas. As consultas
          bloqueadas nunca chegam nele.
        </p>
        {listening && (
          <p className="-mt-3 text-xs text-muted-foreground">
            Trocar o upstream reinicia o servidor e zera os contadores abaixo.
          </p>
        )}

        <div className="flex flex-wrap gap-2">
          {UPSTREAMS.map((u) => {
            const isActive = active === u.value;
            const isBusy = busy === u.value;
            return (
              <Button
                key={u.value}
                type="button"
                variant={isActive ? "default" : "outline"}
                onClick={() => void onApply(u.value)}
                disabled={busy !== null}
                className="h-8"
                title={`${u.label} — ${u.note}`}
              >
                {isBusy ? <Loader2 className="animate-spin" /> : <span className="font-mono">{u.host}</span>}
                {u.label}
                {isActive && <Check className="size-3.5" />}
              </Button>
            );
          })}
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Input
            type="text"
            placeholder="ou informe outro: ex. 1.1.1.1:53"
            className="max-w-64 font-mono"
            value={custom}
            onChange={(e) => onCustomChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void onApply(custom.trim());
            }}
            disabled={busy !== null}
            aria-label="Upstream DNS personalizado"
          />
          <Button
            variant="secondary"
            onClick={() => void onApply(custom.trim())}
            disabled={busy !== null || !custom.trim()}
          >
            {busy === custom.trim() && busy !== null ? (
              <Loader2 className="animate-spin" />
            ) : (
              <ArrowRight />
            )}
            Aplicar
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
