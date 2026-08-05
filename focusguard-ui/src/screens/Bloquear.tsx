import { useState } from "react";
import { Lock, ShieldCheck } from "lucide-react";
import { api, DaemonError } from "@/api/client";
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useApp } from "@/context";
import { formatClock } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

const DURATIONS = [
  { label: "30 min", value: "30m" },
  { label: "1 h", value: "1h" },
  { label: "2 h", value: "2h" },
  { label: "4 h", value: "4h" },
  { label: "8 h", value: "8h" },
];

// ConflictState guarda o domínio que o daemon recusou com Conflict:true, para
// o dialog "somar/substituir" poder reenviar com o domínio congelado (mesmo se
// o usuário editar o input enquanto o dialog está aberto).
interface ConflictState {
  domain: string;
  expires?: string;
}

export function Bloquear() {
  const { presets, toast, daemonUp, refresh } = useApp();
  const [mode, setMode] = useState<"preset" | "domain">("preset");
  const [preset, setPreset] = useState<string>("");
  const [domain, setDomain] = useState<string>("");
  const [duration, setDuration] = useState<string>("1h");
  const [customMin, setCustomMin] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState<ConflictState | null>(null);

  const effectiveDuration = duration === "custom" && customMin ? `${customMin}m` : duration;

  const submit = async (resolve?: { extend?: boolean; replace?: boolean }) => {
    if (!resolve) {
      if (!effectiveDuration) {
        toast("Escolha uma duração.", "err");
        return;
      }
      if (mode === "preset" && !preset) {
        toast("Escolha uma categoria.", "err");
        return;
      }
      if (mode === "domain" && !domain.trim()) {
        toast("Digite o domínio a bloquear.", "err");
        return;
      }
    }
    setBusy(true);
    try {
      const resp = await api.block({
        preset: mode === "preset" ? preset : undefined,
        domain: conflict ? conflict.domain : mode === "domain" ? domain.trim() : undefined,
        duration: effectiveDuration,
        extend: resolve?.extend,
        replace: resolve?.replace,
      });
      if (resp.conflict) {
        setConflict({
          domain: mode === "domain" ? domain.trim() : "",
          expires: resp.conflict_block?.expires_at,
        });
        return;
      }
      setConflict(null);
      if (!resp.success) {
        toast(resp.message ?? "Falha ao bloquear.", "err");
      } else {
        toast(resp.message ?? "Bloqueio aplicado!");
        setDomain("");
        await refresh();
      }
    } catch (e) {
      setConflict(null);
      toast(e instanceof DaemonError ? e.message : "Erro ao bloquear.", "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScreenHeader
        title="Bloquear"
        subtitle="Escolha uma categoria ou um domínio e a duração do bloqueio."
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — bloqueio indisponível.</p>
        </EmptyCard>
      ) : (
        <Card className="max-w-2xl">
          <CardContent className="flex flex-col gap-5 px-5 py-5">
            <Tabs value={mode} onValueChange={(v) => setMode(v as "preset" | "domain")}>
              <TabsList className="w-full">
                <TabsTrigger value="preset" className="flex-1">
                  <ShieldCheck /> Categoria (preset)
                </TabsTrigger>
                <TabsTrigger value="domain" className="flex-1">
                  <Lock /> Domínio específico
                </TabsTrigger>
              </TabsList>
            </Tabs>

            {mode === "preset" ? (
              <div className="flex flex-wrap gap-2">
                {presets.map((p) => (
                  <Button
                    key={p.name}
                    type="button"
                    variant={preset === p.name ? "default" : "outline"}
                    title={p.domains.join(", ")}
                    onClick={() => setPreset(p.name)}
                    className="h-7"
                  >
                    {p.label}
                    <span className="rounded-full bg-muted px-1.5 text-xs tabular-nums">
                      {p.domains.length}
                    </span>
                  </Button>
                ))}
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                <Label htmlFor="domain">Domínio</Label>
                <Input
                  id="domain"
                  type="text"
                  placeholder="ex: youtube.com"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && void submit()}
                />
              </div>
            )}

            <div className="flex flex-col gap-2">
              <Label>Duração</Label>
              <div className="flex flex-wrap gap-2">
                {DURATIONS.map((d) => (
                  <Button
                    key={d.value}
                    type="button"
                    variant={duration === d.value ? "default" : "outline"}
                    onClick={() => setDuration(d.value)}
                    className="h-7"
                  >
                    {d.label}
                  </Button>
                ))}
                <Button
                  type="button"
                  variant={duration === "custom" ? "default" : "outline"}
                  onClick={() => setDuration("custom")}
                  className="h-7"
                >
                  personalizado
                </Button>
              </div>
              {duration === "custom" && (
                <div className={cn("flex items-center gap-2")}>
                  <Input
                    type="number"
                    min={1}
                    placeholder="minutos"
                    className="max-w-28"
                    value={customMin}
                    onChange={(e) => setCustomMin(e.target.value)}
                  />
                  <span className="text-sm text-muted-foreground">minutos</span>
                </div>
              )}
            </div>

            <Button onClick={() => void submit()} disabled={busy} className="w-full" size="lg">
              <Lock /> {busy ? "Bloqueando…" : "Bloquear"}
            </Button>
          </CardContent>
        </Card>
      )}

      <Dialog
        open={conflict !== null}
        onOpenChange={(open) => !open && setConflict(null)}
      >
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>Já está bloqueado</DialogTitle>
            <DialogDescription>
              {conflict?.domain} já está bloqueado
              {conflict?.expires ? <> até {formatClock(conflict.expires)}</> : null}. O
              que você quer fazer?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setConflict(null)}
            >
              Cancelar
            </Button>
            <Button variant="secondary" disabled={busy} onClick={() => void submit({ replace: true })}>
              Substituir
            </Button>
            <Button disabled={busy} onClick={() => void submit({ extend: true })}>
              Somar {effectiveDuration}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}
