import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { api, pingDaemon } from "./api/client";
import type { ApiResponse, Preset } from "./api/types";

export type Screen =
  | "dashboard"
  | "bloquear"
  | "panico"
  | "pomodoro"
  | "agenda"
  | "apps"
  | "presets"
  | "stats"
  | "seguranca"
  | "config";

export interface Toast {
  id: number;
  msg: string;
  kind: "ok" | "err";
}

interface AppState {
  /** null = ainda verificando; true/false = daemon acessível. */
  daemonUp: boolean | null;
  status: ApiResponse | null;
  presets: Preset[];
  stats: ApiResponse | null;
  refresh: () => Promise<void>;
  toast: (msg: string, kind?: Toast["kind"]) => void;
  toasts: Toast[];
}

const Ctx = createContext<AppState | null>(null);

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp fora do AppProvider");
  return v;
}

export function AppProvider({ children }: { children: ReactNode }) {
  const [daemonUp, setDaemonUp] = useState<boolean | null>(null);
  const [status, setStatus] = useState<ApiResponse | null>(null);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [stats, setStats] = useState<ApiResponse | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);

  const toast = (msg: string, kind: Toast["kind"] = "ok") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg, kind }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500);
  };

  const refresh = async () => {
    const up = await pingDaemon();
    setDaemonUp(up);
    if (!up) {
      setStatus(null);
      return;
    }
    try {
      const st = await api.status();
      if (st.success) setStatus(st);
    } catch {
      setDaemonUp(false);
      setStatus(null);
    }
    try {
      const pr = await api.presets();
      if (pr.success) setPresets(pr.presets ?? []);
    } catch {
      /* presets são opcionais */
    }
  };

  // Status em polling leve (10s). O countdown é client-side, então não precisa
  // de polling frequente — o WebSocket (fase futura) elimina o polling.
  useEffect(() => {
    void refresh();
    const id = setInterval(() => void refresh(), 10_000);
    return () => clearInterval(id);
  }, []);

  // Stats em polling mais espaçado (60s): leitura de JSONL, custo baixo.
  useEffect(() => {
    const load = () => {
      api
        .stats()
        .then((st) => {
          if (st.success) setStats(st);
        })
        .catch(() => {});
    };
    load();
    const id = setInterval(load, 60_000);
    return () => clearInterval(id);
  }, []);

  return (
    <Ctx.Provider value={{ daemonUp, status, presets, stats, refresh, toast, toasts }}>
      {children}
    </Ctx.Provider>
  );
}
