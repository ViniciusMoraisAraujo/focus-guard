// Testes da tela Segurança. O banner de bloqueio preventivo do Clock Guard
// (sentinela *all-internet*) foi removido junto com a funcionalidade de
// bloqueio total da internet. Os testes cobrem apenas o histórico tamper-log.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import type { ApiResponse, TamperEvent } from "@/api/types";
import { useData } from "@/context";
import { Seguranca } from "./Seguranca";

const now = new Date("2026-08-10T12:00:00Z");

function defaultData() {
  return {
    daemonUp: true,
    status: { success: true, blocks: [] } as ApiResponse,
    presets: [],
    stats: null,
    refresh: vi.fn(),
  };
}

vi.mock("@/context", () => ({
  useData: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  api: { tamperLog: vi.fn() },
}));

import { api } from "@/api/client";

const tamperLogMock = vi.mocked(api.tamperLog);
const useDataMock = vi.mocked(useData);

function okTamper(events: TamperEvent[]) {
  tamperLogMock.mockResolvedValue({ success: true, tamper_log: events });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  tamperLogMock.mockReset();
  useDataMock.mockReset();
  useDataMock.mockReturnValue(defaultData());
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Segurança", () => {
  it("mostra vazio quando não há eventos", async () => {
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.textContent).toContain("Nenhuma tentativa registrada");
  });

  it("mostra banner de daemon desligado", async () => {
    useDataMock.mockReturnValue({ ...defaultData(), daemonUp: false });
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.textContent).toContain("desligado");
  });

  it("mantém o badge do tamper-log para divergência confirmada (source=clock + action=lockdown)", async () => {
    okTamper([
      {
        at: now.toISOString(),
        source: "clock",
        action: "lockdown",
        detail: "relógio local 3h0m0s à frente do real, confirmado por NTP; expirações ajustadas para a hora real",
      },
    ]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.textContent).toContain("relógio");
    expect(container?.textContent).toContain("relógio fora da hora real");
    expect(container?.textContent).toContain("confirmado por NTP");
  });

  it("mostra eventos de restore de hosts", async () => {
    okTamper([
      {
        at: now.toISOString(),
        source: "hosts",
        action: "restore",
        detail: "youtube.com removido do /etc/hosts",
      },
    ]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.textContent).toContain("hosts");
    expect(container?.textContent).toContain("restaurado");
  });
});
