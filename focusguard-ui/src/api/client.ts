import type { ApiRequest, ApiResponse } from "./types";

// DaemonError distingue falha de conectividade (daemon/servidor fora do ar)
// de uma resposta de erro do daemon (que chega como success:false normal).
export class DaemonError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DaemonError";
  }
}

async function action(req: ApiRequest): Promise<ApiResponse> {
  let res: Response;
  try {
    res = await fetch("/api/action", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    });
  } catch {
    throw new DaemonError("Não foi possível falar com o servidor web. Ele está rodando?");
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

export const api = {
  status: () => action({ action: "status" }),
  presets: () => action({ action: "presets" }),
  stats: () => action({ action: "stats" }),
  block: (p: { domain?: string; preset?: string; duration: string }) =>
    action({ action: "block", ...p }),
  blockAll: (duration: string, allowlist: string[]) =>
    action({ action: "block-all", duration, allowlist }),
  goalSet: (goalMinutes: number) => action({ action: "goal-set", goal_minutes: goalMinutes }),
};
