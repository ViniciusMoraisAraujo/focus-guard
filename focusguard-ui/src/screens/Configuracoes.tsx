import { useState } from "react";
import { api, DaemonError } from "../api/client";
import { Button, Card, Chip } from "../components/ui";
import { useApp } from "../context";
import { formatMinutes } from "../hooks/useCountdown";

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

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Configurações</h2>
        <p className="muted">Meta diária, atualizações e estado do sistema.</p>
      </header>

      <Card>
        <h3>🎯 Meta diária de foco</h3>
        <p className="muted">
          {goalNs > 0 ? `Atual: ${formatMinutes(goalNs)} por dia` : "Nenhuma meta definida ainda."}
        </p>
        <div className="chips">
          {GOALS.map((g) => (
            <Chip
              key={g.minutes}
              selected={g.minutes === goalMin}
              onClick={() => void setGoal(g.minutes)}
            >
              {g.label}
            </Chip>
          ))}
        </div>
        <div className="inline-field">
          <input
            type="number"
            className="input"
            min={1}
            max={1440}
            placeholder="minutos personalizados"
            value={customGoal}
            onChange={(e) => setCustomGoal(e.target.value)}
          />
          <Button variant="secondary" onClick={saveCustom} disabled={busy}>
            Definir
          </Button>
        </div>
      </Card>

      <Card>
        <h3>🔄 Atualizações</h3>
        {status?.update_available ? (
          <p className="update-available">
            Nova versão <strong>{status.update_version}</strong> disponível (atual:{" "}
            {status.current_version}). Aplique no terminal com{" "}
            <code>focusguard update</code>.
          </p>
        ) : (
          <p className="muted">
            Você está na versão mais recente
            {status?.current_version ? ` (${status.current_version})` : ""}.
          </p>
        )}
      </Card>

      <Card>
        <h3>🛡️ Proteção do sistema</h3>
        {status?.protection_error ? (
          <p className="muted">Não foi possível consultar o firewall: {status.protection_error}</p>
        ) : (
          <ul className="info-list">
            <li>
              <span>Regras de firewall (FocusGuard)</span>
              <strong>{status?.firewall_rules ?? 0}</strong>
            </li>
            <li>
              <span>Proteção DoH/DoT</span>
              <strong className={status?.doh_active ? "ok-text" : "warn-text"}>
                {status?.doh_active ? "ATIVA" : "inativa"}
              </strong>
            </li>
          </ul>
        )}
      </Card>

      <Card>
        <h3>ℹ️ Sobre</h3>
        <ul className="info-list">
          <li>
            <span>Interface web</span>
            <strong>FocusGuard UI</strong>
          </li>
          <li>
            <span>Versão do sistema</span>
            <strong>{status?.current_version ?? "—"}</strong>
          </li>
          <li>
            <span>Daemon</span>
            <strong className={daemonUp ? "ok-text" : "warn-text"}>
              {daemonUp ? "ativo" : "offline"}
            </strong>
          </li>
        </ul>
        <p className="muted hint">
          A interface web fica em <code>http://127.0.0.1:48902</code> e conversa com o
          daemon apenas via localhost.
        </p>
      </Card>
    </section>
  );
}
