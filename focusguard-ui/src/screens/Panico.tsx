import { useState } from "react";
import { api, DaemonError } from "../api/client";
import { Card, Chip, Field, Modal } from "../components/ui";
import { useApp } from "../context";

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
    <section className="screen">
      <header className="screen-head">
        <h2>Modo pânico</h2>
        <p className="muted">Bloqueia TODA a internet por um período. Use em momentos de decisão. 😤</p>
      </header>

      <Card className={`panic-card${panicActive ? " active" : ""}`}>
        <button
          type="button"
          className="panic-button"
          disabled={busy || daemonUp === false}
          onClick={() => setConfirmOpen(true)}
        >
          <span className="panic-icon" aria-hidden="true">
            🚨
          </span>
          <span className="panic-label">
            {panicActive ? "Pânico em andamento" : "Bloquear toda a internet"}
          </span>
        </button>

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
          </div>
        </Field>

        <Field label="Allowlist (opcional — o que continua acessível)">
          <textarea
            className="input textarea"
            placeholder="docs.google.com, github.com"
            value={allow}
            onChange={(e) => setAllow(e.target.value)}
          />
          <p className="muted hint">
            Separe por vírgula. Vazio = bloquear toda a internet (sem exceções).
          </p>
        </Field>
        {daemonUp === false && <p className="muted">Daemon desligado — bloqueio indisponível.</p>}
      </Card>

      {confirmOpen && (
        <Modal
          title="Confirmar modo pânico"
          danger
          busy={busy}
          confirmLabel="Bloquear internet"
          onConfirm={run}
          onCancel={() => setConfirmOpen(false)}
        >
          <p>
            Toda a internet ficará bloqueada por{" "}
            <strong>{duration}</strong>
            {allow.trim() ? " — exceto os domínios permitidos." : ", sem exceções."}
          </p>
          <p className="muted">Bloqueios temporários não podem ser desfeitos manualmente.</p>
        </Modal>
      )}
    </section>
  );
}
