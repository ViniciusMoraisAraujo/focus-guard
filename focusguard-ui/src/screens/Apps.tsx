import { useEffect, useState } from "react";
import { api, execAction } from "../api/client";
import { Button, Card } from "../components/ui";
import { useApp } from "../context";

export function Apps() {
  const { daemonUp, toast } = useApp();
  const [apps, setApps] = useState<string[] | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    api
      .appsList()
      .then((r) => setApps(r.success ? (r.apps ?? []) : []))
      .catch(() => setApps([]));
  };

  useEffect(() => {
    load();
  }, []);

  const add = async () => {
    const proc = name.trim();
    if (!proc) {
      toast("Informe o nome do processo (ex: spotify.exe).", "err");
      return;
    }
    setBusy(true);
    try {
      const res = await execAction({ action: "apps-add", app_name: proc });
      toast(res.message || (res.ok ? "Processo adicionado!" : "Falha ao adicionar."), res.ok ? "ok" : "err");
      if (res.ok) {
        load();
        setName("");
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao adicionar.", "err");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (proc: string) => {
    setBusy(true);
    try {
      const res = await execAction({ action: "apps-remove", app_name: proc });
      toast(res.message || (res.ok ? "Processo removido." : "Falha ao remover."), res.ok ? "ok" : "err");
      if (res.ok) load();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao remover.", "err");
    } finally {
      setBusy(false);
    }
  };

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Apps</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para gerenciar os processos.</p>
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Apps (denylist)</h2>
        <p className="muted">
          Processos encerrados enquanto uma sessão de foco estiver ativa.
        </p>
      </header>

      <Card>
        <h3>Adicionar processo</h3>
        <div className="inline-field">
          <input
            type="text"
            className="input"
            placeholder="ex: spotify.exe"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void add()}
          />
          <Button variant="primary" onClick={() => void add()} disabled={busy}>
            {busy ? "Adicionando…" : "＋ Adicionar"}
          </Button>
        </div>
      </Card>

      <div className="section-title">
        <h3>Processos da denylist</h3>
        <span className="muted">{apps?.length ?? 0}</span>
      </div>
      {apps === null ? (
        <Card className="empty-card">
          <p className="muted">Carregando…</p>
        </Card>
      ) : apps.length === 0 ? (
        <Card className="empty-card">
          <p>Nenhum processo na denylist — o guard está inativo.</p>
        </Card>
      ) : (
        <div className="rule-list">
          {apps.map((a) => (
            <Card key={a} className="rule-card">
              <div className="rule-head">
                <span className="rule-title">
                  <code>{a}</code>
                </span>
                <Button variant="ghost" onClick={() => void remove(a)}>
                  ✕
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}
    </section>
  );
}
