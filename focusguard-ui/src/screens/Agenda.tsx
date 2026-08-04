import { useEffect, useMemo, useState } from "react";
import { CalendarPlus, Trash2, Upload } from "lucide-react";
import { api, execAction } from "@/api/client";
import type { ScheduleRule } from "@/api/types";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { EmptyCard, Screen, ScreenHeader, SectionTitle } from "@/components/screen";
import { WeeklyGrid } from "@/components/weekly-grid";
import { useApp } from "@/context";

const DAY_NAMES = ["dom", "seg", "ter", "qua", "qui", "sex", "sab"];

function daysLabel(days: number[]): string {
  return days
    .slice()
    .sort((a, b) => a - b)
    .map((d) => DAY_NAMES[d] ?? d)
    .join(", ");
}

function windowLabel(r: ScheduleRule): string {
  return r.windows && r.windows.length > 0 ? r.windows.join(", ") : `${r.start}-${r.end}`;
}

export function Agenda() {
  const { presets, toast, daemonUp } = useApp();

  const [schedules, setSchedules] = useState<ScheduleRule[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [toRemove, setToRemove] = useState<ScheduleRule | null>(null);

  // formulário de nova regra
  const [preset, setPreset] = useState("");
  const [days, setDays] = useState<number[]>([]);
  const [start, setStart] = useState("08:00");
  const [end, setEnd] = useState("12:00");
  const [windows, setWindows] = useState("");
  const [label, setLabel] = useState("");

  // importação .ics
  const [icsPreset, setIcsPreset] = useState("");
  const [icsFile, setIcsFile] = useState<File | null>(null);

  const load = () => {
    api
      .scheduleList()
      .then((r) => setSchedules(r.success ? (r.schedules ?? []) : []))
      .catch(() => setSchedules([]));
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    if (!preset && presets.length > 0) setPreset(presets[0].name);
    if (!icsPreset && presets.length > 0) setIcsPreset(presets[0].name);
  }, [presets, preset, icsPreset]);

  const toggleDay = (d: number) => {
    setDays((cur) => (cur.includes(d) ? cur.filter((x) => x !== d) : [...cur, d]));
  };

  const add = async () => {
    if (!preset || days.length === 0) {
      toast("Escolha a categoria e ao menos um dia.", "err");
      return;
    }
    setBusy(true);
    try {
      const rule: ScheduleRule = {
        id: "",
        preset,
        days: [...days].sort((a, b) => a - b),
        start: "",
        end: "",
        enabled: true,
        label: label.trim() || undefined,
      };
      if (windows.trim()) {
        rule.windows = windows
          .split(",")
          .map((w) => w.trim())
          .filter(Boolean);
      } else {
        rule.start = start;
        rule.end = end;
      }
      const res = await execAction({ action: "schedule-add", schedule_rule: rule });
      toast(res.message || (res.ok ? "Agendamento criado!" : "Falha ao criar."), res.ok ? "ok" : "err");
      if (res.ok) {
        load();
        setDays([]);
        setWindows("");
        setLabel("");
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao criar o agendamento.", "err");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!toRemove) return;
    setBusy(true);
    try {
      const res = await execAction({ action: "schedule-remove", schedule_id: toRemove.id });
      toast(res.message || (res.ok ? "Agendamento removido." : "Falha ao remover."), res.ok ? "ok" : "err");
      if (res.ok) load();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao remover.", "err");
    } finally {
      setBusy(false);
      setToRemove(null);
    }
  };

  const importIcs = async () => {
    if (!icsFile) {
      toast("Selecione um arquivo .ics.", "err");
      return;
    }
    setBusy(true);
    try {
      const text = await icsFile.text();
      const res = await execAction({ action: "schedule-import", ics_content: text, ics_preset: icsPreset });
      toast(res.message || (res.ok ? "Calendário importado!" : "Falha ao importar."), res.ok ? "ok" : "err");
      if (res.ok) {
        load();
        setIcsFile(null);
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao importar o calendário.", "err");
    } finally {
      setBusy(false);
    }
  };

  const rules = useMemo(() => schedules ?? [], [schedules]);

  return (
    <Screen>
      <ScreenHeader
        title="Agenda"
        subtitle="Bloqueios recorrentes por horário e importação de calendário."
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para gerenciar a agenda.</p>
        </EmptyCard>
      ) : (
        <>
          <Card className="max-w-2xl">
            <CardContent className="flex flex-col gap-5 px-5 py-5">
              <div>
                <h3 className="font-heading text-base font-semibold">Nova regra</h3>
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
                  <Label htmlFor="agenda-label">Rótulo (opcional)</Label>
                  <Input
                    id="agenda-label"
                    type="text"
                    placeholder="ex: Estudo matinal"
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                  />
                </div>
              </div>

              <div className="flex flex-col gap-2">
                <Label>Dias da semana</Label>
                <div className="flex flex-wrap gap-1.5">
                  {DAY_NAMES.map((name, i) => (
                    <Button
                      key={name}
                      type="button"
                      variant={days.includes(i) ? "default" : "outline"}
                      onClick={() => toggleDay(i)}
                      className="h-7"
                    >
                      {name}
                    </Button>
                  ))}
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <div className="flex flex-col gap-2 sm:col-span-3">
                  <Label htmlFor="agenda-windows">Janelas (ex: 08:00-12:00,14:00-18:00)</Label>
                  <Input
                    id="agenda-windows"
                    type="text"
                    placeholder="deixe vazio para usar início/fim"
                    value={windows}
                    onChange={(e) => setWindows(e.target.value)}
                  />
                </div>
                {!windows.trim() && (
                  <>
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="agenda-start">Início</Label>
                      <Input
                        id="agenda-start"
                        type="time"
                        value={start}
                        onChange={(e) => setStart(e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="agenda-end">Fim</Label>
                      <Input
                        id="agenda-end"
                        type="time"
                        value={end}
                        onChange={(e) => setEnd(e.target.value)}
                      />
                    </div>
                  </>
                )}
              </div>

              <Button onClick={() => void add()} disabled={busy} size="lg" className="w-full">
                <CalendarPlus /> {busy ? "Criando…" : "Criar agendamento"}
              </Button>
            </CardContent>
          </Card>

          <Card className="max-w-2xl">
            <CardContent className="flex flex-col gap-5 px-5 py-5">
              <div>
                <h3 className="font-heading text-base font-semibold">Importar calendário (.ics)</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  Eventos semanais do arquivo viram regras recorrentes da categoria escolhida.
                </p>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label>Categoria</Label>
                  <Select value={icsPreset} onValueChange={setIcsPreset}>
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
                  <Label htmlFor="agenda-ics">Arquivo</Label>
                  <Input
                    id="agenda-ics"
                    type="file"
                    accept=".ics,text/calendar"
                    onChange={(e) => setIcsFile(e.target.files?.[0] ?? null)}
                  />
                </div>
              </div>

              <div>
                <Button variant="outline" onClick={() => void importIcs()} disabled={busy || !icsFile}>
                  <Upload /> {busy ? "Importando…" : "Importar"}
                </Button>
              </div>
            </CardContent>
          </Card>

          {rules.length > 0 && (
            <>
              <SectionTitle>Grade semanal</SectionTitle>
              <WeeklyGrid rules={rules} />
            </>
          )}

          <SectionTitle count={rules.length}>Agendamentos</SectionTitle>
          {rules.length === 0 ? (
            <EmptyCard>
              <p>Nenhum agendamento recorrente configurado.</p>
            </EmptyCard>
          ) : (
            <div className="flex flex-col gap-3">
              {rules.map((r) => (
                <Card key={r.id} size="sm">
                  <CardContent className="flex flex-col gap-2 px-4 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <span className="text-sm font-semibold">
                        {r.label || r.preset}
                        {!r.enabled && <Badge className="ml-2">desativada</Badge>}
                      </span>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="Remover agendamento"
                            onClick={() => setToRemove(r)}
                          >
                            <Trash2 />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Remover agendamento</TooltipContent>
                      </Tooltip>
                    </div>
                    <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                      <span>{daysLabel(r.days)}</span>
                      <span className="font-mono">{windowLabel(r)}</span>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </>
      )}

      <Dialog open={toRemove !== null} onOpenChange={(o) => !o && setToRemove(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remover agendamento?</DialogTitle>
            <DialogDescription asChild>
              <p>
                {toRemove?.label || toRemove?.preset} — {toRemove ? daysLabel(toRemove.days) : ""}{" "}
                {toRemove ? windowLabel(toRemove) : ""}
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setToRemove(null)} disabled={busy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={() => void remove()}
              disabled={busy}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {busy ? "Removendo…" : "Remover"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}
