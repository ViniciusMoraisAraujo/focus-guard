import { useState } from "react";
import { Download, RefreshCw, Settings, ShieldCheck, Target } from "lucide-react";
import { api, DaemonError, execAction } from "@/api/client";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Screen, ScreenHeader } from "@/components/screen";
import { useApp } from "@/context";
import { formatMinutes } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

const GOALS = [
  { label: "2 h", minutes: 120 },
  { label: "4 h", minutes: 240 },
  { label: "6 h", minutes: 360 },
  { label: "8 h", minutes: 480 },
];

export function Configuracoes() {
  const { status, toast, daemonUp, refresh } = useApp();
  const [customGoal, setCustomGoal] = useState("");
  const [busy, setBusy] = useState(false);
  const [channel, setChannel] = useState("stable");
  const [confirmUpdate, setConfirmUpdate] = useState(false);
  const [updating, setUpdating] = useState(false);

  const goalNs = status?.goal ?? 0;
  const goalMin = Math.round(goalNs / 6e10);

  const setGoal = async (minutes: number) => {
    setBusy(true);
    try {
      const resp = await api.goalSet(minutes);
      if (!resp.success) {
        toast(resp.message ?? "Falha ao definir a meta.", "err");
      } else {
        toast("Meta diária atualizada!");
        await refresh();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Erro ao definir a meta.", "err");
    } finally {
      setBusy(false);
    }
  };

  const saveCustom = () => {
    const n = Number(customGoal);
    if (!Number.isFinite(n) || n <= 0 || n > 1440) {
      toast("Meta inválida (1 a 1440 minutos).", "err");
      return;
    }
    void setGoal(Math.round(n));
  };

  const applyUpdate = async () => {
    setUpdating(true);
    try {
      const res = await execAction({ action: "update", channel });
      toast(
        res.message ||
          (res.ok
            ? "Atualização aplicada — o daemon reinicia ao final."
            : "Falha ao atualizar."),
        res.ok ? "ok" : "err",
      );
      await refresh();
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Erro ao atualizar.", "err");
    } finally {
      setUpdating(false);
      setConfirmUpdate(false);
    }
  };

  return (
    <Screen>
      <ScreenHeader title="Configurações" subtitle="Meta diária, atualizações e estado do sistema." />

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-5 px-5 py-5">
          <div className="flex items-center gap-2">
            <Target className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Meta diária de foco</h3>
          </div>
          <p className="-mt-3 text-sm text-muted-foreground">
            {goalNs > 0 ? `Atual: ${formatMinutes(goalNs)} por dia` : "Nenhuma meta definida ainda."}
          </p>
          <div className="flex flex-wrap gap-2">
            {GOALS.map((g) => (
              <Button
                key={g.minutes}
                type="button"
                variant={g.minutes === goalMin ? "default" : "outline"}
                onClick={() => void setGoal(g.minutes)}
                disabled={busy}
                className="h-7"
              >
                {g.label}
              </Button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Input
              type="number"
              min={1}
              max={1440}
              placeholder="minutos personalizados"
              className="max-w-40"
              value={customGoal}
              onChange={(e) => setCustomGoal(e.target.value)}
            />
            <Button variant="secondary" onClick={saveCustom} disabled={busy}>
              Definir
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-5 px-5 py-5">
          <div className="flex items-center gap-2">
            <Download className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Atualizações</h3>
          </div>
          {status?.update_available ? (
            <p className="-mt-3 text-sm">
              Nova versão <strong>{status.update_version}</strong> disponível (atual:{" "}
              {status.current_version}).
            </p>
          ) : (
            <p className="-mt-3 text-sm text-muted-foreground">
              Você está na versão mais recente
              {status?.current_version ? ` (${status.current_version})` : ""}.
            </p>
          )}
          <div className="flex max-w-60 flex-col gap-2">
            <Label htmlFor="channel">Canal</Label>
            <Select value={channel} onValueChange={setChannel}>
              <SelectTrigger id="channel" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="stable">stable (recomendado)</SelectItem>
                <SelectItem value="beta">beta (prereleases)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void refresh()} disabled={busy}>
              <RefreshCw /> Verificar
            </Button>
            <Button disabled={!status?.update_available} onClick={() => setConfirmUpdate(true)}>
              <Download /> Aplicar atualização
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-3 px-5 py-5">
          <div className="flex items-center gap-2">
            <ShieldCheck className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Proteção do sistema</h3>
          </div>
          {status?.protection_error ? (
            <p className="text-sm text-muted-foreground">
              Não foi possível consultar o firewall: {status.protection_error}
            </p>
          ) : (
            <dl className="divide-y">
              <Row label="Regras de firewall (FocusGuard)">
                <Badge variant="secondary">{status?.firewall_rules ?? 0}</Badge>
              </Row>
              <Row label="Proteção DoH/DoT">
                <Badge variant={status?.doh_active ? "default" : "outline"}>
                  {status?.doh_active ? "ATIVA" : "inativa"}
                </Badge>
              </Row>
            </dl>
          )}
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-3 px-5 py-5">
          <div className="flex items-center gap-2">
            <Settings className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Sobre</h3>
          </div>
          <dl className="divide-y">
            <Row label="Interface web">FocusGuard UI</Row>
            <Row label="Versão do sistema">{status?.current_version ?? "—"}</Row>
            <Row label="Daemon">
              <span
                className={cn(
                  "font-medium",
                  daemonUp ? "text-emerald-500" : "text-muted-foreground",
                )}
              >
                {daemonUp ? "ativo" : "offline"}
              </span>
            </Row>
          </dl>
          <p className="text-xs text-muted-foreground">
            A interface web fica em <code className="rounded bg-muted px-1">http://127.0.0.1:48902</code>{" "}
            e conversa com o daemon apenas via localhost.
          </p>
        </CardContent>
      </Card>

      <Dialog open={confirmUpdate} onOpenChange={setConfirmUpdate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Aplicar atualização?</DialogTitle>
            <DialogDescription asChild>
              <p>
                Baixar e aplicar <strong>{status?.update_version}</strong> no canal{" "}
                <code className="rounded bg-muted px-1">{channel}</code>? O daemon atualiza os
                binários e reinicia ao final — a interface pode ficar indisponível por alguns
                instantes.
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmUpdate(false)} disabled={updating}>
              Cancelar
            </Button>
            <Button onClick={() => void applyUpdate()} disabled={updating}>
              {updating ? "Atualizando…" : "Atualizar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{children}</dd>
    </div>
  );
}
