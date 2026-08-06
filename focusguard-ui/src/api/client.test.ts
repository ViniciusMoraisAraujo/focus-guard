// Testes de CARACTERIZAÇÃO (Fase 0 do plano de refatoração): congelam o
// comportamento ATUAL do client de API antes da reestruturação dos providers.
// Não devem mudar de semântica com a refatoração — se um teste aqui quebrar,
// a refatoração mudou comportamento.
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  SESSION_EXPIRED_EVENT,
  DaemonError,
  SessionExpiredError,
  api,
  authStatus,
  login,
  logout,
  pingDaemon,
} from "./client";

const fetchMock = vi.fn();

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

describe("api.* — montagem do payload de /api/action", () => {
  it("status envia POST /api/action com action=status", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ success: true, blocks: [] }));

    const res = await api.status();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/action",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ "Content-Type": "application/json" }),
      }),
    );
    expect(fetchMock.mock.calls[0][1]?.body).toBe(JSON.stringify({ action: "status" }));
    expect(res.success).toBe(true);
  });

  it("block monta o payload com preset/domain/duration (estende sem sobrescrever action)", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ success: true }));
    let body: Record<string, unknown> | null = null;
    fetchMock.mockImplementation(async (_url: string, init?: RequestInit) => {
      body = JSON.parse(String(init?.body));
      return jsonResponse({ success: true });
    });

    await api.block({ preset: "social", duration: "4h", extend: true });

    expect(body).toEqual({ action: "block", preset: "social", duration: "4h", extend: true });
  });
});

describe("action — mapeamento de erros HTTP/redes", () => {
  it("503 (daemon fora) vira DaemonError com mensagem amigável", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({}, 503));

    await expect(api.status()).rejects.toBeInstanceOf(DaemonError);
    await expect(api.status()).rejects.toThrow("O daemon FocusGuard está desligado.");
  });

  it("401 (sessão expirada) dispara o re-check em /api/auth/status e o evento quando não autenticado", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock
      .mockResolvedValueOnce(jsonResponse({}, 401)) // /api/action
      .mockResolvedValueOnce(jsonResponse({ authenticated: false })); // re-check

    let fired = 0;
    const onEvent = () => fired++;
    window.addEventListener(SESSION_EXPIRED_EVENT, onEvent);
    try {
      await expect(api.status()).rejects.toBeInstanceOf(SessionExpiredError);
      await vi.waitFor(() => expect(fired).toBe(1));
      expect(fetchMock.mock.calls[1][0]).toBe("/api/auth/status");
    } finally {
      window.removeEventListener(SESSION_EXPIRED_EVENT, onEvent);
    }
  });

  it("401 com sessão ainda válida (transitório) não dispara o evento", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock
      .mockResolvedValueOnce(jsonResponse({}, 401))
      .mockResolvedValueOnce(jsonResponse({ authenticated: true, username: "admin" }));

    let fired = 0;
    const onEvent = () => fired++;
    window.addEventListener(SESSION_EXPIRED_EVENT, onEvent);
    try {
      await expect(api.status()).rejects.toBeInstanceOf(SessionExpiredError);
      await new Promise((r) => setTimeout(r, 30));
      expect(fired).toBe(0);
    } finally {
      window.removeEventListener(SESSION_EXPIRED_EVENT, onEvent);
    }
  });

  it("outro status HTTP (500) vira DaemonError genérico", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({}, 500));

    await expect(api.status()).rejects.toBeInstanceOf(DaemonError);
    await expect(api.status()).rejects.toThrow("Erro do servidor (HTTP 500).");
  });

  it("falha de rede vira DaemonError (servidor web fora do ar)", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockRejectedValue(new TypeError("fetch failed"));

    await expect(api.status()).rejects.toBeInstanceOf(DaemonError);
    await expect(api.status()).rejects.toThrow("Não foi possível falar com o servidor web.");
  });
});

describe("pingDaemon", () => {
  it("200 → true; falha de rede → false", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({}));
    await expect(pingDaemon()).resolves.toBe(true);

    fetchMock.mockRejectedValue(new TypeError("fetch failed"));
    await expect(pingDaemon()).resolves.toBe(false);
  });
});

describe("login / logout / authStatus", () => {
  it("login com sucesso devolve ok, username e is_admin do servidor", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ success: true, username: "admin", is_admin: true }));

    const res = await login("admin", "SP02cfasm#");

    expect(res.ok).toBe(true);
    expect(res.username).toBe("admin");
    expect(res.isAdmin).toBe(true);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/login");
  });

  it("login com credencial inválida (401) devolve ok=false com a mensagem", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ success: false, message: "usuário ou senha inválidos" }, 401));

    const res = await login("admin", "errada");

    expect(res.ok).toBe(false);
    expect(res.message).toBe("usuário ou senha inválidos");
  });

  it("login sem rede devolve ok=false (nunca lança)", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockRejectedValue(new TypeError("fetch failed"));

    const res = await login("admin", "SP02cfasm#");

    expect(res.ok).toBe(false);
    expect(res.message).toContain("servidor web");
  });

  it("authStatus devolve o payload parseado; !ok devolve {authenticated:false}", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ authenticated: true, username: "admin", is_admin: true }));
    await expect(authStatus()).resolves.toEqual({ authenticated: true, username: "admin", is_admin: true });

    fetchMock.mockResolvedValue(jsonResponse({}, 500));
    await expect(authStatus()).resolves.toEqual({ authenticated: false });
  });

  it("logout chama POST /api/logout", async () => {
    vi.stubGlobal("fetch", fetchMock);
    fetchMock.mockResolvedValue(jsonResponse({ success: true }));

    await logout();

    expect(fetchMock).toHaveBeenCalledWith("/api/logout", expect.objectContaining({ method: "POST" }));
  });
});
