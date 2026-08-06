import { useEffect, useState } from "react";
import { Play, Square, Timer } from "lucide-react";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { CircularTimer } from "@/components/circular-timer";
import { EmptyCard, Screen, ScreenHeader } from "@/components/screen";
import { useData } from "@/context";
import { formatClock, useCountdown } from "@/hooks/useCountdown";
import { useAction } from "@/hooks/use-action";
import { toast } from "@/lib/toast";

export function Pomodoro() {
  const { status, presets, refresh, daemonUp } = useData();
  const { busy, run } = useAction();

  const [defaults, setDefaults] = useState<{
    work: number;
    rest: number;
    cycles: number;
  } | null>(null);
  const [preset, setPreset] = useState("");
  const [work, setWork] = useState("25");
  const [rest, setRest] = useState("5");
  const [cycles, setCycles] = useState("4");
  const [strict, setStrict] = useState(false);
  const [save, setSave] = useState(false);
  const [label, setLabel] = useState("");
  const [confirmStop, setConfirmStop] = useState(false);

  useEffect(() => {
    api
      .pomodoroDefaults()
      .then((r) => {
        if (r.success) {
          const d = {
            work: r.pomodoro_work ?? 25,
            rest: r.pomodoro_rest ?? 5,
            cycles: r.pomodoro_cycles ?? 4,
          };
          setDefaults(d);
          setWork(String(d.work));
          setRest(String(d.rest));
          setCycles(String(d.cycles));
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!preset && presets.length > 0) setPreset(presets[0].name);
  }, [presets, preset]);

  const pomo = status?.pomodoro?.active ? status.pomodoro : null;
  const phaseUntilMs = useCountdown(pomo?.phase_until ?? null);

  // Referência do anel: usa os padrões atuais como proxy da duração da fase
  // (o daemon não expõe a duração da fase ativa no status).
  const ref = defaults ?? { work: 25, rest: 5 };
  const phaseTotalMs =
    (pomo?.phase === "rest" ? ref.rest : ref.work) * 60_000;

  const start = async () => {
    if (!preset) {
      toast("Escolha uma categoria (preset).", "err");
      return;
    }
    const res = await run(
      {
        action: "pomodoro",
        preset,
        work_min: Number(work) || undefined,
        rest_min: Number(rest) || undefined,
        cycles: Number(cycles) || undefined,
        strict,
        save,
        label: label.trim() || undefined,
      },
      { success: "Pomodoro iniciado!", error: "Falha ao iniciar." },
    );
    if (res.ok) await refresh();
  };

  const stop = async () => {
    const res = await run({ action: "pomodoro-stop" }, { success: "Sessão encerrada." });
    if (res.ok) await refresh();
    setConfirmStop(false);
  };

  return (
    <Screen>
      <ScreenHeader
        title="Pomodoro"
        subtitle="Sessões de foco com ciclos de trabalho e descanso sobre uma categoria."
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para usar o pomodoro.</p>
        </EmptyCard>
      ) : pomo ? (
        <Card className="ring-emerald-500/30">
          <CardContent className="flex flex-wrap items-center gap-4 px-5 py-4">
            <div className="grid size-12 shrink-0 place-items-center rounded-xl bg-muted text-emerald-500 ring-1 ring-emerald-500/30">
              <Timer className="size-6" />
            </div>
            <div className="min-w-0 flex-1">
              <h3 className="font-heading text-lg font-semibold">
                {pomo.phase === "rest" ? "Descanso" : "Foco"} — ciclo {pomo.cycle}/{pomo.cycles}
              </h3>
              <p className="text-sm text-muted-foreground">
                Preset {pomo.preset ?? "—"} · iniciado {formatClock(pomo.started_at ?? "")}
              </p>
            </div>
            <CircularTimer
              ms={phaseUntilMs}
              totalMs={phaseTotalMs}
              tone={pomo.phase === "rest" ? "rest" : "focus"}
              label={pomo.phase === "rest" ? "descanso" : "foco"}
              cycle={pomo.cycle}
              cycles={pomo.cycles}
            />
            <Button variant="destructive" onClick={() => setConfirmStop(true)}>
              <Square /> Encerrar sessão
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Card className="max-w-2xl">
          <CardContent className="flex flex-col gap-5 px-5 py-5">
            <div>
              <h3 className="font-heading text-base font-semibold">Nova sessão</h3>
              {defaults && (
                <p className="mt-1 text-sm text-muted-foreground">
                  Padrões salvos: {defaults.work}m trabalho / {defaults.rest}m descanso /{" "}
                  {defaults.cycles} ciclos
                </p>
              )}
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="flex flex-col gap-2">
                <Label>Categoria</Label>
                <Select value={preset} onValueChange={setPreset}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Escolha uma categoria" />
                  </SelectTrigger>
                  <SelectContent>
                    {presets.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="pomo-label">Missão (label, opcional)</Label>
                <Input
                  id="pomo-label"
                  type="text"
                  placeholder="ex: Estudar ENEM"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="pomo-work">Trabalho (min)</Label>
                <Input
                  id="pomo-work"
                  type="number"
                  min={1}
                  max={10080}
                  value={work}
                  onChange={(e) => setWork(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="pomo-rest">Descanso (min)</Label>
                <Input
                  id="pomo-rest"
                  type="number"
                  min={0}
                  max={240}
                  value={rest}
                  onChange={(e) => setRest(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="pomo-cycles">Ciclos</Label>
                <Input
                  id="pomo-cycles"
                  type="number"
                  min={1}
                  max={24}
                  value={cycles}
                  onChange={(e) => setCycles(e.target.value)}
                />
              </div>
            </div>

            <div className="flex flex-wrap gap-x-6 gap-y-3 text-sm">
              <label className="flex items-center gap-2">
                <Checkbox checked={strict} onCheckedChange={(c) => setStrict(c === true)} />
                <span>Modo estrito (não pode encerrar antecipadamente)</span>
              </label>
              <label className="flex items-center gap-2">
                <Checkbox checked={save} onCheckedChange={(c) => setSave(c === true)} />
                <span>Salvar como padrão para as próximas sessões</span>
              </label>
            </div>

            <Button onClick={() => void start()} disabled={busy} size="lg" className="w-full">
              <Play /> {busy ? "Iniciando…" : "Iniciar pomodoro"}
            </Button>
          </CardContent>
        </Card>
      )}

      <Dialog open={confirmStop} onOpenChange={setConfirmStop}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Encerrar sessão?</DialogTitle>
            <DialogDescription asChild>
              <p>
                A sessão atual será encerrada. Em modo estrito, o daemon recusa o encerramento até o
                fim do ciclo.
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmStop(false)} disabled={busy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={() => void stop()}
              disabled={busy}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {busy ? "Encerrando…" : "Encerrar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}
