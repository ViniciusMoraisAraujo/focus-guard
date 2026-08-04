import { useState } from "react";
import { Siren } from "lucide-react";
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
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useApp } from "@/context";
import { cn } from "@/lib/utils";

const DURATIONS = [
  { label: "15 min", value: "15m" },
  { label: "30 min", value: "30m" },
  { label: "1 h", value: "1h" },
  { label: "2 h", value: "2h" },
];

export function Panico() {
  const { toast, daemonUp, refresh, status } = useApp();
  const [duration, setDuration] = useState("30m");
  const [allow, setAllow] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const panicActive = (status?.blocks ?? []).some((b) => b.domain === "*all-internet*");

  const run = async () => {
    setConfirmOpen(false);
    setBusy(true);
    const allowlist = allow
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      const resp = await api.blockAll(duration, allowlist);
      if (!resp.success) {
        toast(resp.message ?? "Falha ao bloquear a internet.", "err");
      } else {
        toast(resp.message ?? "Internet bloqueada!");
        await refresh();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Erro ao bloquear a internet.", "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScreenHeader
        title="Modo pânico"
        subtitle="Bloqueia TODA a internet por um período. Use em momentos de decisão. 😤"
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — bloqueio indisponível.</p>
        </EmptyCard>
      ) : (
        <Card className={cn("max-w-xl", panicActive && "ring-destructive/40")}>
          <CardContent className="flex flex-col gap-5 px-5 py-5">
            <Button
              type="button"
              onClick={() => setConfirmOpen(true)}
              disabled={busy}
              className={cn(
                "h-auto flex-col gap-3 border-2 border-destructive/40 bg-destructive/10 py-8 text-destructive hover:bg-destructive/20 hover:text-destructive",
                panicActive && "border-destructive/70",
              )}
            >
              <Siren className="size-9 animate-pulse" />
              <span className="text-lg font-bold">
                {panicActive ? "Pânico em andamento" : "Bloquear toda a internet"}
              </span>
            </Button>

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
              </div>
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="allow">Allowlist (opcional — o que continua acessível)</Label>
              <Textarea
                id="allow"
                placeholder="docs.google.com, github.com"
                value={allow}
                onChange={(e) => setAllow(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Separe por vírgula. Vazio = bloquear toda a internet (sem exceções).
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirmar modo pânico</DialogTitle>
            <DialogDescription asChild>
              <div>
                <p>
                  Toda a internet ficará bloqueada por <strong>{duration}</strong>
                  {allow.trim() ? " — exceto os domínios permitidos." : ", sem exceções."}
                </p>
                <p className="mt-2">Bloqueios temporários não podem ser desfeitos manualmente.</p>
              </div>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)} disabled={busy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={() => void run()}
              disabled={busy}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {busy ? "Bloqueando…" : "Bloquear internet"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}
