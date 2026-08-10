// Testes de componente da WeeklyGrid (bug-hunt Etapa 6 — critério de saída:
// "grade overnight"). Congelam o posicionamento das janelas recorrentes:
// janela overnight (fim após meia-noite) vira DOIS segmentos no mesmo dia,
// regras sobrepostas são empilhadas em lanes, e regras fora dos dias/falsas
// não ocupam a grade.
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import type { ScheduleRule } from "@/api/types";
import { WeeklyGrid } from "./weekly-grid";

afterEach(cleanup);

function rule(partial: Partial<ScheduleRule>): ScheduleRule {
  return {
    id: "r1",
    preset: "social",
    days: [1],
    start: "08:00",
    end: "12:00",
    enabled: true,
    ...partial,
  };
}

/** Títulos dos blocos de regra renderizados (title = "label · HH:MM–HH:MM"). */
function blockTitles(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll<HTMLElement>("[title]"))
    .map((el) => el.title)
    .filter((t) => t.includes("·"));
}

function blocksFor(container: HTMLElement, label: string): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>("[title]")).filter((el) =>
    el.title.startsWith(`${label} ·`),
  );
}

describe("WeeklyGrid — janelas da grade semanal", () => {
  it("janela normal (start/end, fim depois do início) vira UM segmento", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({ id: "a", label: "Estudo", days: [1], start: "08:00", end: "17:00" }),
        ]}
      />,
    );

    expect(blockTitles(container)).toEqual(["Estudo · 08:00–17:00"]);
  });

  it("janela overnight (22:00–06:00) vira DOIS segmentos no mesmo dia", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({ id: "a", label: "Estudo", days: [1], start: "22:00", end: "06:00" }),
        ]}
      />,
    );

    // A grade ordena por seg.start, então o segmento 00:00 vem antes no DOM —
    // a asserção é por CONJUNTO: ambos os segmentos existem.
    expect(new Set(blockTitles(container))).toEqual(
      new Set(["Estudo · 22:00–24:00", "Estudo · 00:00–06:00"]),
    );
  });

  it("janela overnight via windows (22:00-06:00) também vira dois segmentos", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({
            id: "a",
            label: "Estudo",
            days: [1],
            start: "",
            end: "",
            windows: ["22:00-06:00"],
          }),
        ]}
      />,
    );

    expect(new Set(blockTitles(container))).toEqual(
      new Set(["Estudo · 22:00–24:00", "Estudo · 00:00–06:00"]),
    );
  });

  it("windows múltiplas viram vários segmentos (uma por janela)", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({
            id: "a",
            label: "Estudo",
            days: [1, 3],
            start: "",
            end: "",
            windows: ["08:00-12:00", "14:00-18:00"],
          }),
        ]}
      />,
    );

    // Cada dia com a regra recebe os 2 segmentos: Seg (1) e Qua (3).
    const titles = blockTitles(container);
    expect(titles.filter((t) => t === "Estudo · 08:00–12:00")).toHaveLength(2);
    expect(titles.filter((t) => t === "Estudo · 14:00–18:00")).toHaveLength(2);
  });

  it("regra não renderiza blocos em dias fora de days[]", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[rule({ id: "a", label: "Estudo", days: [2] })]}
      />,
    );

    // days=[2] (terça) — exatamente 1 bloco na grade inteira.
    expect(blockTitles(container)).toHaveLength(1);
  });
});

describe("WeeklyGrid — lanes (sobreposição)", () => {
  it("regras sobrepostas no mesmo dia são empilhadas lado a lado (lanes)", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({ id: "a", label: "Matutino", days: [1], start: "08:00", end: "10:00" }),
          rule({ id: "b", label: "Tarde", days: [1], start: "09:00", end: "11:00" }),
        ]}
      />,
    );

    const [matutino] = blocksFor(container, "Matutino");
    const [tarde] = blocksFor(container, "Tarde");
    expect(matutino).toBeDefined();
    expect(tarde).toBeDefined();
    // Duas lanes: cada bloco ocupa metade da coluna.
    expect(matutino.style.left).toBe("0%");
    expect(matutino.style.width).toBe("50%");
    expect(tarde.style.left).toBe("50%");
    expect(tarde.style.width).toBe("50%");
  });

  it("regras em horários disjuntos ocupam a largura total da coluna", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[
          rule({ id: "a", label: "Matutino", days: [1], start: "08:00", end: "09:00" }),
          rule({ id: "b", label: "Noite", days: [1], start: "20:00", end: "21:00" }),
        ]}
      />,
    );

    const [matutino] = blocksFor(container, "Matutino");
    const [noite] = blocksFor(container, "Noite");
    expect(matutino.style.left).toBe("0%");
    expect(matutino.style.width).toBe("100%");
    expect(noite.style.left).toBe("0%");
    expect(noite.style.width).toBe("100%");
  });
});

describe("WeeklyGrid — estado da regra", () => {
  it("regra desativada fica com opacidade reduzida (opacity-40)", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[rule({ id: "a", label: "Estudo", days: [1], enabled: false })]}
      />,
    );

    const [bloco] = blocksFor(container, "Estudo");
    expect(bloco).toBeDefined();
    expect(bloco.className).toContain("opacity-40");
  });

  it("legenda mostra o nome de cada regra e 'agora'", () => {
    const { container } = render(
      <WeeklyGrid
        rules={[rule({ id: "a", label: "Estudo", days: [1] })]}
      />,
    );

    // Escopado à estrutura da LEGENDA (swatch size-2.5 + linha h-px w-4): um
    // query em todos os spans seria vacuo — os blocos da grade também contêm
    // o texto da regra e o marcador "agora" do dia atual.
    const swatch = Array.from(
      container.querySelectorAll<HTMLElement>("[class*='size-2.5']"),
    ).find((s) => s.parentElement?.textContent?.includes("Estudo"));
    expect(swatch).toBeDefined();
    expect(swatch?.parentElement?.textContent).toContain("Estudo");

    const agoraLine = container.querySelector<HTMLElement>(
      "[class*='h-px'][class*='w-4'][class*='bg-destructive']",
    );
    expect(agoraLine?.parentElement?.textContent).toContain("agora");
  });
});
