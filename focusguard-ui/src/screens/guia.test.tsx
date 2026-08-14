// Testes da tela Guia — manual de configuração do DNS sinkhole: as seções de
// sistema, roteador (genérico) e diagnóstico, os guias por fabricante com a
// troca de aba, os links para manuais oficiais e o card de IP/MAC da máquina
// (usado na reserva DHCP do roteador).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import type { ApiResponse } from "@/api/types";
import { useData } from "@/context";
import { Guia } from "./Guia";

vi.mock("@/context", () => ({
  useData: vi.fn(),
}));

const useDataMock = vi.mocked(useData);

function defaultData() {
  return {
    daemonUp: true,
    status: {
      success: true,
      lan_ip: "192.168.1.100",
      lan_mac: "aa:bb:cc:dd:ee:ff",
    } as ApiResponse,
    presets: [],
    stats: null,
    refresh: vi.fn(),
  };
}

beforeEach(() => {
  useDataMock.mockReset();
  useDataMock.mockReturnValue(defaultData());
});

afterEach(() => {
  cleanup();
});

describe("Guia — manual de configuração do DNS sinkhole", () => {
  it("renderiza as seções de sistema, roteador e diagnóstico", () => {
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    expect(text).toContain("No sistema (Windows)");
    expect(text).toContain("No roteador (modem)");
    expect(text).toContain("Guias por fabricante");
    expect(text).toContain("Testar e diagnosticar");
  });

  it("manual do sistema: firewall automático, perfil Privada e porta 53", () => {
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    expect(text).toContain("abre a porta 53 no firewall automaticamente");
    expect(text).toContain("Rede privada");
    expect(text).toContain("net stop SharedAccess");
    expect(text).toContain("FocusGuard_DNS_Inbound_UDP/TCP");
  });

  it("manual do roteador: DHCP, failover e o bypass IPv6 (fe80::1)", () => {
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    expect(text).toContain("DNS primário");
    expect(text).toContain("DNS secundário");
    expect(text).toContain("fe80::1");
    expect(text).toContain("192.168.1.100");
  });

  it("lista todos os fabricantes e mostra o conteúdo do ZTE por padrão", () => {
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    for (const label of ["ZTE", "TP-Link", "Huawei", "Intelbras", "D-Link", "Asus", "Outro"]) {
      expect(text).toContain(label);
    }
    // A aba ZTE é a ativa por padrão → o caminho de menu dela aparece.
    expect(text).toContain("Rede → LAN → DHCP Server");
  });

  it("trocar de fabricante mostra o caminho de menu do fabricante selecionado", () => {
    const r = render(<Guia />);

    // O Radix Tabs ativa na aba via onMouseDown (botão 0), não no click.
    fireEvent.mouseDown(r.getByRole("tab", { name: "TP-Link" }), { button: 0 });
    expect(r.container.textContent ?? "").toContain("Address Reservation");

    fireEvent.mouseDown(r.getByRole("tab", { name: "Asus" }), { button: 0 });
    expect(r.container.textContent ?? "").toContain("Manual Assignment");
  });

  it("mostra o IP e o MAC da máquina para a reserva DHCP do roteador", () => {
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    expect(text).toContain("IP e MAC desta máquina");
    expect(text).toContain("192.168.1.100");
    expect(text).toContain("aa:bb:cc:dd:ee:ff");
    expect(text).toContain("reserva DHCP");
  });

  it("sem IP (daemon offline) mostra o aviso em vez dos valores", () => {
    useDataMock.mockReturnValue({
      daemonUp: false,
      status: null,
      presets: [],
      stats: null,
      refresh: vi.fn(),
    });
    const { container } = render(<Guia />);
    const text = container.textContent ?? "";
    expect(text).toContain(
      "O IP e o MAC aparecem quando o serviço FocusGuard está ativo",
    );
    expect(text).not.toContain("aa:bb:cc:dd:ee:ff");
  });

  it("a aba ativa mostra a screenshot do painel (img /manuais/{id}.png)", () => {
    const r = render(<Guia />);

    // Aba ZTE (ativa por padrão) → img do zte.
    expect(r.container.querySelector('img[src="/manuais/zte.png"]')).not.toBeNull();

    // Troca para TP-Link → img do tplink.
    fireEvent.mouseDown(r.getByRole("tab", { name: "TP-Link" }), { button: 0 });
    expect(r.container.querySelector('img[src="/manuais/tplink.png"]')).not.toBeNull();
  });

  it("sem arquivo de screenshot, a aba mostra o placeholder com a instrução", () => {
    const r = render(<Guia />);
    const img = r.container.querySelector('img[src="/manuais/zte.png"]') as HTMLImageElement;
    expect(img).not.toBeNull();

    // Imagem ausente (404) → onError troca para o placeholder.
    fireEvent.error(img);
    const text = r.container.textContent ?? "";
    expect(text).toContain("Screenshot do painel ainda não disponível");
    expect(text).toContain("public/manuais/zte.png");
  });

  it("cada fabricante linka para o manual/emulador oficial (aba ativa)", () => {
    const r = render(<Guia />);

    // Aba ZTE (ativa por padrão) tem o link do suporte ZTE.
    const zte = r.container.querySelector('a[href="https://www.ztedevices.com/en/support.html"]');
    expect(zte).not.toBeNull();
    expect(zte?.getAttribute("target")).toBe("_blank");
    expect(zte?.getAttribute("rel")).toContain("noopener");

    // TP-Link linka para o emulador do painel.
    fireEvent.mouseDown(r.getByRole("tab", { name: "TP-Link" }), { button: 0 });
    const tplink = r.container.querySelector('a[href="https://www.tp-link.com/us/support/emulator/"]');
    expect(tplink).not.toBeNull();

    // Intelbras linka para o portal de ajuda em português.
    fireEvent.mouseDown(r.getByRole("tab", { name: "Intelbras" }), { button: 0 });
    expect(
      r.container.querySelector('a[href="https://www.intelbras.com/pt-br/ajuda-download"]'),
    ).not.toBeNull();
  });
});
