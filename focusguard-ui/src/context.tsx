import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { toast as sonnerToast } from "sonner";
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

interface AppState {
  /** null = ainda verificando; true/false = daemon acessível. */
  daemonUp: boolean | null;
  status: ApiResponse | null;
  presets: Preset[];
  stats: ApiResponse | null;
  refresh: () => Promise<void>;
  /** toast renderiza via sonner (sucesso/erro), mesma assinatura de antes. */
  toast: (msg: string, kind?: "ok" | "err") => void;
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

  const toast = (msg: string, kind: "ok" | "err" = "ok") => {
    if (kind === "err") {
      sonnerToast.error(msg);
    } else {
      sonnerToast.success(msg);
    }
  };

  const refresh = async () => {
    // O indicador de conectividade é decidido SOMENTE pelo pingDaemon (probe
    // real de conectividade, 5s de orçamento). Uma ação lenta (status pode
    // enumerar o firewall, 15s de orçamento no httpapi) NÃO significa daemon
    // desligado: com um daemon saudável ela apenas omite o dado neste ciclo.
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
    <Ctx.Provider value={{ daemonUp, status, presets, stats, refresh, toast }}>
      {children}
    </Ctx.Provider>
  );
}
