import { formatMs } from "@/hooks/useCountdown";
import { cn } from "@/lib/utils";

/**
 * CircularTimer renderiza um anel de progresso SVG com o tempo restante no
 * centro. Usado no Pomodoro como o contador visual da fase atual.
 */
export function CircularTimer({
  ms,
  totalMs,
  tone = "focus",
  label,
  className,
}: {
  ms: number;
  totalMs: number;
  tone?: "focus" | "rest";
  label?: string;
  className?: string;
}) {
  const R = 62;
  const C = 2 * Math.PI * R;
  const frac = totalMs > 0 ? Math.min(1, Math.max(0, ms / totalMs)) : 1;
  const dash = C * (1 - frac);

  return (
    <div className={cn("relative size-40 shrink-0", className)}>
      <svg viewBox="0 0 160 160" className="size-full -rotate-90">
        <circle
          cx="80"
          cy="80"
          r={R}
          fill="none"
          strokeWidth="10"
          className="stroke-muted"
        />
        <circle
          cx="80"
          cy="80"
          r={R}
          fill="none"
          strokeWidth="10"
          strokeLinecap="round"
          strokeDasharray={C}
          strokeDashoffset={dash}
          className={cn(
            "transition-[stroke-dashoffset] duration-1000 ease-linear",
            tone === "rest" ? "stroke-sky-500" : "stroke-emerald-500",
          )}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center gap-0.5">
        <span
          className={cn(
            "font-mono text-3xl font-bold tabular-nums",
            tone === "rest" ? "text-sky-500" : "text-primary",
          )}
        >
          {formatMs(ms)}
        </span>
        {label && (
          <span className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {label}
          </span>
        )}
      </div>
    </div>
  );
}
