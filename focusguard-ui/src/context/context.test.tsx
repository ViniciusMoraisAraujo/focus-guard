// Testes de CARACTERIZAÇÃO (Fase 0): congelam o comportamento do AppProvider
// (auth + dados) ANTES da separação em AuthProvider/DataProvider. A sonda
// (Probe) lê o estado pela superfície pública via useApp, que continua
// existindo após a refatoração (compat: useAuth + useData combinados).
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { AppProvider, useApp } from "@/context";

type Handler = (init?: RequestInit) => { status?: number; body: unknown };

// Roteador de fetch por URL: cada endpoint devolve um status/body fixo.
function route(handlers: Record<string, Handler>) {
  return (url: string, init?: RequestInit): Response => {
    const key = url.split("?")[0];
    const h = handlers[key];
    if (!h) throw new Error(`fetch não esperado neste teste: ${url}`);
    const { status = 200, body } = h(init);
    return new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  };
}

// FakeEventSource: substituto determinístico do EventSource (o jsdom tentaria
// uma conexão real a /api/events e poluiria os testes). O DataProvider (Fase
// 7) abre uma conexão quando autenticado; o fake registra os listeners e os
// testes disparam eventos/abertura/falha por ele.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  closed = false;
  private listeners = new Map<string, Array<(ev: unknown) => void>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, cb: (ev: unknown) => void) {
    const list = this.listeners.get(type) ?? [];
    list.push(cb);
    this.listeners.set(type, list);
  }

  close() {
    this.closed = true;
  }

  /** helpers de teste */
  emit(type: string) {
    for (const cb of this.listeners.get(type) ?? []) cb({ type });
  }

  open() {
    this.onopen?.();
  }

  fail() {
    this.onerror?.(new Event("error"));
  }
}

const fetchMock = vi.fn();

const ok = (body: unknown): { body: unknown } => ({ body });
const notAuthenticated = () => ok({ authenticated: false });
const authenticated = () => ok({ authenticated: true, username: "admin", is_admin: true });
const daemonUp = () => ok({ success: true });
const statusOk = () => ok({ success: true, blocks: [], current_version: "0.15.2" });
const presetsOk = () => ok({ success: true, presets: [{ name: "social", label: "Redes sociais", domains: ["x.com"] }] });

// Probe: expõe o estado do provider para as assertivas (sem toast/refresh).
function Probe() {
  const { auth, daemonUp: up, status, presets, stats, login, logout } = useApp();
  return (
    <div>
      <span data-testid="auth">{auth === null ? "null" : String(auth.authenticated)}</span>
      <span data-testid="username">{auth?.username ?? ""}</span>
      <span data-testid="daemonUp">{up === null ? "null" : String(up)}</span>
      <span data-testid="status-ok">{status ? String(status.success) : "null"}</span>
      <span data-testid="presets">{presets.length}</span>
      <span data-testid="stats">{stats ? "set" : "null"}</span>
      <button data-testid="login" onClick={() => void login("admin", "SP02cfasm#")}>
        login
      </button>
      <button data-testid="logout" onClick={() => void logout()}>logout</button>
    </div>
  );
}

function renderApp(fetchImpl: (url: string, init?: RequestInit) => Response) {
  fetchMock.mockImplementation(fetchImpl);
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("EventSource", FakeEventSource);
  return render(
    <AppProvider>
      <Probe />
    </AppProvider>,
  );
}

afterEach(() => {
  cleanup();
  fetchMock.mockReset();
  FakeEventSource.instances = [];
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("AppProvider — boot (checagem de sessão)", () => {
  it("sem sessão: auth=false e o daemon fica acessível", async () => {
    renderApp(
      route({
        "/api/auth/status": notAuthenticated,
        "/api/ping": daemonUp,
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("false"));
    expect(text("daemonUp")).toBe("true");
    expect(text("username")).toBe("");
  });

  it("com sessão válida: auth=true, username preenchido e dados carregados (status + presets)", async () => {
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") return statusOk();
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));
    await waitFor(() => expect(text("presets")).toBe("1"));
    expect(text("username")).toBe("admin");
    expect(FakeEventSource.instances.length).toBe(1); // SSE aberto com sessão
  });
});

describe("AppProvider — login e logout", () => {
  it("login bem-sucedido troca auth para true e carrega os dados", async () => {
    renderApp(
      route({
        "/api/auth/status": notAuthenticated,
        "/api/ping": daemonUp,
        "/api/login": () => ok({ success: true, username: "admin", is_admin: true }),
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") return statusOk();
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("false"));
    fireEvent.click(el("login"));
    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));
  });

  it("logout devolve auth=false e limpa status/presets", async () => {
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/logout": () => ok({ success: true }),
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") return statusOk();
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    fireEvent.click(el("logout"));
    await waitFor(() => expect(text("auth")).toBe("false"));
    expect(text("status-ok")).toBe("null");
  });
});

describe("AppProvider — sessão expirada e daemon offline", () => {
  it("evento de sessão expirada devolve auth=false e limpa os dados", async () => {
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") return statusOk();
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));

    window.dispatchEvent(new Event("focusguard:session-expired"));

    await waitFor(() => expect(text("auth")).toBe("false"));
    expect(text("status-ok")).toBe("null");
    expect(text("presets")).toBe("0");
    expect(text("stats")).toBe("null");
  });

  it("daemon offline: daemonUp=false e status fica nulo", async () => {
    renderApp(
      route({
        "/api/auth/status": notAuthenticated,
        "/api/ping": () => ({ status: 503, body: { success: false } }),
      }),
    );

    await waitFor(() => expect(text("daemonUp")).toBe("false"));
    expect(text("status-ok")).toBe("null");
  });
});

describe("AppProvider — eventos em tempo real (Fase 7)", () => {
  it("blocks-changed dispara o refresh de status (sem polling de 10s)", async () => {
    let statusCalls = 0;
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") {
            statusCalls++;
            return ok({
              success: true,
              blocks: statusCalls > 1 ? [{ domain: "x.com" }] : [],
            });
          }
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));
    expect(statusCalls).toBe(1); // carga inicial apenas — sem polling ativo

    const es = FakeEventSource.instances.at(-1);
    expect(es).toBeDefined();
    es?.open(); // SSE conectado
    es?.emit("blocks-changed");

    await waitFor(() => expect(statusCalls).toBe(2));
    expect(text("status-ok")).toBe("true");
  });

  it("pomodoro-complete recarrega stats (a sessão mudou o histórico)", async () => {
    let statsCalls = 0;
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") return statusOk();
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") {
            statsCalls++;
            return ok({ success: true });
          }
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("stats")).toBe("set"));
    expect(statsCalls).toBe(1); // baseline de 60s (carga imediata)

    const es = FakeEventSource.instances.at(-1);
    es?.emit("pomodoro-complete");

    await waitFor(() => expect(statsCalls).toBe(2));
  });

  it("stream quebrado liga o polling de fallback (30s) até reconectar", async () => {
    let statusCalls = 0;
    renderApp(
      route({
        "/api/auth/status": authenticated,
        "/api/ping": daemonUp,
        "/api/action": (init) => {
          const body = JSON.parse(String(init?.body)) as { action: string };
          if (body.action === "status") {
            statusCalls++;
            return statusOk();
          }
          if (body.action === "presets") return presetsOk();
          if (body.action === "stats") return ok({ success: true });
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));
    expect(statusCalls).toBe(1);

    const es = FakeEventSource.instances.at(-1);
    es?.open(); // conectado — sem fallback

    // O fallback só roda com timers falsos; a carga inicial e o SSE já
    // aconteceram com timers reais (waitFor não conflita). O fail() vem
    // DEPOIS do useFakeTimers para o intervalo de fallback nascer falso.
    vi.useFakeTimers();
    try {
      es?.fail(); // stream caiu → fallback de 30s
      await act(async () => {
        vi.advanceTimersByTime(31_000);
      });
      expect(statusCalls).toBeGreaterThanOrEqual(2);

      // Reconectou → fallback desliga (não vira polling permanente).
      es?.open();
      const before = statusCalls;
      await act(async () => {
        vi.advanceTimersByTime(31_000);
      });
      expect(statusCalls).toBe(before);
    } finally {
      vi.useRealTimers();
    }
  });
});

function el(testId: string): HTMLElement {
  return document.querySelector(`[data-testid="${testId}"]`) as HTMLElement;
}

function text(testId: string): string {
  return el(testId)?.textContent ?? "";
}
