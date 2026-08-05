import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { toast as sonnerToast } from "sonner";
import { api, authStatus, login as apiLogin, logout as apiLogout, pingDaemon } from "./api/client";
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
  | "rede"
  | "seguranca"
  | "config";

/** Sessão do browser: null = ainda verificando (splash), ou quem logou. */
export interface AuthState {
  authenticated: boolean;
  username: string;
  isAdmin: boolean;
}

interface AppState {
  /** null = ainda verificando; true/false = daemon acessível. */
  daemonUp: boolean | null;
  status: ApiResponse | null;
  presets: Preset[];
  stats: ApiResponse | null;
  /** null = checando sessão no boot; ver AuthState depois disso. */
  auth: AuthState | null;
  login: (
    username: string,
    password: string,
  ) => Promise<{ ok: boolean; message: string; username?: string; isAdmin?: boolean }>;
  logout: () => Promise<void>;
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
  const [auth, setAuth] = useState<AuthState | null>(null);

  // authRef espelha auth para os intervalos (que fecham sobre o valor inicial)
  // não baterem em /api/action enquanto o browser estiver deslogado (401).
  const authRef = useRef<AuthState | null>(null);
  const setAuthBoth = (a: AuthState | null) => {
    authRef.current = a;
    setAuth(a);
  };

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
    // Sem sessão, /api/action responde 401 — nada para buscar.
    if (authRef.current?.authenticated !== true) return;
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

  const login = async (username: string, password: string) => {
    const res = await apiLogin(username, password);
    if (res.ok) {
      setAuthBoth({
        authenticated: true,
        username: res.username ?? username,
        isAdmin: res.isAdmin ?? false,
      });
      void refresh();
    }
    return res;
  };

  const logout = async () => {
    await apiLogout();
    setAuthBoth(null);
    setStatus(null);
    setPresets([]);
    setStats(null);
  };

  // Boot: descobre se o browser já tem sessão (cookie fg_session) e, se tiver,
  // já carrega os dados. Enquanto isso, auth === null (splash no App.tsx).
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const st = await authStatus();
      if (cancelled) return;
      if (st.authenticated) {
        setAuthBoth({
          authenticated: true,
          username: st.username ?? "",
          isAdmin: st.is_admin ?? false,
        });
        void refresh();
      } else {
        setAuthBoth(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

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
      if (authRef.current?.authenticated !== true) return;
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
    <Ctx.Provider
      value={{ daemonUp, status, presets, stats, auth, login, logout, refresh, toast }}
    >
      {children}
    </Ctx.Provider>
  );
}
