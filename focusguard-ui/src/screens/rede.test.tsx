// Testes do card de acesso ao manual do DNS sinkhole na tela Rede: o card
// compacto deve resumir o guia e levar à tela Guia (manual completo com guias
// por fabricante).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render } from "@testing-library/react";
import type { ApiResponse } from "@/api/types";
import { useData } from "@/context";
import { Rede } from "./Rede";

vi.mock("@/context", () => ({
  useData: vi.fn(),
}));

// A tela consulta a telemetria (só quando listening) e a lista de dispositivos
// (incondicional). Com dns_listening=false, a telemetria nem é chamada.
vi.mock("@/api/client", () => ({
  api: { dnsTelemetry: vi.fn(), devicesList: vi.fn() },
  execAction: vi.fn(),
}));

import { api } from "@/api/client";

const useDataMock = vi.mocked(useData);
const devicesListMock = vi.mocked(api.devicesList);

function defaultData() {
  return {
    daemonUp: true,
    status: {
      success: true,
      dns_enabled: false,
      dns_listening: false,
      dns_upstream: "1.1.1.2:53",
      lan_ip: "192.168.1.100",
      lan_mac: "aa:bb:cc:dd:ee:ff",
    } as ApiResponse,
    presets: [],
    stats: null,
    refresh: vi.fn(),
  };
}

beforeEach(() => {
  devicesListMock.mockReset();
  devicesListMock.mockResolvedValue({ success: true, devices: [] });
  useDataMock.mockReset();
  useDataMock.mockReturnValue(defaultData());
});

afterEach(() => {
  cleanup();
});

describe("Rede — acesso ao manual do DNS sinkhole", () => {
  it("mostra o card compacto com o botão para o guia completo", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Rede onNavigate={vi.fn()} />);
      container = r.container;
    });

    const text = container?.textContent ?? "";
    expect(text).toContain("Manual de configuração");
    expect(text).toContain("guias por fabricante");
    expect(text).toContain("Abrir guia completo");
  });

  it("mostra o IP e o MAC da máquina ao lado do botão Ligar (reserva DHCP)", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Rede onNavigate={vi.fn()} />);
      container = r.container;
    });

    const text = container?.textContent ?? "";
    expect(text).toContain("IP da máquina");
    expect(text).toContain("aa:bb:cc:dd:ee:ff");
    expect(text).toContain("reserva DHCP");
    // O IP aparece no card principal E no card compacto do manual (2×).
    const ips = text.match(/192\.168\.1\.100/g) ?? [];
    expect(ips.length).toBeGreaterThanOrEqual(2);
  });

  it("o botão 'Abrir guia completo' navega para a tela Guia", async () => {
    const onNavigate = vi.fn();
    let r: ReturnType<typeof render> | undefined;
    await act(async () => {
      r = render(<Rede onNavigate={onNavigate} />);
    });

    fireEvent.click(
      (r as ReturnType<typeof render>).getByRole("button", {
        name: /Abrir guia completo/i,
      }),
    );
    expect(onNavigate).toHaveBeenCalledWith("guia");
  });
});
