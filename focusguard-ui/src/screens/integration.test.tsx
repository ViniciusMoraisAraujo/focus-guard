import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { api, pingDaemon, authStatus } from "@/api/client";
import { AuthProvider } from "@/context/auth-context";
import { DataProvider } from "@/context/data-context";
import { Bloquear } from "./Bloquear";
import { Dashboard } from "./Dashboard";
import { Pomodoro } from "./Pomodoro";
import type { ApiResponse } from "@/api/types";

const now = new Date("2026-08-28T14:00:00Z");

vi.mock("@/api/client", () => ({
  pingDaemon: vi.fn(),
  authStatus: vi.fn(),
  SESSION_EXPIRED_EVENT: "focusguard:session-expired",
  api: {
    status: vi.fn(),
    presets: vi.fn(),
    stats: vi.fn(),
    block: vi.fn(),
    pomodoro: vi.fn(),
    pomodoroStop: vi.fn(),
    pomodoroDefaults: vi.fn().mockResolvedValue({ success: true, work_min: 25, rest_min: 5 }),
    tamperLog: vi.fn(),
  },
  DaemonError: class DaemonError extends Error {},
}));

const mockPingDaemon = vi.mocked(pingDaemon);
const mockAuthStatus = vi.mocked(authStatus);
const mockApi = vi.mocked(api);

// Mock de EventSource para testar SSE
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners: Record<string, ((ev: MessageEvent) => void)[]> = {};

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
    setTimeout(() => {
      if (this.onopen) this.onopen();
    }, 10);
  }

  addEventListener(type: string, listener: (ev: MessageEvent) => void) {
    if (!this.listeners[type]) this.listeners[type] = [];
    this.listeners[type].push(listener);
  }

  removeEventListener(type: string, listener: (ev: MessageEvent) => void) {
    if (this.listeners[type]) {
      this.listeners[type] = this.listeners[type].filter((l) => l !== listener);
    }
  }

  close() {}

  emit(type: string, data = "") {
    const list = this.listeners[type] || [];
    for (const l of list) {
      l(new MessageEvent(type, { data }));
    }
  }
}

describe("E2E Integration: Fluxo de Bloqueio, SSE e Timers", () => {
  let originalEventSource: typeof EventSource;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
    MockEventSource.instances = [];
    originalEventSource = globalThis.EventSource;
    // @ts-expect-error Mocking global EventSource
    globalThis.EventSource = MockEventSource;

    mockPingDaemon.mockResolvedValue(true);
    mockAuthStatus.mockResolvedValue({
      authenticated: true,
    });
    mockApi.presets.mockResolvedValue({
      success: true,
      presets: [
        {
          name: "social",
          label: "Redes Sociais",
          description: "Twitter, Instagram, etc.",
          domains: ["twitter.com", "instagram.com"],
        },
      ],
    } as unknown as ApiResponse);
    mockApi.stats.mockResolvedValue({ success: true } as ApiResponse);
    mockApi.tamperLog.mockResolvedValue({ success: true, events: [] } as unknown as ApiResponse);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    globalThis.EventSource = originalEventSource;
    vi.clearAllMocks();
  });

  it("1. Fluxo de Bloqueio Completo: Bloquear domínio e refletir no Dashboard", async () => {
    let currentBlocks: any[] = [];
    mockApi.status.mockImplementation(async () => ({
      success: true,
      blocks: currentBlocks,
    } as unknown as ApiResponse));

    mockApi.block.mockImplementation(async (req) => {
      currentBlocks = [
        {
          domain: req.domain,
          started_at: now.toISOString(),
          expires_at: new Date(now.getTime() + 60 * 60 * 1000).toISOString(),
          resolved_ips: ["104.244.42.1"],
        },
      ];
      return { success: true, message: "Bloqueio aplicado!" } as ApiResponse;
    });

    const TestApp = () => (
      <AuthProvider>
        <DataProvider>
          <div>
            <Bloquear />
            <Dashboard onNavigate={() => {}} />
          </div>
        </DataProvider>
      </AuthProvider>
    );

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<TestApp />);
      container = r.container;
    });

    expect(container?.textContent).toContain("Sem bloqueios ativos");

    // Seleciona o preset 'social'
    const presetBtn = screen.getByRole("button", { name: /redes sociais/i });
    await act(async () => {
      fireEvent.click(presetBtn);
    });

    // Clica no botão Bloquear
    const blockButton = screen.getByRole("button", { name: /^bloquear$/i });
    await act(async () => {
      fireEvent.click(blockButton);
    });

    // Verifica que a API de bloqueio foi chamada com os parâmetros corretos
    expect(mockApi.block).toHaveBeenCalledWith(
      expect.objectContaining({
        preset: "social",
        duration: "1h",
      })
    );

    // O Dashboard agora deve exibir o foco ativo
    expect(container?.textContent).toContain("Foco ativo");
  });

  it("2. Eventos SSE em Tempo Real: Atualiza dados automaticamente sem interação do usuário", async () => {
    let callCount = 0;
    mockApi.status.mockImplementation(async () => {
      callCount++;
      if (callCount === 1) {
        return { success: true, blocks: [] } as unknown as ApiResponse;
      }
      return {
        success: true,
        blocks: [
          {
            domain: "reddit.com",
            started_at: now.toISOString(),
            expires_at: new Date(now.getTime() + 30 * 60 * 1000).toISOString(),
            resolved_ips: ["151.101.1.140"],
          },
        ],
      } as unknown as ApiResponse;
    });

    const TestDashboard = () => (
      <AuthProvider>
        <DataProvider>
          <Dashboard onNavigate={() => {}} />
        </DataProvider>
      </AuthProvider>
    );

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<TestDashboard />);
      container = r.container;
    });

    expect(container?.textContent).toContain("Sem bloqueios ativos");

    // Simula chegada de evento SSE 'blocks-changed' emitido pelo daemon
    expect(MockEventSource.instances.length).toBeGreaterThan(0);
    const es = MockEventSource.instances[0];

    await act(async () => {
      es.emit("blocks-changed");
    });

    // O Dashboard deve ter reagido ao evento SSE e atualizado os dados para reddit.com
    expect(container?.textContent).toContain("Foco ativo");
    expect(container?.textContent).toContain("reddit.com");
  });

  it("3. Expiração de Timers: Contagem regressiva em tempo real e expiração", async () => {
    mockApi.status.mockResolvedValue({
      success: true,
      blocks: [
        {
          domain: "youtube.com",
          started_at: now.toISOString(),
          expires_at: new Date(now.getTime() + 10 * 60 * 1000).toISOString(), // 10 min
          resolved_ips: ["142.250.190.46"],
        },
      ],
    } as unknown as ApiResponse);

    const TestDashboard = () => (
      <AuthProvider>
        <DataProvider>
          <Dashboard onNavigate={() => {}} />
        </DataProvider>
      </AuthProvider>
    );

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<TestDashboard />);
      container = r.container;
    });

    expect(container?.textContent).toContain("youtube.com");
    expect(container?.textContent).toContain("10:00");

    // Avança 6 minutos no tempo
    await act(async () => {
      vi.advanceTimersByTime(6 * 60 * 1000);
    });

    // Deve exibir 04:00 restantes
    expect(container?.textContent).toContain("04:00");
  });

  it("4. Sessão Pomodoro: Início de ciclo com missão e alternância", async () => {
    mockApi.status.mockResolvedValue({
      success: true,
      pomodoro: {
        active: true,
        phase: "work",
        cycle: 1,
        cycles: 4,
        preset: "social",
        work_sec: 1500,
        rest_sec: 300,
        started_at: now.toISOString(),
        phase_started_at: now.toISOString(),
        phase_expires_at: new Date(now.getTime() + 25 * 60 * 1000).toISOString(),
      },
    } as unknown as ApiResponse);

    mockApi.pomodoro.mockResolvedValue({
      success: true,
      message: "Pomodoro iniciado!",
    } as ApiResponse);

    const TestPomodoro = () => (
      <AuthProvider>
        <DataProvider>
          <Pomodoro />
        </DataProvider>
      </AuthProvider>
    );

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<TestPomodoro />);
      container = r.container;
    });

    expect(container?.textContent).toContain("social");
    expect(container?.textContent).toContain("Foco");
    expect(container?.textContent).toContain("ciclo 1/4");
  });
});
