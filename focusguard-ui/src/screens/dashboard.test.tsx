// Testes do Dashboard. O polling do tamper-log do Clock Guard foi removido
// junto com a funcionalidade de bloqueio total da internet.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import type { ApiResponse } from "@/api/types";
import { Dashboard } from "./Dashboard";

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

import { useData } from "@/context";

vi.mock("@/api/client", () => ({
  api: { tamperLog: vi.fn() },
}));

const useDataMock = vi.mocked(useData);

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  useDataMock.mockReset();
  useDataMock.mockReturnValue(defaultData());
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Dashboard", () => {
  it("renderiza sem bloqueios ativos", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(container?.textContent).toContain("Sem bloqueios ativos");
    expect(container?.textContent).toContain("Ótimo momento para iniciar um foco");
  });

  it("mostra foco ativo com bloqueios", async () => {
    useDataMock.mockReturnValue({
      ...defaultData(),
      status: {
        success: true,
        blocks: [
          {
            domain: "youtube.com",
            started_at: new Date(now.getTime() - 10 * 60 * 1000).toISOString(),
            expires_at: new Date(now.getTime() + 50 * 60 * 1000).toISOString(),
            resolved_ips: ["1.2.3.4"],
          },
        ],
      } as ApiResponse,
    });

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Dashboard onNavigate={() => {}} />);
      container = r.container;
    });

    expect(container?.textContent).toContain("Foco ativo");
    expect(container?.textContent).toContain("1 bloqueio");
    expect(container?.textContent).toContain("youtube.com");
  });
});
