import type {
  ApiRequest,
  ApiResponse,
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
): Promise<{ ok: boolean; message: string }> {
  const resp = await action(req);
  if (!resp.success) {
    return { ok: false, message: resp.message ?? "Falha ao executar a ação." };
  }
  return { ok: true, message: resp.message ?? "" };
}

export const api = {
  status: () => action({ action: "status" }),
  presets: () => action({ action: "presets" }),
  stats: () => action({ action: "stats" }),
  block: (p: { domain?: string; preset?: string; duration: string }) =>
    action({ action: "block", ...p }),
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

  // Atualização
  update: (channel?: string) =>
    action({ action: "update", channel }, UPDATE_ACTION_TIMEOUT_MS),
  updateCheck: (channel?: string) =>
    action({ action: "update-check", channel }, UPDATE_ACTION_TIMEOUT_MS),
};
