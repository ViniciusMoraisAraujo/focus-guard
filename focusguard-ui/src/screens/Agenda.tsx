import { useEffect, useMemo, useState } from "react";
import { api, execAction } from "../api/client";
import type { ScheduleRule } from "../api/types";
import { Button, Card, Field, Modal } from "../components/ui";
import { useApp } from "../context";

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

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Agenda</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para gerenciar a agenda.</p>
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Agenda</h2>
        <p className="muted">Bloqueios recorrentes por horário e importação de calendário.</p>
      </header>

      <Card>
        <h3>Nova regra</h3>
        <div className="form-grid">
          <Field label="Categoria">
            <select className="input" value={preset} onChange={(e) => setPreset(e.target.value)}>
              {presets.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Rótulo (opcional)">
            <input
              type="text"
              className="input"
              placeholder="ex: Estudo matinal"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
          </Field>
        </div>
        <div className="field">
          <span className="field-label">Dias da semana</span>
          <div className="day-picker">
            {DAY_NAMES.map((name, i) => (
              <button
                key={name}
                type="button"
                className={`chip${days.includes(i) ? " selected" : ""}`}
                onClick={() => toggleDay(i)}
              >
                {name}
              </button>
            ))}
          </div>
        </div>
        <div className="form-grid">
          <Field label="Janelas (ex: 08:00-12:00,14:00-18:00)">
            <input
              type="text"
              className="input"
              placeholder="deixe vazio para usar início/fim"
              value={windows}
              onChange={(e) => setWindows(e.target.value)}
            />
          </Field>
          {!windows.trim() && (
            <>
              <Field label="Início">
                <input type="time" className="input" value={start} onChange={(e) => setStart(e.target.value)} />
              </Field>
              <Field label="Fim">
                <input type="time" className="input" value={end} onChange={(e) => setEnd(e.target.value)} />
              </Field>
            </>
          )}
        </div>
        <div className="card-actions">
          <Button variant="primary" onClick={() => void add()} disabled={busy}>
            {busy ? "Criando…" : "＋ Criar agendamento"}
          </Button>
        </div>
      </Card>

      <Card>
        <h3>Importar calendário (.ics)</h3>
        <p className="muted">
          Eventos semanais do arquivo viram regras recorrentes da categoria escolhida.
        </p>
        <div className="form-grid">
          <Field label="Categoria">
            <select className="input" value={icsPreset} onChange={(e) => setIcsPreset(e.target.value)}>
              {presets.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Arquivo">
            <input
              type="file"
              className="input file-input"
              accept=".ics,text/calendar"
              onChange={(e) => setIcsFile(e.target.files?.[0] ?? null)}
            />
          </Field>
        </div>
        <div className="card-actions">
          <Button variant="secondary" onClick={() => void importIcs()} disabled={busy || !icsFile}>
            {busy ? "Importando…" : "⬆ Importar"}
          </Button>
        </div>
      </Card>

      <div className="section-title">
        <h3>Agendamentos</h3>
        <span className="muted">{rules.length}</span>
      </div>
      {rules.length === 0 ? (
        <Card className="empty-card">
          <p>Nenhum agendamento recorrente configurado.</p>
        </Card>
      ) : (
        <div className="rule-list">
          {rules.map((r) => (
            <Card key={r.id} className="rule-card">
              <div className="rule-head">
                <span className="rule-title">
                  {r.label || r.preset}
                  {!r.enabled && <span className="badge">desativada</span>}
                </span>
                <Button variant="ghost" onClick={() => setToRemove(r)}>
                  ✕
                </Button>
              </div>
              <div className="rule-meta muted">
                <span>{daysLabel(r.days)}</span>
                <span>{windowLabel(r)}</span>
              </div>
            </Card>
          ))}
        </div>
      )}

      {toRemove && (
        <Modal
          title="Remover agendamento?"
          danger
          onCancel={() => setToRemove(null)}
          onConfirm={() => void remove()}
          confirmLabel="Remover"
          busy={busy}
        >
          <p>
            {toRemove.label || toRemove.preset} — {daysLabel(toRemove.days)} {windowLabel(toRemove)}
          </p>
        </Modal>
      )}
    </section>
  );
}
