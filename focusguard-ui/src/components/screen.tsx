import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function Screen({ children, className }: { children: ReactNode; className?: string }) {
  return <section className={cn("flex flex-col gap-5", className)}>{children}</section>;
}

export function ScreenHeader({
  title,
  subtitle,
  actions,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 className="font-heading text-2xl font-semibold tracking-tight">{title}</h2>
        {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </header>
  );
}

export function SectionTitle({ children, count }: { children: ReactNode; count?: number }) {
  return (
    <div className="flex items-baseline gap-2 pt-1">
      <h3 className="font-heading text-sm font-semibold text-muted-foreground">{children}</h3>
      {count !== undefined && <span className="text-sm text-muted-foreground">{count}</span>}
    </div>
  );
}

export function EmptyCard({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "rounded-xl border border-dashed bg-card p-8 text-center text-sm text-muted-foreground",
        className,
      )}
    >
      {children}
    </div>
  );
}
