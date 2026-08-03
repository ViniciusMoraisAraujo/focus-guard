import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { TamperEvent } from "../api/types";
import { Button, Card, Spinner } from "../components/ui";
import { useApp } from "../context";

function fmtDate(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return d.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function Seguranca() {
  const { daemonUp } = useApp();
  const [events, setEvents] = useState<TamperEvent[] | null>(null);

  const load = () => {
    api
      .tamperLog()
      .then((r) => setEvents(r.success ? (r.tamper_log ?? []) : []))
      .catch(() => setEvents([]));
  };

  useEffect(() => {
    load();
  }, []);

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Segurança</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para ver o histórico.</p>
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Segurança</h2>
        <div className="quick-actions">
          <Button variant="secondary" onClick={load}>
            ↻ Recarregar
          </Button>
        </div>
      </header>
      <p className="muted">
        Tentativas de adulteração dos arquivos de bloqueio (hosts/estado) detectadas e revertidas
        pelo daemon.
      </p>

      {events === null ? (
        <Card className="empty-card">
          <Spinner label="Carregando histórico…" />
        </Card>
      ) : events.length === 0 ? (
        <Card className="empty-card">
          <p>Nenhuma tentativa registrada. 👌</p>
          <p className="muted">O FocusGuard restaura automaticamente qualquer alteração externa.</p>
        </Card>
      ) : (
        <div className="tamper-list">
          {events.map((e, i) => (
            <Card key={`${e.at}-${i}`} className="tamper-card">
              <div className="tamper-head">
                <span className="badge badge-red">
                  {e.source === "hosts" ? "hosts" : "estado"}
                </span>
                <span className="badge">{e.action}</span>
                <span className="muted">{fmtDate(e.at)}</span>
              </div>
              {e.detail && <p className="tamper-detail">{e.detail}</p>}
            </Card>
          ))}
        </div>
      )}
    </section>
  );
}
