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

// Tokens de evento espelham o contrato (internal/ipc, Fase 7): o daemon os
// publica no hub, o focusguard-web os relê via SSE e o browser decide o que
// re-buscar. O daemon é a fonte da verdade — o evento só avisa.
//
// schedule-changed NÃO tem listener aqui de propósito: o provider não guarda
// schedules (a tela Agenda busca via api.scheduleList() no mount), então o
// mapeamento seria inerte. O daemon continua publicando (contrato para outros
// clientes — CLI/tray); o web cobre o caso com o refresh do próprio fluxo.
const EV_BLOCKS = "blocks-changed";
const EV_POMO_CHANGED = "pomodoro-changed";
const EV_POMO_COMPLETE = "pomodoro-complete";

// Fallback do SSE: quando o stream falha (daemon fora, servidor web
// reiniciado, sessão expirada), o polling cai para 30s. O EventSource
// reconecta sozinho e o fallback desliga no próximo onopen.
const FALLBACK_POLL_MS = 30_000;

/**
 * DataProvider é dono dos DADOS do daemon (conectividade + status + presets +
 * stats + eventos em tempo real). Fica DENTRO do AuthProvider: o estado de
 * autenticação decide quando buscar dados, sem que este provider se preocupe
 * com login/logout (SRP — F1 do plano de refatoração).
 *
 * Tempo real (Fase 7 — F3 do ui-plan): autenticado, o provider abre um
 * EventSource em /api/events. Cada evento coarse dispara o refresh do dado
 * afetado no lugar do polling de status/presets (10s → eventos). O ping de
 * conectividade continua em 10s (roda até deslogado) e stats mantém o
 * baseline de 60s + recarga imediata no pomodoro-complete.
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

  const reloadStats = useCallback(async () => {
    try {
      const st = await api.stats();
      if (st.success) setStats(st);
    } catch {
      /* stats são opcionais */
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

  // Eventos em tempo real (Fase 7): com sessão, abre o EventSource em
  // /api/events. No login a carga é imediata (refresh abaixo). Eventos
  // blocks/pomodoro/schedule → refresh de status+presets; pomodoro-complete →
  // também recarrega stats (a sessão que terminou mudou o histórico).
  useEffect(() => {
    if (!authenticated) return;
    void refresh();

    let es: EventSource | null = null;
    let fallback: number | null = null;
    const startFallback = () => {
      if (fallback != null) return;
      fallback = window.setInterval(() => void refresh(), FALLBACK_POLL_MS);
    };
    const stopFallback = () => {
      if (fallback != null) {
        window.clearInterval(fallback);
        fallback = null;
      }
    };

    es = new EventSource("/api/events");
    es.onopen = stopFallback;
    es.onerror = startFallback;
    const onRefresh = () => void refresh();
    es.addEventListener(EV_BLOCKS, onRefresh);
    es.addEventListener(EV_POMO_CHANGED, onRefresh);
    // Na conclusão o controller também publica pomodoro-changed (estado →
    // inativo); o refresh duplo é inofensivo e o pomodoro-complete garante a
    // recarga de stats.
    es.addEventListener(EV_POMO_COMPLETE, () => {
      void refresh();
      void reloadStats();
    });

    return () => {
      stopFallback();
      es?.close();
    };
  }, [authenticated, refresh, reloadStats]);

  // Stats em polling espaçado (60s): leitura de JSONL, custo baixo. O caminho
  // rápido é o evento pomodoro-complete (recarga imediata) — este baseline só
  // cobre mudanças que o daemon não publica (ex.: outro processo no CLI).
  useEffect(() => {
    if (!authenticated) return;
    void reloadStats();
    const id = setInterval(() => void reloadStats(), 60_000);
    return () => clearInterval(id);
  }, [authenticated, reloadStats]);

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
