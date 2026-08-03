import { useState } from "react";
import { Agenda } from "./screens/Agenda";
import { Apps } from "./screens/Apps";
import { Bloquear } from "./screens/Bloquear";
import { Configuracoes } from "./screens/Configuracoes";
import { Dashboard } from "./screens/Dashboard";
import { Estatisticas } from "./screens/Estatisticas";
import { Panico } from "./screens/Panico";
import { Pomodoro } from "./screens/Pomodoro";
import { Presets } from "./screens/Presets";
import { Seguranca } from "./screens/Seguranca";
import { AppProvider, useApp, type Screen } from "./context";

const NAV: { id: Screen; label: string; icon: string }[] = [
  { id: "dashboard", label: "Painel", icon: "🛡️" },
  { id: "bloquear", label: "Bloquear", icon: "🔒" },
  { id: "panico", label: "Modo pânico", icon: "🚨" },
  { id: "pomodoro", label: "Pomodoro", icon: "🍅" },
  { id: "agenda", label: "Agenda", icon: "📅" },
  { id: "apps", label: "Apps", icon: "🛑" },
  { id: "presets", label: "Presets", icon: "🗂️" },
  { id: "stats", label: "Estatísticas", icon: "📊" },
  { id: "seguranca", label: "Segurança", icon: "🛠️" },
  { id: "config", label: "Configurações", icon: "⚙️" },
];

export function App() {
  return (
    <AppProvider>
      <Shell />
    </AppProvider>
  );
}

function Shell() {
  const [screen, setScreen] = useState<Screen>("dashboard");
  const { daemonUp, toasts, status } = useApp();

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-logo" aria-hidden="true">
            <svg viewBox="0 0 32 32" width="30" height="30">
              <path d="M16 2 28 7v9c0 7.2-4.9 12.2-12 14C8.9 28.2 4 23.2 4 16V7z" fill="#1d4ed8" />
              <path
                d="M10.5 16.2l3.8 3.8 7.2-8"
                fill="none"
                stroke="#22c55e"
                strokeWidth="3"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </span>
          <div className="brand-text">
            <h1>FocusGuard</h1>
            <p>Bloqueio de distrações</p>
          </div>
        </div>

        <nav className="nav" aria-label="Navegação principal">
          {NAV.map((n) => (
            <button
              key={n.id}
              type="button"
              className={`nav-item${screen === n.id ? " active" : ""}`}
              onClick={() => setScreen(n.id)}
            >
              <span className="nav-icon" aria-hidden="true">
                {n.icon}
              </span>
              {n.label}
            </button>
          ))}
        </nav>

        <footer className="sidebar-footer">
          {status?.current_version ? (
            <span className="version-chip">v{status.current_version}</span>
          ) : (
            <span className="version-chip muted">—</span>
          )}
          <span className={`dot${daemonUp ? " ok" : daemonUp === null ? "" : " down"}`} aria-hidden="true" />
          {daemonUp ? "daemon ativo" : daemonUp === null ? "verificando…" : "daemon offline"}
        </footer>
      </aside>

      <main className="content">
        {daemonUp === false && (
          <div className="daemon-banner" role="alert">
            ⚠️ O daemon FocusGuard está desligado. As ações ficam indisponíveis
            até ele iniciar (execute <code>focusguard install</code> ou inicie o serviço).
          </div>
        )}
        {screen === "dashboard" && <Dashboard onNavigate={setScreen} />}
        {screen === "bloquear" && <Bloquear />}
        {screen === "panico" && <Panico />}
        {screen === "pomodoro" && <Pomodoro />}
        {screen === "agenda" && <Agenda />}
        {screen === "apps" && <Apps />}
        {screen === "presets" && <Presets />}
        {screen === "stats" && <Estatisticas />}
        {screen === "seguranca" && <Seguranca />}
        {screen === "config" && <Configuracoes />}
      </main>

      <div className="toasts" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind}`}>
            <span aria-hidden="true">{t.kind === "ok" ? "✅" : "❌"}</span>
            {t.msg}
          </div>
        ))}
      </div>
    </div>
  );
}
