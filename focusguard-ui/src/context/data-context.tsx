import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, pingDaemon } from "@/api/client";
import type { ApiResponse, Preset } from "@/api/types";
import { useAuth } from "./auth-context";

interface DataContextValue {
  /** null = ainda verificando; true/false = daemon acessível. */
  daemonUp: boolean | null;
  status: ApiResponse | null;
  presets: Preset[];
  stats: ApiResponse | null;
  refresh: () => Promise<void>;
}

const Ctx = createContext<DataContextValue | null>(null);

export function useData(): DataContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useData fora do DataProvider");
  return v;
}

/**
 * DataProvider é dono dos DADOS do daemon (conectividade + status + presets +
 * stats + polling). Fica DENTRO do AuthProvider: o estado de autenticação
 * decide quando buscar dados, sem que este provider se preocupe com
 * login/logout (SRP — F1 do plano de refatoração).
 */
export function DataProvider({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  const [daemonUp, setDaemonUp] = useState<boolean | null>(null);
  const [status, setStatus] = useState<ApiResponse | null>(null);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [stats, setStats] = useState<ApiResponse | null>(null);

  const authenticated = auth?.authenticated === true;
  // Espelho para o refresh() (estável via useCallback) não depender do estado
  // de autenticação — o intervalo de polling não precisa reiniciar a cada
  // render, só quando a sessão muda.
  const authenticatedRef = useRef(authenticated);
  useEffect(() => {
    authenticatedRef.current = authenticated;
  }, [authenticated]);

  const refresh = useCallback(async () => {
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
    if (authenticatedRef.current !== true) return;
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
  }, []);

  // Ping do daemon: roda SEMPRE (mesmo deslogado — a tela de login mostra o
  // aviso de daemon fora) e decide o indicador de conectividade.
  useEffect(() => {
    const ping = async () => {
      const up = await pingDaemon();
      setDaemonUp(up);
      if (!up) setStatus(null);
    };
    void ping();
    const id = setInterval(() => void ping(), 10_000);
    return () => clearInterval(id);
  }, []);

  // Status + presets em polling leve (10s), apenas autenticado. O intervalo
  // reinicia quando a autenticação muda (login/logout) — no login a carga é
  // imediata (mesmo comportamento do refresh() do provider antigo).
  useEffect(() => {
    if (!authenticated) return;
    void refresh();
    const id = setInterval(() => void refresh(), 10_000);
    return () => clearInterval(id);
  }, [authenticated, refresh]);

  // Stats em polling mais espaçado (60s): leitura de JSONL, custo baixo.
  useEffect(() => {
    if (!authenticated) return;
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
  }, [authenticated]);

  // Deslogado (logout ou sessão expirada): limpa os dados do usuário anterior.
  useEffect(() => {
    if (auth && !auth.authenticated) {
      setStatus(null);
      setPresets([]);
      setStats(null);
    }
  }, [auth]);

  return (
    <Ctx.Provider value={{ daemonUp, status, presets, stats, refresh }}>
      {children}
    </Ctx.Provider>
  );
}
