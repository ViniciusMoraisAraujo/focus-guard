import { useCallback, useEffect, useState } from "react";
import {
  ArrowRight,
  BookOpen,
  Check,
  Copy,
  EyeOff,
  Laptop,
  Loader2,
  Network,
  Play,
  Plus,
  Power,
  RefreshCw,
  ServerCog,
  ShieldBan,
  Trash2,
} from "lucide-react";
import { api, execAction } from "@/api/client";
import type { Device, TelemetryEntry, TelemetrySummary } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Screen, ScreenHeader } from "@/components/screen";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useData, type Screen as ScreenId } from "@/context";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";

// Rede.tsx — servidor DNS sinkhole ("Rei da Rede"). Controla o dns-start/stop
// do daemon, o upstream (dns-set-upstream, persistido no state.json) e mostra
// o estado ao vivo (listening, upstream, consultas, bloqueios e diagnóstico
// de porta 53).
export function Rede({ onNavigate }: { onNavigate: (s: ScreenId) => void }) {
  const { daemonUp, status, refresh } = useData();
  const [busy, setBusy] = useState<null | "start" | "stop">(null);
  const [upstreamBusy, setUpstreamBusy] = useState<string | null>(null);
  const [customUpstream, setCustomUpstream] = useState("");
  const [telemetry, setTelemetry] = useState<{
    entries: TelemetryEntry[];
    summary: TelemetrySummary[];
    total: number;
  } | null>(null);

  const dns = status;
  const enabled = dns?.dns_enabled === true;
  const listening = dns?.dns_listening === true;
  const bindError = dns?.dns_bind_error;
  const activeUpstream = dns?.dns_upstream;

  // Telemetria do sinkhole (Fase 1.2): polling silencioso de 5s enquanto a
  // aba está aberta e o sinkhole está ativo — "o que foi bloqueado e de onde".
  const pollTelemetry = useCallback(async () => {
    if (!listening) return;
    try {
      const r = await api.dnsTelemetry(30);
      if (r.success) {
        setTelemetry({
          entries: r.telemetry_entries ?? [],
          summary: r.telemetry_summary ?? [],
          total: r.telemetry_total ?? 0,
        });
      }
    } catch {
      /* best-effort: sem rede o próximo ciclo re-tenta */
    }
  }, [listening]);

  useEffect(() => {
    if (!listening) {
      setTelemetry(null);
      return;
    }
    void pollTelemetry();
    const id = window.setInterval(() => void pollTelemetry(), 5000);
    return () => window.clearInterval(id);
  }, [listening, pollTelemetry]);

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
            <CardContent className="flex flex-col gap-4 px-5 py-4">
              <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
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
              </div>

              {/* IP/MAC da máquina — os valores da reserva DHCP do roteador */}
              <LanInfoStrip ip={dns?.lan_ip} mac={dns?.lan_mac} />
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

          {/* Políticas por dispositivo (Fase 4 — edição Server) */}
          <DevicesCard listening={listening} />

          {/* Atividade bloqueada (telemetria do sinkhole — Fase 1.2) */}
          <Card>
            <CardContent className="flex flex-col gap-4 px-5 py-4">
              <div className="flex flex-wrap items-center gap-2">
                <ShieldBan className="size-4 text-muted-foreground" />
                <h3 className="font-heading text-base font-semibold">Atividade bloqueada</h3>
                {telemetry && (
                  <Badge variant="secondary" className="ml-auto font-mono">
                    {telemetry.total.toLocaleString("pt-BR")} no total
                  </Badge>
                )}
              </div>

              {!listening ? (
                <p className="text-sm text-muted-foreground">
                  Ligue o sinkhole para ver o que está sendo bloqueado na rede.
                </p>
              ) : telemetry === null ? (
                <div
                  className="flex items-center gap-2 py-2 text-sm text-muted-foreground"
                  aria-busy="true"
                >
                  <Loader2 className="size-4 animate-spin" />
                  Carregando atividade…
                </div>
              ) : telemetry.summary.length === 0 ? (
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                  <EyeOff className="size-4" />
                  Nenhum bloqueio registrado ainda — a rede está limpa.
                </p>
              ) : (
                <div className="flex flex-col gap-3">
                  <ul className="divide-y">
                    {telemetry.summary.slice(0, 8).map((s) => (
                      <li
                        key={s.domain}
                        className="group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 py-2 text-sm"
                      >
                        <div className="flex min-w-0 flex-col gap-0.5">
                          <span className="truncate font-medium" title={s.domain}>
                            {s.domain}
                          </span>
                          <span className="truncate font-mono text-[11px] text-muted-foreground">
                            {s.last_ips.join(" · ")}
                          </span>
                        </div>
                        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                          {s.count.toLocaleString("pt-BR")}×
                        </span>
                      </li>
                    ))}
                  </ul>
                  {telemetry.entries.length > 0 && (
                    <details className="text-sm">
                      <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                        Últimas consultas bloqueadas ({telemetry.entries.length})
                      </summary>
                      <ul className="mt-2 flex flex-col gap-1 border-t pt-2 font-mono text-xs text-muted-foreground">
                        {telemetry.entries.map((e, i) => (
                          <li key={i} className="flex items-center justify-between gap-3">
                            <span className="truncate">{e.domain}</span>
                            <span className="shrink-0">
                              {e.client_ip} · {fmtTime(e.timestamp)}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </details>
                  )}
                  <p className="text-xs text-muted-foreground">
                    IPs de origem são da rede local — o log fica só nesta
                    máquina (telemetry.jsonl).
                  </p>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Manual de configuração: link para a tela Guia (sistema + roteador por fabricante) */}
          <GuideCard onNavigate={onNavigate} />
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

// fmtTime renderiza um timestamp RFC3339 como HH:MM:SS local.
function fmtTime(rfc3339: string): string {
  const d = new Date(rfc3339);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

// DevicesCard — políticas por dispositivo (Fase 4, edição Server). Cada
// dispositivo da rede pode ter uma regra própria (bloquear tudo, allowlist
// ou herdar a regra global); sem regra, vale a global. A identificação é por
// IP (o mesmo que o sinkhole vê como origem das queries).
type DevicePolicy = "inherit" | "block_all" | "allow_list";

function DevicesCard({ listening }: { listening: boolean }) {
  const [devices, setDevices] = useState<Device[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Edição
  const [edit, setEdit] = useState<Device | null>(null);
  const [editIP, setEditIP] = useState("");
  const [editName, setEditName] = useState("");
  const [editPolicy, setEditPolicy] = useState<DevicePolicy>("inherit");
  const [editAllow, setEditAllow] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await api.devicesList();
      if (r.success) {
        setDevices(r.devices ?? []);
        setError(null);
      } else {
        setError(r.message ?? "Falha ao listar dispositivos.");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao listar dispositivos.");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openEdit = (d: Device | null) => {
    setEdit(d);
    setEditIP(d?.ip ?? "");
    setEditName(d?.name ?? "");
    setEditPolicy((d?.policy as DevicePolicy) ?? "inherit");
    setEditAllow(d?.allowed_domains?.join(", ") ?? "");
  };

  const save = async () => {
    const ip = editIP.trim();
    if (!ip) {
      toast("Informe o IP do dispositivo.", "err");
      return;
    }
    if (editPolicy === "allow_list" && !editAllow.trim()) {
      toast("A política allow_list exige ao menos um domínio permitido.", "err");
      return;
    }
    const device: Device = {
      ip,
      name: editName.trim() || undefined,
      policy: editPolicy,
      allowed_domains:
        editPolicy === "allow_list"
          ? editAllow
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean)
          : undefined,
    };
    setBusy(true);
    try {
      const r = await api.devicesUpsert(device);
      toast(r.message ?? (r.success ? "Política atualizada." : "Falha ao salvar."), r.success ? "ok" : "err");
      if (r.success) {
        setEdit(null);
        void load();
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Falha ao salvar.", "err");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (ip: string) => {
    setBusy(true);
    try {
      const r = await api.devicesRemove(ip);
      toast(r.message ?? (r.success ? "Dispositivo removido." : "Falha ao remover."), r.success ? "ok" : "err");
      if (r.success) void load();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Falha ao remover.", "err");
    } finally {
      setBusy(false);
    }
  };

  const policyLabel = (p?: string): string => {
    switch (p) {
      case "block_all":
        return "bloquear tudo";
      case "allow_list":
        return "allowlist";
      default:
        return "herdar global";
    }
  };

  return (
    <Card>
      <CardContent className="flex flex-col gap-4 px-5 py-4">
        <div className="flex flex-wrap items-center gap-2">
          <Laptop className="size-4 text-muted-foreground" />
          <h3 className="font-heading text-base font-semibold">Dispositivos</h3>
          {listening && (
            <Button size="sm" variant="secondary" className="ml-auto" onClick={() => openEdit(null)} disabled={busy}>
              <Plus /> Adicionar
            </Button>
          )}
        </div>
        <p className="-mt-3 text-sm text-muted-foreground">
          Políticas por IP da rede (edição Server). Sem regra, o dispositivo
          segue a política global do sinkhole.
        </p>

        {!listening ? (
          <p className="text-sm text-muted-foreground">
            Ligue o sinkhole para gerenciar políticas por dispositivo.
          </p>
        ) : error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : devices === null ? (
          <div className="flex items-center gap-2 py-1 text-sm text-muted-foreground" aria-busy="true">
            <Loader2 className="size-4 animate-spin" />
            Carregando dispositivos…
          </div>
        ) : devices.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            Nenhum dispositivo configurado — a regra global vale para toda a rede.
          </p>
        ) : (
          <ul className="divide-y">
            {devices.map((d) => (
              <li key={d.ip} className="flex items-center gap-3 py-2.5 text-sm">
                <span className="font-mono">{d.ip}</span>
                <span className="min-w-0 flex-1 truncate">{d.name || "(sem nome)"}</span>
                <Badge variant={d.policy === "inherit" || !d.policy ? "outline" : "secondary"}>
                  {policyLabel(d.policy)}
                </Badge>
                {d.policy === "allow_list" && d.allowed_domains?.length ? (
                  <span className="hidden max-w-40 truncate font-mono text-xs text-muted-foreground md:inline">
                    {d.allowed_domains.join(", ")}
                  </span>
                ) : null}
                <div className="ml-auto flex shrink-0 gap-1">
                  <Button variant="ghost" size="icon-sm" onClick={() => openEdit(d)} disabled={busy} aria-label={`Editar ${d.ip}`} title="Editar">
                    <Network />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                    onClick={() => void remove(d.ip)}
                    disabled={busy}
                    aria-label={`Remover ${d.ip}`}
                    title="Remover"
                  >
                    <Trash2 />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <Dialog open={edit !== null} onOpenChange={(o) => !o && setEdit(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{edit ? `Editar ${edit.name || edit.ip}` : "Novo dispositivo"}</DialogTitle>
            <DialogDescription asChild>
              <p>Defina a política deste IP na rede. Sem regra (herdar global), o sinkhole decide normalmente.</p>
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="dev-ip">IP</Label>
              <Input id="dev-ip" placeholder="ex.: 192.168.1.50" value={editIP} onChange={(e) => setEditIP(e.target.value)} disabled={busy || edit !== null} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="dev-name">Nome (opcional)</Label>
              <Input id="dev-name" placeholder="ex.: TV da sala" value={editName} onChange={(e) => setEditName(e.target.value)} disabled={busy} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="dev-policy">Política</Label>
              <Select
                value={editPolicy}
                onValueChange={(v) => setEditPolicy(v as DevicePolicy)}
                disabled={busy}
              >
                <SelectTrigger id="dev-policy">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="inherit">herdar global (recomendado)</SelectItem>
                  <SelectItem value="block_all">bloquear tudo</SelectItem>
                  <SelectItem value="allow_list">allowlist (só domínios permitidos)</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {editPolicy === "allow_list" && (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="dev-allow">Domínios permitidos (separados por vírgula)</Label>
                <Input id="dev-allow" placeholder="ex.: example.com, github.com" value={editAllow} onChange={(e) => setEditAllow(e.target.value)} disabled={busy} />
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setEdit(null)} disabled={busy}>
              Cancelar
            </Button>
            <Button onClick={() => void save()} disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <Check />} Salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

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

// LanInfoStrip — IP/MAC da máquina no card principal da Rede: os valores que
// entram na reserva DHCP do roteador (IP fixo + DNS primário). Best-effort:
// some quando o daemon ainda não reportou a LAN.
function LanInfoStrip({ ip, mac }: { ip?: string; mac?: string }) {
  const copy = async (text: string, label: string) => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      toast(`${label} copiado.`, "ok");
    } catch {
      toast("Não foi possível copiar.", "err");
    }
  };

  if (!ip) return null;

  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-border pt-3">
      <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <Network className="size-3.5 shrink-0" />
        IP da máquina
        <code className="rounded bg-muted px-1 font-mono text-foreground">{ip}</code>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Copiar IP"
          title="Copiar IP"
          onClick={() => void copy(ip, "IP")}
        >
          <Copy className="size-3.5" />
        </Button>
      </span>
      <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
        <Laptop className="size-3.5 shrink-0" />
        MAC
        <code className="rounded bg-muted px-1 font-mono text-foreground">{mac || "—"}</code>
        {mac && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Copiar MAC"
            title="Copiar MAC"
            onClick={() => void copy(mac, "MAC")}
          >
            <Copy className="size-3.5" />
          </Button>
        )}
      </span>
      <span className="text-xs text-muted-foreground">
        Use na reserva DHCP do roteador — passo a passo na tela Guia.
      </span>
    </div>
  );
}

// GuideCard — atalho para o manual completo (tela Guia): sistema Windows,
// roteador com guias por fabricante e diagnóstico + IP/MAC da máquina (os
// valores da reserva DHCP, com copiar). Mantém a tela Rede enxuta.
function GuideCard({ onNavigate }: { onNavigate: (s: ScreenId) => void }) {
  const { status } = useData();
  return (
    <Card>
      <CardContent className="flex flex-col gap-3 px-5 py-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-3">
            <BookOpen className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="flex flex-col gap-0.5">
              <h3 className="font-heading text-base font-semibold">
                Manual de configuração
              </h3>
              <p className="text-sm text-muted-foreground">
                Como apontar o sinkhole no sistema e no roteador — com guias
                por fabricante (ZTE, TP-Link, Huawei…) e diagnóstico.
              </p>
            </div>
          </div>
          <Button
            variant="outline"
            className="shrink-0"
            onClick={() => onNavigate("guia")}
          >
            <BookOpen /> Abrir guia completo
          </Button>
        </div>

        {/* IP/MAC da máquina — os valores da reserva DHCP do roteador */}
        <LanInfoStrip ip={status?.lan_ip} mac={status?.lan_mac} />
      </CardContent>
    </Card>
  );
}
