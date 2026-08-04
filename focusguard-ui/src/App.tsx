import { useState } from "react";
import {
  Ban,
  BarChart3,
  CalendarDays,
  Folder,
  History,
  Lock,
  Menu,
  Settings,
  Shield,
  ShieldCheck,
  Siren,
  Timer,
  TriangleAlert,
} from "lucide-react";
import { Agenda } from "./screens/Agenda";
import { Apps } from "./screens/Apps";
import { Bloquear } from "./screens/Bloquear";
import { Configuracoes } from "./screens/Configuracoes";
import { Dashboard } from "./screens/Dashboard";
import { Estatisticas } from "./screens/Estatisticas";
import { Panico } from "./screens/Panico";
import { Pomodoro } from "./screens/Pomodoro";
import { Presets } from "./screens/Presets";
import { Seguranca } from "./screens/Seguranca";
import { Badge } from "./components/ui/badge";
import { Button } from "./components/ui/button";
import { Separator } from "./components/ui/separator";
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetTrigger,
} from "./components/ui/sheet";
import { TooltipProvider } from "./components/ui/tooltip";
import { Toaster } from "./components/ui/sonner";
import { ThemeToggle } from "./components/theme-toggle";
import { AppProvider, useApp, type Screen } from "./context";
import { cn } from "./lib/utils";

const NAV: { id: Screen; label: string; icon: typeof Shield }[] = [
  { id: "dashboard", label: "Painel", icon: Shield },
  { id: "bloquear", label: "Bloquear", icon: Lock },
  { id: "panico", label: "Modo pânico", icon: Siren },
  { id: "pomodoro", label: "Pomodoro", icon: Timer },
  { id: "agenda", label: "Agenda", icon: CalendarDays },
  { id: "apps", label: "Apps", icon: Ban },
  { id: "presets", label: "Presets", icon: Folder },
  { id: "stats", label: "Estatísticas", icon: BarChart3 },
  { id: "seguranca", label: "Segurança", icon: History },
  { id: "config", label: "Configurações", icon: Settings },
];

export function App() {
  return (
    <AppProvider>
      <TooltipProvider delayDuration={400}>
        <Shell />
        <Toaster position="bottom-right" richColors closeButton />
      </TooltipProvider>
    </AppProvider>
  );
}

function Shell() {
  const [screen, setScreen] = useState<Screen>("dashboard");
  const { daemonUp } = useApp();

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      {/* Sidebar fixa (desktop) */}
      <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground lg:flex">
        <SidebarContent screen={screen} onNavigate={setScreen} />
      </aside>

      {/* Header mobile: hambúrguer + logo + status */}
      <MobileHeader screen={screen} onNavigate={setScreen} />

      <main className="min-w-0 flex-1 px-4 py-6 pb-24 sm:px-6 lg:px-8 lg:py-8 xl:px-12">
        {daemonUp === false && (
          <div
            role="alert"
            className="mb-6 flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive"
          >
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            <span>
              O daemon FocusGuard está desligado. As ações ficam indisponíveis
              até ele iniciar (execute{" "}
              <code className="rounded bg-muted px-1">focusguard install</code>{" "}
              ou inicie o serviço).
            </span>
          </div>
        )}
        <div key={screen} className="animate-in fade-in duration-200">
          {screen === "dashboard" && <Dashboard onNavigate={setScreen} />}
          {screen === "bloquear" && <Bloquear />}
          {screen === "panico" && <Panico />}
          {screen === "pomodoro" && <Pomodoro />}
          {screen === "agenda" && <Agenda />}
          {screen === "apps" && <Apps />}
          {screen === "presets" && <Presets />}
          {screen === "stats" && <Estatisticas />}
          {screen === "seguranca" && <Seguranca />}
          {screen === "config" && <Configuracoes />}
        </div>
      </main>
    </div>
  );
}

/** Conteúdo compartilhado entre a sidebar fixa e o Sheet mobile. */
function SidebarContent({
  screen,
  onNavigate,
}: {
  screen: Screen;
  onNavigate: (s: Screen) => void;
}) {
  const { status } = useApp();

  return (
    <>
      <div className="flex items-center gap-3 px-5 pt-5 pb-6">
        <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary text-primary-foreground">
          <ShieldCheck className="size-5" />
        </div>
        <div>
          <div className="font-heading text-[15px] leading-tight font-semibold">
            FocusGuard
          </div>
          <div className="text-xs text-muted-foreground">
            Bloqueio de distrações
          </div>
        </div>
      </div>

      <Separator className="mx-4" />

      <nav
        className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
        aria-label="Navegação principal"
      >
        {NAV.map((n) => {
          const Icon = n.icon;
          const active = screen === n.id;
          return (
            <Button
              key={n.id}
              variant="ghost"
              type="button"
              aria-current={active ? "page" : undefined}
              onClick={() => onNavigate(n.id)}
              className={cn(
                "h-9 justify-start gap-3 px-3",
                active &&
                  "bg-sidebar-accent text-sidebar-accent-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
              )}
            >
              <Icon
                className={cn(
                  "size-4",
                  active ? "text-primary" : "text-muted-foreground",
                )}
              />
              {n.label}
            </Button>
          );
        })}
      </nav>

      <div className="border-t border-sidebar-border px-5 py-4">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Badge variant="secondary" className="font-mono">
            {status?.current_version ? `v${status.current_version}` : "—"}
          </Badge>
          <DaemonStatus />
          <ThemeToggle className="ml-auto" />
        </div>
      </div>
    </>
  );
}

/** Barra superior no mobile: abre a navegação em um Sheet lateral. */
function MobileHeader({
  screen,
  onNavigate,
}: {
  screen: Screen;
  onNavigate: (s: Screen) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b border-sidebar-border bg-sidebar px-4 text-sidebar-foreground lg:hidden">
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Abrir menu de navegação"
          >
            <Menu />
          </Button>
        </SheetTrigger>
        <SheetContent
          side="left"
          className="w-72 border-r border-sidebar-border bg-sidebar text-sidebar-foreground"
        >
          <SheetTitle className="sr-only">Navegação</SheetTitle>
          <SidebarContent
            screen={screen}
            onNavigate={(s) => {
              onNavigate(s);
              setOpen(false);
            }}
          />
        </SheetContent>
      </Sheet>

      <div className="flex items-center gap-2">
        <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground">
          <ShieldCheck className="size-4" />
        </div>
        <span className="font-heading text-sm font-semibold">FocusGuard</span>
      </div>

      <div className="ml-auto flex items-center gap-1">
        <ThemeToggle />
        <DaemonStatus pill />
      </div>
    </header>
  );
}

/** Indicador do estado do daemon: pontinho + rótulo (sidebar) ou pill (mobile). */
function DaemonStatus({
  pill = false,
  className,
}: {
  pill?: boolean;
  className?: string;
}) {
  const { daemonUp } = useApp();
  const label = daemonUp
    ? "daemon ativo"
    : daemonUp === null
      ? "verificando…"
      : "daemon offline";
  const dot = cn(
    "size-2 shrink-0 rounded-full",
    daemonUp === true && "bg-emerald-500",
    daemonUp === false && "bg-destructive",
    daemonUp === null && "animate-pulse bg-muted-foreground/50",
  );

  if (pill) {
    return (
      <span
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs",
          daemonUp === true && "border-emerald-500/30 bg-emerald-500/10 text-emerald-500",
          daemonUp === false && "border-destructive/30 bg-destructive/10 text-destructive",
          daemonUp === null && "text-muted-foreground",
          className,
        )}
      >
        <span className={dot} aria-hidden="true" />
        {label}
      </span>
    );
  }

  return (
    <span className={cn("ml-auto inline-flex items-center gap-2", className)}>
      <span
        className={cn(
          "size-2 shrink-0 rounded-full",
          daemonUp === true && "bg-emerald-500",
          daemonUp === false && "bg-destructive",
          daemonUp === null && "bg-muted-foreground/40",
        )}
        aria-hidden="true"
      />
      {label}
    </span>
  );
}
