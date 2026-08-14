// Testes do manual de configuração do DNS sinkhole na tela Rede: o card
// "Manual — como configurar o DNS sinkhole" deve cobrir as duas pontas da
// configuração (sistema Windows e roteador/modem) e o diagnóstico.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
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

describe("Rede — manual de configuração do DNS sinkhole", () => {
  it("renderiza as três seções do manual (sistema, roteador e testes)", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Rede />);
      container = r.container;
    });

    const text = container?.textContent ?? "";
    expect(text).toContain("Manual — como configurar o DNS sinkhole");
    expect(text).toContain("No sistema (Windows)");
    expect(text).toContain("No roteador (modem)");
    expect(text).toContain("Testar e diagnosticar");
  });

  it("manual do sistema: firewall automático, perfil Privada e porta 53", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Rede />);
      container = r.container;
    });

    const text = container?.textContent ?? "";
    expect(text).toContain("abre a porta 53 no firewall automaticamente");
    expect(text).toContain("Rede privada");
    expect(text).toContain("net stop SharedAccess");
  });

  it("manual do roteador: DHCP, failover e o bypass IPv6 (fe80::1)", async () => {
    let container: HTMLElement | undefined;
    await act(async () => {
      const r = render(<Rede />);
      container = r.container;
    });

    const text = container?.textContent ?? "";
    expect(text).toContain("DNS primário");
    expect(text).toContain("DNS secundário");
    expect(text).toContain("fe80::1");
    expect(text).toContain("192.168.1.100");
  });
});
