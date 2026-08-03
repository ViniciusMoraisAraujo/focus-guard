import { useEffect, useMemo, useState } from "react";

/** useCountdown retorna os ms restantes até target (RFC3339), tick a cada segundo. */
export function useCountdown(target: string | null | undefined): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!target) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [target]);

  return useMemo(() => {
    if (!target) return 0;
    return Math.max(0, new Date(target).getTime() - now);
  }, [target, now]);
}

/** formatMs renderiza mm:ss ou h:mm:ss. */
export function formatMs(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const mm = String(m).padStart(2, "0");
  const ss = String(s).padStart(2, "0");
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

/** formatMinutes converte nanosegundos (time.Duration do Go) em texto legível. */
export function formatMinutes(ns: number): string {
  const min = Math.round(ns / 6e10);
  if (min <= 0) return "0 min";
  const h = Math.floor(min / 60);
  const m = min % 60;
  if (h === 0) return `${m} min`;
  if (m === 0) return `${h} h`;
  return `${h} h ${m} min`;
}

/** formatClock renderiza um timestamp RFC3339 como HH:MM. */
export function formatClock(rfc3339: string): string {
  const d = new Date(rfc3339);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" });
}
