import { useState } from "react";
import { api, DaemonError } from "../api/client";
import { Button, Card, Chip, Field } from "../components/ui";
import { useApp } from "../context";

const DURATIONS = [
  { label: "30 min", value: "30m" },
  { label: "1 h", value: "1h" },
  { label: "2 h", value: "2h" },
  { label: "4 h", value: "4h" },
  { label: "8 h", value: "8h" },
];

export function Bloquear() {
  const { presets, toast, daemonUp, refresh } = useApp();
  const [mode, setMode] = useState<"preset" | "domain">("preset");
  const [preset, setPreset] = useState<string>("");
  const [domain, setDomain] = useState<string>("");
  const [duration, setDuration] = useState<string>("1h");
  const [customMin, setCustomMin] = useState<string>("");
  const [busy, setBusy] = useState(false);

  const effectiveDuration =
    duration === "custom" && customMin ? `${customMin}m` : duration;

  const submit = async () => {
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
    setBusy(true);
    try {
      const resp = await api.block({
        preset: mode === "preset" ? preset : undefined,
        domain: mode === "domain" ? domain.trim() : undefined,
        duration: effectiveDuration,
      });
      if (!resp.success) {
        toast(resp.message ?? "Falha ao bloquear.", "err");
      } else {
        toast(resp.message ?? "Bloqueio aplicado!");
        setDomain("");
        await refresh();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Erro ao bloquear.", "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Bloquear</h2>
        <p className="muted">Escolha uma categoria ou um domínio e a duração do bloqueio.</p>
      </header>

      <Card className="form-card">
        <div className="segmented">
          <button
            type="button"
            className={`segment${mode === "preset" ? " active" : ""}`}
            onClick={() => setMode("preset")}
          >
            Categoria (preset)
          </button>
          <button
            type="button"
            className={`segment${mode === "domain" ? " active" : ""}`}
            onClick={() => setMode("domain")}
          >
            Domínio específico
          </button>
        </div>

        {mode === "preset" ? (
          <div className="chips">
            {presets.map((p) => (
              <Chip
                key={p.name}
                selected={preset === p.name}
                onClick={() => setPreset(p.name)}
                title={p.domains.join(", ")}
              >
                <span className="chip-name">{p.label}</span>
                <span className="chip-count">{p.domains.length}</span>
              </Chip>
            ))}
          </div>
        ) : (
          <Field label="Domínio">
            <input
              type="text"
              className="input"
              placeholder="ex: youtube.com"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void submit()}
            />
          </Field>
        )}

        <Field label="Duração">
          <div className="chips">
            {DURATIONS.map((d) => (
              <Chip
                key={d.value}
                selected={duration === d.value}
                onClick={() => setDuration(d.value)}
              >
                {d.label}
              </Chip>
            ))}
            <Chip selected={duration === "custom"} onClick={() => setDuration("custom")}>
              personalizado
            </Chip>
          </div>
          {duration === "custom" && (
            <div className="inline-field">
              <input
                type="number"
                className="input"
                min={1}
                placeholder="minutos"
                value={customMin}
                onChange={(e) => setCustomMin(e.target.value)}
              />
              <span className="muted">minutos</span>
            </div>
          )}
        </Field>

        <Button className="submit" onClick={submit} disabled={busy || daemonUp === false}>
          {busy ? "Bloqueando…" : "🔒 Bloquear"}
        </Button>
        {daemonUp === false && <p className="muted">Daemon desligado — bloqueio indisponível.</p>}
      </Card>
    </section>
  );
}
