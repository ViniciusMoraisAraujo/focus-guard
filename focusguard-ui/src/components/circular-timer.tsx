import { useId } from "react";
import { formatMs } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

/**
 * CircularTimer renderiza um anel de progresso SVG com o tempo restante no
 * centro. Usado no Pomodoro como o contador visual da fase atual.
 *
 * Polimento F4: traço com gradiente (foco = esmeralda, descanso = céu), glow
 * sutil, pulso quando a fase está prestes a terminar (< 15% restante) e dots
 * de progresso do ciclo (cycle/cycles).
 */
export function CircularTimer({
  ms,
  totalMs,
  tone = "focus",
  label,
  cycle,
  cycles,
  className,
}: {
  ms: number;
  totalMs: number;
  tone?: "focus" | "rest";
  label?: string;
  /** Ciclo atual (1-based) e total, para os dots de progresso. */
  cycle?: number;
  cycles?: number;
  className?: string;
}) {
  const rawId = useId();
  // useId gera ":r0:" — sanitiza para um id de SVG seguro (url(#id)).
  const gradId = `fg-grad-${rawId.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const R = 62;
  const C = 2 * Math.PI * R;
  const frac = totalMs > 0 ? Math.min(1, Math.max(0, ms / totalMs)) : 1;
  const dash = C * (1 - frac);
  const nearlyDone = frac < 0.15 && frac > 0;
  const from = tone === "rest" ? "#38bdf8" : "#34d399";
  const to = tone === "rest" ? "#0284c7" : "#059669";
  const dots = cycles && cycles > 0 ? Math.max(1, Math.min(cycles, 12)) : 0;
  // Progresso proporcional: com cycles > dots (máx 24 vs 12 dots), os dots
  // enchem de forma fiel ao avanço real da sessão.
  const done =
    dots > 0 && cycles && cycles > 0
      ? Math.round((Math.min(Math.max(cycle ?? 0, 0), cycles) / cycles) * dots)
      : 0;

  return (
    <div className={cn("relative size-40 shrink-0", className)}>
      <svg
        viewBox="0 0 160 160"
        className={cn(
          "size-full -rotate-90",
          nearlyDone && "animate-pulse",
          !nearlyDone &&
            (tone === "rest"
              ? "drop-shadow-[0_0_10px_rgba(56,189,248,0.3)]"
              : "drop-shadow-[0_0_10px_rgba(16,185,129,0.3)]"),
        )}
      >
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor={from} />
            <stop offset="100%" stopColor={to} />
          </linearGradient>
        </defs>
        <circle cx="80" cy="80" r={R} fill="none" strokeWidth="10" className="stroke-muted" />
        <circle
          cx="80"
          cy="80"
          r={R}
          fill="none"
          strokeWidth="10"
          strokeLinecap="round"
          strokeDasharray={C}
          strokeDashoffset={dash}
          stroke={`url(#${gradId})`}
          className="transition-[stroke-dashoffset] duration-1000 ease-linear"
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center gap-0.5">
        <span
          className={cn(
            "font-mono text-3xl font-bold tabular-nums",
            tone === "rest" ? "text-sky-500" : "text-primary",
            nearlyDone && "text-amber-500",
          )}
        >
          {formatMs(ms)}
        </span>
        {label && (
          <span className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {label}
          </span>
        )}
        {dots > 0 && (
          <span className="mt-0.5 flex items-center gap-1" aria-label={`ciclo ${done} de ${dots}`}>
            {Array.from({ length: dots }, (_, i) => (
              <span
                key={i}
                className={cn(
                  "size-1.5 rounded-full transition-colors duration-300",
                  i < done
                    ? tone === "rest"
                      ? "bg-sky-500"
                      : "bg-emerald-500"
                    : "bg-muted-foreground/25",
                )}
              />
            ))}
          </span>
        )}
      </div>
    </div>
  );
}
