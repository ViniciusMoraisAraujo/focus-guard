import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import {
  SESSION_EXPIRED_EVENT,
  authStatus,
  login as apiLogin,
  logout as apiLogout,
} from "@/api/client";
import { NOT_AUTHENTICATED, type AuthState } from "./types";

export interface LoginResult {
  ok: boolean;
  message: string;
  username?: string;
  isAdmin?: boolean;
}

interface AuthContextValue {
  /** null = ainda checando a sessão no boot; AuthState depois disso. */
  auth: AuthState | null;
  login: (username: string, password: string) => Promise<LoginResult>;
  logout: () => Promise<void>;
}

const Ctx = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth fora do AuthProvider");
  return v;
}

/**
 * AuthProvider é dono EXCLUSIVO do estado de autenticação (auth, login,
 * logout e o evento de sessão expirada). Não carrega dados do daemon — isso
 * é do DataProvider, que fica abaixo e observa o auth para decidir quando
 * buscar (e quando limpar).
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthState | null>(null);

  const login = async (username: string, password: string): Promise<LoginResult> => {
    const res = await apiLogin(username, password);
    if (res.ok) {
      setAuth({
        authenticated: true,
        username: res.username ?? username,
        isAdmin: res.isAdmin ?? false,
      });
    }
    return res;
  };

  const logout = async () => {
    await apiLogout();
    setAuth(NOT_AUTHENTICATED);
  };

  // Boot: descobre se o browser já tem sessão (cookie fg_session). Enquanto
  // isso, auth === null (splash no App.tsx).
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const st = await authStatus();
      if (cancelled) return;
      if (st.authenticated) {
        setAuth({
          authenticated: true,
          username: st.username ?? "",
          isAdmin: st.is_admin ?? false,
        });
      } else {
        setAuth(NOT_AUTHENTICATED);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Sessão expirada (TTL 12h): o client.ts re-checa /api/auth/status após um
  // 401 e só então dispara o evento. Aqui devolvemos o gate à tela de login;
  // o DataProvider observa o auth e limpa os próprios dados (status/presets/
  // stats) — responsabilidade separada por provider.
  useEffect(() => {
    const onSessionExpired = () => setAuth(NOT_AUTHENTICATED);
    window.addEventListener(SESSION_EXPIRED_EVENT, onSessionExpired);
    return () => window.removeEventListener(SESSION_EXPIRED_EVENT, onSessionExpired);
  }, []);

  return <Ctx.Provider value={{ auth, login, logout }}>{children}</Ctx.Provider>;
}
