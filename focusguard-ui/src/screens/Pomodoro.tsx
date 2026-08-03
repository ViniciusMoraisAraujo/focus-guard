import { useEffect, useState } from "react";
import { api, execAction } from "../api/client";
import { Button, Card, Field, Modal } from "../components/ui";
import { useApp } from "../context";
import { formatClock, formatMs, useCountdown } from "../hooks/useCountdown";

export function Pomodoro() {
  const { status, presets, refresh, toast, daemonUp } = useApp();

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
  const [busy, setBusy] = useState(false);
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

  const start = async () => {
    if (!preset) {
      toast("Escolha uma categoria (preset).", "err");
      return;
    }
    setBusy(true);
    try {
      const res = await execAction({
        action: "pomodoro",
        preset,
        work_min: Number(work) || undefined,
        rest_min: Number(rest) || undefined,
        cycles: Number(cycles) || undefined,
        strict,
        save,
        label: label.trim() || undefined,
      });
      toast(res.message || (res.ok ? "Pomodoro iniciado!" : "Falha ao iniciar."), res.ok ? "ok" : "err");
      if (res.ok) await refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao iniciar o pomodoro.", "err");
    } finally {
      setBusy(false);
    }
  };

  const stop = async () => {
    setBusy(true);
    try {
      const res = await execAction({ action: "pomodoro-stop" });
      toast(res.message || "Sessão encerrada.", res.ok ? "ok" : "err");
      if (res.ok) await refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao encerrar a sessão.", "err");
    } finally {
      setBusy(false);
      setConfirmStop(false);
    }
  };

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Pomodoro</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para usar o pomodoro.</p>
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Pomodoro</h2>
        <p className="muted">
          Sessões de foco com ciclos de trabalho e descanso sobre uma categoria.
        </p>
      </header>

      {pomo ? (
        <Card className={`hero-card status-focus`}>
          <div className="hero-main">
            <span className="hero-pill" aria-hidden="true">
              🍅
            </span>
            <div>
              <h3>
                {pomo.phase === "rest" ? "Descanso" : "Foco"} — ciclo {pomo.cycle}/
                {pomo.cycles}
              </h3>
              <p className="muted">
                Preset {pomo.preset ?? "—"} · iniciado {formatClock(pomo.started_at ?? "")}
              </p>
            </div>
          </div>
          <div className="hero-countdown">
            <span className="countdown-label">
              {pomo.phase === "rest" ? "Fim do descanso" : "Fim do ciclo"} em
            </span>
            <span className="countdown">{formatMs(phaseUntilMs)}</span>
          </div>
          <div className="hero-actions">
            <Button variant="danger" onClick={() => setConfirmStop(true)}>
              ■ Encerrar sessão
            </Button>
          </div>
        </Card>
      ) : (
        <Card>
          <h3>Nova sessão</h3>
          {defaults && (
            <p className="muted">
              Padrões salvos: {defaults.work}m trabalho / {defaults.rest}m descanso /{" "}
              {defaults.cycles} ciclos
            </p>
          )}
          <div className="form-grid">
            <Field label="Categoria">
              <select
                className="input"
                value={preset}
                onChange={(e) => setPreset(e.target.value)}
              >
                {presets.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.label}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Trabalho (min)">
              <input
                type="number"
                className="input"
                min={1}
                max={10080}
                value={work}
                onChange={(e) => setWork(e.target.value)}
              />
            </Field>
            <Field label="Descanso (min)">
              <input
                type="number"
                className="input"
                min={0}
                max={240}
                value={rest}
                onChange={(e) => setRest(e.target.value)}
              />
            </Field>
            <Field label="Ciclos">
              <input
                type="number"
                className="input"
                min={1}
                max={24}
                value={cycles}
                onChange={(e) => setCycles(e.target.value)}
              />
            </Field>
            <Field label="Missão (label, opcional)">
              <input
                type="text"
                className="input"
                placeholder='ex: Estudar ENEM'
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
            </Field>
          </div>
          <div className="check-row">
            <label className="check">
              <input type="checkbox" checked={strict} onChange={(e) => setStrict(e.target.checked)} />
              <span>Modo estrito (não pode encerrar antecipadamente)</span>
            </label>
            <label className="check">
              <input type="checkbox" checked={save} onChange={(e) => setSave(e.target.checked)} />
              <span>Salvar como padrão para as próximas sessões</span>
            </label>
          </div>
          <div className="card-actions">
            <Button variant="primary" onClick={() => void start()} disabled={busy}>
              {busy ? "Iniciando…" : "▶ Iniciar pomodoro"}
            </Button>
          </div>
        </Card>
      )}

      {confirmStop && (
        <Modal
          title="Encerrar sessão?"
          danger
          onCancel={() => setConfirmStop(false)}
          onConfirm={() => void stop()}
          confirmLabel="Encerrar"
          busy={busy}
        >
          <p>A sessão atual será encerrada. Em modo estrito, o daemon recusa o
          encerramento até o fim do ciclo.</p>
        </Modal>
      )}
    </section>
  );
}
