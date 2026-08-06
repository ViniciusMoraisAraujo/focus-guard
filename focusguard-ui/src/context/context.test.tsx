// Testes de CARACTERIZAÇÃO (Fase 0): congelam o comportamento do AppProvider
// (auth + dados) ANTES da separação em AuthProvider/DataProvider. A sonda
// (Probe) lê o estado pela superfície pública via useApp, que continua
// existindo após a refatoração (compat: useAuth + useData combinados).
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
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
  return render(
    <AppProvider>
      <Probe />
    </AppProvider>,
  );
}

afterEach(() => {
  cleanup();
  fetchMock.mockReset();
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
          throw new Error(`action inesperada: ${body.action}`);
        },
      }),
    );

    await waitFor(() => expect(text("auth")).toBe("true"));
    await waitFor(() => expect(text("status-ok")).toBe("true"));
    await waitFor(() => expect(text("presets")).toBe("1"));
    expect(text("username")).toBe("admin");
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
        "/api/action": () => statusOk(),
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
        "/api/action": () => statusOk(),
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

function el(testId: string): HTMLElement {
  return document.querySelector(`[data-testid="${testId}"]`) as HTMLElement;
}

function text(testId: string): string {
  return el(testId)?.textContent ?? "";
}
