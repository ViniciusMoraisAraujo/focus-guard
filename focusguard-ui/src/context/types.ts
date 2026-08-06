/** Navegação principal (ids das telas do Shell). */
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

/**
 * Estado "não autenticado" — um AuthState de verdade (authenticated: false),
 * NUNCA null. No gate do App.tsx, null significa "ainda checando a sessão" e
 * renderiza o splash para sempre: confundir os dois tornava a tela de login
 * inalcançável (bug v0.15.1, corrigido).
 */
export const NOT_AUTHENTICATED: AuthState = { authenticated: false, username: "", isAdmin: false };
