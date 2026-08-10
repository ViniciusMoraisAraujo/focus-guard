import type {
  ApiRequest,
  ApiResponse,
  Device,
  ReportConfig,
  ScheduleRule,
} from "./types";

// UPDATE_ACTION_TIMEOUT_MS: o proxy web (internal/httpapi) dá 150s para as
// ações update/update-check (download + troca de binários). O timeout do
// browser precisa ser maior para não cancelar um update que ainda vai
// terminar bem — 170s cobre o proxy com folga.
const UPDATE_ACTION_TIMEOUT_MS = 170_000;

// DaemonError distingue falha de conectividade (daemon/servidor fora do ar)
// de uma resposta de erro do daemon (que chega como success:false normal).
export class DaemonError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DaemonError";
  }
}

/**
 * SessionExpiredError marca um 401 do /api/action (sessão inválida/expirada
 * — o endpoint exige sessão). Estende DaemonError para as telas continuarem
 * mostrando e.message; o AppProvider também é avisado (evento) para devolver
 * o gate à tela de login.
 */
export class SessionExpiredError extends DaemonError {
  constructor(message: string) {
    super(message);
    this.name = "SessionExpiredError";
  }
}

// SESSION_EXPIRED_EVENT é o evento de janela que informa o AppProvider que a
// sessão morreu (após o re-check em /api/auth/status confirmar). Exportado
// para o context.tsx escutar sem duplicar a string.
export const SESSION_EXPIRED_EVENT = "focusguard:session-expired";

// confirmSessionExpired re-consulta /api/auth/status depois de um 401 do
// /api/action: só quando o servidor confirma que não há sessão o evento é
// disparado (o gate troca o splash/login na hora). Best-effort — sem rede,
// o próximo /api/action ou poll re-tenta.
async function confirmSessionExpired(): Promise<void> {
  try {
    const res = await fetch("/api/auth/status");
    if (!res.ok) return;
    const st = (await res.json()) as AuthStatus;
    if (st.authenticated) return; // sessão ainda válida (401 transitório) — nada a fazer
    window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
  } catch {
    /* sem rede: o gate segue; a próxima chamada re-verifica */
  }
}

// action envia uma ação ao servidor web. timeoutMs opcional protege contra
// respostas que nunca chegam (ex.: o proxy IPC travou); o padrão deixa o
// servidor web decidir (proxyTimeout/updateTimeout do lado Go).
async function action(req: ApiRequest, timeoutMs?: number): Promise<ApiResponse> {
  const controller = timeoutMs ? new AbortController() : undefined;
  const timer = timeoutMs
    ? window.setTimeout(() => controller?.abort(), timeoutMs)
    : undefined;
  let res: Response;
  try {
    res = await fetch("/api/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
      signal: controller?.signal,
    });
  } catch {
    if (controller?.signal.aborted) {
      throw new DaemonError("A operação demorou demais e foi cancelada. Tente novamente.");
    }
    throw new DaemonError("Não foi possível falar com o servidor web. Ele está rodando?");
  } finally {
    if (timer) window.clearTimeout(timer);
  }
  if (res.status === 503) {
    throw new DaemonError("O daemon FocusGuard está desligado.");
  }
  if (res.status === 401) {
    // Sessão inválida/expirada: confirma no /api/auth/status (o re-check) e
    // avisa o AppProvider para voltar o gate à tela de login. A exceção
    // especial ainda deixa a tela que disparou a ação mostrar o motivo.
    void confirmSessionExpired();
    throw new SessionExpiredError("Sua sessão expirou. Entre novamente.");
  }
  if (!res.ok) {
    throw new DaemonError(`Erro do servidor (HTTP ${res.status}).`);
  }
  return (await res.json()) as ApiResponse;
}

/** pingDaemon responde true quando o focusguard-web alcança o daemon. */
export async function pingDaemon(): Promise<boolean> {
  try {
    const res = await fetch("/api/ping");
    return res.ok;
  } catch {
    return false;
  }
}

/** execAction roda uma ação e devolve um erro amigável quando o daemon rejeita. */
export async function execAction(
  req: ApiRequest,
): Promise<{ ok: boolean; message: string; code?: string; updatePendingReboot?: boolean }> {
  const resp = await action(req);
  if (!resp.success) {
    return { ok: false, message: resp.message ?? "Falha ao executar a ação.", code: resp.code };
  }
  return {
    ok: true,
    message: resp.message ?? "",
    code: resp.code,
    updatePendingReboot: resp.update_pending_reboot,
  };
}

// ---------------------------------------------------------------------------
// Autenticação (Fase 4): /api/login, /api/logout e /api/auth/status são
// endpoints públicos (o gate vive no React). Erros chegam como
// { success:false, message } — 401 = credencial inválida, 429 = rate limit.
// ---------------------------------------------------------------------------

/** AuthStatus espelha o JSON de GET /api/auth/status (sempre 200). */
export interface AuthStatus {
  authenticated: boolean;
  username?: string;
  is_admin?: boolean;
}

/** authStatus pergunta ao servidor se o browser tem uma sessão válida. */
export async function authStatus(): Promise<AuthStatus> {
  try {
    const res = await fetch("/api/auth/status");
    if (!res.ok) return { authenticated: false };
    return (await res.json()) as AuthStatus;
  } catch {
    return { authenticated: false };
  }
}

/**
 * login autentica no focusguard-web. Devolve ok=false com uma mensagem
 * amigável para credencial errada (401), rate limit (429), daemon fora (503)
 * ou falha de rede — nunca lança.
 */
export async function login(
  username: string,
  password: string,
): Promise<{
  ok: boolean;
  message: string;
  username?: string;
  isAdmin?: boolean;
}> {
  let res: Response;
  try {
    res = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
  } catch {
    return {
      ok: false,
      message: "Não foi possível falar com o servidor web. Ele está rodando?",
    };
  }
  const data = (await res.json().catch(() => null)) as {
    success?: boolean;
    message?: string;
    username?: string;
    is_admin?: boolean;
  } | null;
  if (!res.ok) {
    return {
      ok: false,
      message: data?.message ?? `Erro do servidor (HTTP ${res.status}).`,
    };
  }
  return {
    ok: true,
    message: data?.message ?? "",
    username: data?.username,
    isAdmin: data?.is_admin,
  };
}

/** logout invalida a sessão no servidor (best-effort: falha de rede não importa). */
export async function logout(): Promise<void> {
  try {
    await fetch("/api/logout", { method: "POST" });
  } catch {
    /* a sessão local morre de qualquer forma */
  }
}

export const api = {
  status: () => action({ action: "status" }),
  presets: () => action({ action: "presets" }),
  stats: () => action({ action: "stats" }),
  block: (p: {
    domain?: string;
    preset?: string;
    duration: string;
    extend?: boolean;
    replace?: boolean;
  }) => action({ action: "block", ...p }),
  blockAll: (duration: string, allowlist: string[]) =>
    action({ action: "block-all", duration, allowlist }),
  goalSet: (goalMinutes: number) => action({ action: "goal-set", goal_minutes: goalMinutes }),

  // Pomodoro
  pomodoro: (p: {
    preset: string;
    work_min?: number;
    rest_min?: number;
    cycles?: number;
    strict?: boolean;
    save?: boolean;
    label?: string;
  }) => action({ action: "pomodoro", ...p }),
  pomodoroStop: () => action({ action: "pomodoro-stop" }),
  pomodoroDefaults: () => action({ action: "pomodoro-defaults" }),

  // Agenda (agendamentos recorrentes)
  scheduleList: () => action({ action: "schedule-list" }),
  scheduleAdd: (rule: ScheduleRule) => action({ action: "schedule-add", schedule_rule: rule }),
  scheduleRemove: (id: string) => action({ action: "schedule-remove", schedule_id: id }),
  scheduleImport: (icsContent: string, preset: string) =>
    action({ action: "schedule-import", ics_content: icsContent, ics_preset: preset }),

  // Apps (denylist de processos)
  appsList: () => action({ action: "apps-list" }),
  appsAdd: (name: string) => action({ action: "apps-add", app_name: name }),
  appsRemove: (name: string) => action({ action: "apps-remove", app_name: name }),

  // Presets personalizados
  presetAdd: (name: string, label: string, domains: string[]) =>
    action({ action: "preset-add", preset_name: name, preset_label: label, preset_domains: domains }),
  presetRemove: (name: string) => action({ action: "preset-remove", preset_name: name }),

  // Estatísticas e missões
  missions: () => action({ action: "missions" }),
  sessions: () => action({ action: "sessions" }),

  // Histórico de burla
  tamperLog: () => action({ action: "tamper-log" }),

  // Servidor DNS sinkhole ("Rei da Rede")
  dnsStart: () => action({ action: "dns-start" }),
  dnsStop: () => action({ action: "dns-stop" }),
  dnsStatus: () => action({ action: "dns-status" }),
  dnsSetUpstream: (upstream: string) =>
    action({ action: "dns-set-upstream", upstream }),
  dnsTelemetry: (limit?: number) =>
    action({ action: "dns-telemetry", telemetry_limit: limit }),

  // Focus Interceptor Page (Fase 3)
  interceptorSet: (enabled: boolean) =>
    action({ action: "interceptor-set", interceptor_enabled: enabled }),
  interceptorStatus: () => action({ action: "interceptor-status" }),

  // Dispositivos (Fase 4 — edição Server)
  devicesList: () => action({ action: "devices-list" }),
  devicesUpsert: (device: Device) => action({ action: "devices-upsert", device }),
  devicesRemove: (ip: string) => action({ action: "devices-remove", device_ip: ip }),

  // Conquistas (Fase 5.2)
  achievements: () => action({ action: "achievements-get" }),

  // Relatório semanal (Fase 5.1)
  reportConfigGet: () => action({ action: "reports-config-get" }),
  reportConfigSet: (reportConfig: ReportConfig) =>
    action({ action: "reports-config-set", report_config: reportConfig }),
  reportGenerate: (reportExportPath?: string) =>
    action({ action: "reports-generate", report_export_path: reportExportPath }),

  // Usuários da interface web (só admin gerencia; senha própria para o resto)
  usersList: () => action({ action: "user-list" }),
  userAdd: (name: string, password: string) =>
    action({ action: "user-add", user_name: name, user_password: password }),
  userRemove: (name: string) => action({ action: "user-remove", user_name: name }),
  userSetPassword: (name: string, password: string) =>
    action({ action: "user-set-password", user_name: name, user_password: password }),

  // Atualização
  update: (channel?: string) =>
    action({ action: "update", channel }, UPDATE_ACTION_TIMEOUT_MS),
  updateCheck: (channel?: string) =>
    action({ action: "update-check", channel }, UPDATE_ACTION_TIMEOUT_MS),
};
