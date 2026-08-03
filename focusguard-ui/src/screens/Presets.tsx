import { useState } from "react";
import { execAction } from "../api/client";
import type { Preset } from "../api/types";
import { Button, Card, Field, Modal } from "../components/ui";
import { useApp } from "../context";

export function Presets() {
  const { presets, refresh, toast, daemonUp } = useApp();

  const [name, setName] = useState("");
  const [label, setLabel] = useState("");
  const [domains, setDomains] = useState("");
  const [busy, setBusy] = useState(false);
  const [toRemove, setToRemove] = useState<Preset | null>(null);

  const add = async () => {
    const n = name.trim().toLowerCase().replace(/\s+/g, "-");
    const doms = domains
      .split(",")
      .map((d) => d.trim().toLowerCase())
      .filter(Boolean);
    if (!n || doms.length === 0) {
      toast("Informe o nome e ao menos um domínio.", "err");
      return;
    }
    setBusy(true);
    try {
      const res = await execAction({
        action: "preset-add",
        preset_name: n,
        preset_label: label.trim() || n,
        preset_domains: doms,
      });
      toast(res.message || (res.ok ? "Preset criado!" : "Falha ao criar."), res.ok ? "ok" : "err");
      if (res.ok) {
        await refresh();
        setName("");
        setLabel("");
        setDomains("");
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao criar o preset.", "err");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!toRemove) return;
    setBusy(true);
    try {
      const res = await execAction({ action: "preset-remove", preset_name: toRemove.name });
      toast(
        res.message ||
          (res.ok ? "Preset removido." : "Presets embutidos não podem ser removidos."),
        res.ok ? "ok" : "err",
      );
      if (res.ok) await refresh();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao remover.", "err");
    } finally {
      setBusy(false);
      setToRemove(null);
    }
  };

  if (daemonUp === false) {
    return (
      <section className="screen">
        <h2>Presets</h2>
        <Card className="empty-card">
          <p>O daemon está desligado — inicie o serviço para gerenciar os presets.</p>
        </Card>
      </section>
    );
  }

  return (
    <section className="screen">
      <header className="screen-head">
        <h2>Presets</h2>
        <p className="muted">Categorias de domínios para bloqueio e pomodoro.</p>
      </header>

      <Card>
        <h3>Criar preset personalizado</h3>
        <div className="form-grid">
          <Field label="Nome">
            <input
              type="text"
              className="input"
              placeholder="ex: estudos"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="Rótulo (opcional)">
            <input
              type="text"
              className="input"
              placeholder="ex: Estudos profundos"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
          </Field>
        </div>
        <Field label="Domínios (separados por vírgula)">
          <input
            type="text"
            className="input"
            placeholder="ex: reddit.com, news.ycombinator.com"
            value={domains}
            onChange={(e) => setDomains(e.target.value)}
          />
        </Field>
        <div className="card-actions">
          <Button variant="primary" onClick={() => void add()} disabled={busy}>
            {busy ? "Criando…" : "＋ Criar preset"}
          </Button>
        </div>
      </Card>

      <div className="section-title">
        <h3>Catálogo</h3>
        <span className="muted">{presets.length}</span>
      </div>
      <div className="preset-grid">
        {presets.map((p) => (
          <Card key={p.name} className="preset-card">
            <div className="preset-head">
              <div>
                <h4>{p.label}</h4>
                <code className="muted">{p.name}</code>
              </div>
              <Button variant="ghost" title="Remover preset" onClick={() => setToRemove(p)}>
                ✕
              </Button>
            </div>
            <div className="chips">
              {p.domains.map((d) => (
                <span key={d} className="chip static">
                  {d}
                </span>
              ))}
            </div>
          </Card>
        ))}
      </div>

      {toRemove && (
        <Modal
          title="Remover preset?"
          danger
          onCancel={() => setToRemove(null)}
          onConfirm={() => void remove()}
          confirmLabel="Remover"
          busy={busy}
        >
          <p>
            <strong>{toRemove.label}</strong> ({toRemove.name}). Presets embutidos do sistema são
            recusados pelo daemon.
          </p>
        </Modal>
      )}
    </section>
  );
}
