// Testes do banner de bloqueio preventivo do Clock Guard na tela Segurança.
// O estado VIVO do lockdown vem do status (sentinela *all-internet* com
// source "clock-guard" não expirado) — o tamper-log só registra burlas
// CONFIRMADAS por NTP, então o lockdown de suspeita (NTP offline/falhou) não
// tem evento correspondente e só aparece por aqui. O pânico do usuário
// (mesmo sentinela, source "user"/ausente) NÃO renderiza o banner.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import type { ApiResponse, Block, TamperEvent } from "@/api/types";
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

// A única chamada do client na tela é o tamper-log (o banner lê o status do
// data-context, sem fetch novo).
vi.mock("@/api/client", () => ({
  api: { tamperLog: vi.fn() },
}));

import { api } from "@/api/client";

const tamperLogMock = vi.mocked(api.tamperLog);
const useDataMock = vi.mocked(useData);

function sentinelClockGuard(expiresAt: string): Block {
  return {
    domain: "*all-internet*",
    source: "clock-guard",
    started_at: now.toISOString(),
    expires_at: expiresAt,
    resolved_ips: [],
  };
}

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

describe("Segurança — bloqueio preventivo do Clock Guard ativo", () => {
  it("mostra o banner quando o lockdown do clock guard está ativo no status", async () => {
    useDataMock.mockReturnValue({
      ...defaultData(),
      status: {
        success: true,
        blocks: [sentinelClockGuard(new Date(now.getTime() + 30 * 60 * 1000).toISOString())],
      } as ApiResponse,
    });
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    const alert = container?.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain("Bloqueio preventivo do relógio ativo");
    // Sem eventos de tamper (lockdown de suspeita não loga) o banner é a
    // única evidência — e a lista continua mostrando o vazio.
    expect(container?.textContent).toContain("Nenhuma tentativa registrada");
  });

  it("não mostra o banner para o modo pânico do usuário (source user)", async () => {
    useDataMock.mockReturnValue({
      ...defaultData(),
      status: {
        success: true,
        blocks: [{ ...sentinelClockGuard(now.toISOString()), source: "user" }],
      } as ApiResponse,
    });
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
  });

  it("não mostra o banner para sentinela legado sem source (pré-migração)", async () => {
    // Um sentinela persistido antes do campo source (ou criado por outro
    // cliente) nunca é do guard — o scheduler trata legacy como do usuário.
    const legacy: Block = sentinelClockGuard(now.toISOString());
    delete legacy.source;
    useDataMock.mockReturnValue({
      ...defaultData(),
      status: { success: true, blocks: [legacy] } as ApiResponse,
    });
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
  });

  it("não mostra o banner com sentinela do clock guard já expirado", async () => {
    useDataMock.mockReturnValue({
      ...defaultData(),
      status: {
        success: true,
        blocks: [sentinelClockGuard(new Date(now.getTime() - 60 * 1000).toISOString())],
      } as ApiResponse,
    });
    okTamper([]);

    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Seguranca />);
      container = r.container;
    });

    expect(container?.querySelector('[role="alert"]')).toBeNull();
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
    // NTP confirmou → sem bloqueio: o badge de clock diz a verdade (o
    // "bloqueio preventivo" só existe na suspeita com NTP indisponível).
    expect(container?.textContent).toContain("relógio fora da hora real");
    expect(container?.textContent).not.toContain("bloqueio preventivo");
    expect(container?.textContent).toContain("confirmado por NTP");
    expect(container?.textContent).toContain("sem bloqueio");
  });
});
