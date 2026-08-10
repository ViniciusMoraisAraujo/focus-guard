// Testes do alerta de Clock Guard (Fase 2 — só front-end) no Dashboard.
// Congelam o filtro do tamper-log: um evento source="clock" + action
// "lockdown" dentro da janela de 1h renderiza o alerta destrutivo; eventos
// antigos, de outra fonte/ação ou ausentes não renderizam nada. O polling de
// 30s do componente é isolado com timers falsos (o load do mount decide o
// estado; o intervalo só re-consulta).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import type { ApiResponse, TamperEvent } from "@/api/types";
import { Dashboard } from "./Dashboard";

const now = new Date("2026-08-10T12:00:00Z");

// O Dashboard (e o DnsCard interno) lê o estado pelo useData; o teste injeta
// um daemon acessível sem bloqueios para isolar o alerta.
vi.mock("@/context", () => ({
  useData: () => ({
    daemonUp: true,
    status: { success: true, blocks: [] } as ApiResponse,
    presets: [],
    stats: null,
    refresh: vi.fn(),
  }),
}));

// api.tamperLog é a única chamada do alerta — o restante do client é inerte
// no teste (nenhuma outra ação roda com status vazio).
vi.mock("@/api/client", () => ({
  api: { tamperLog: vi.fn() },
}));

import { api } from "@/api/client";

const tamperLogMock = vi.mocked(api.tamperLog);

function evento(partial: Partial<TamperEvent>): TamperEvent {
  return {
    at: now.toISOString(),
    source: "clock",
    action: "lockdown",
    ...partial,
  };
}

function okTamper(events: TamperEvent[]) {
  tamperLogMock.mockResolvedValue({ success: true, tamper_log: events });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  tamperLogMock.mockReset();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Dashboard — alerta de Clock Guard (Fase 2)", () => {
  it("renderiza o alerta destrutivo com lockdown de relógio recente", async () => {
    okTamper([
      evento({
        at: new Date(now.getTime() - 5 * 60 * 1000).toISOString(), // 5 min atrás
        detail: "relógio adulterado; bloqueio preventivo até 13:00",
      }),
    ]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(tamperLogMock).toHaveBeenCalledTimes(1);
    expect(container?.querySelector('[role="alert"]')).not.toBeNull();
    expect(container?.querySelector('[role="alert"]')?.textContent).toContain(
      "Inconsistência de relógio detectada",
    );
    expect(container?.querySelector('[role="alert"]')?.textContent).toContain("bloqueio preventivo");
    expect(container?.querySelector('[role="alert"]')?.textContent).toContain("13:00");
  });

  it("não renderiza o alerta sem eventos de clock/lockdown", async () => {
    okTamper([evento({ source: "hosts", action: "restore" })]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
  });

  it("ignora lockdown de relógio fora da janela de 1h", async () => {
    okTamper([
      evento({ at: new Date(now.getTime() - 2 * 60 * 60 * 1000).toISOString() }), // 2h atrás
    ]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
  });

  it("trata falha do tamper-log como ausência de alerta", async () => {
    tamperLogMock.mockRejectedValue(new Error("daemon fora"));

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
  });
});
