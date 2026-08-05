import { useState, type FormEvent } from "react";
import {
  ArrowRight,
  Eye,
  EyeOff,
  Loader2,
  Lock,
  ShieldCheck,
  TriangleAlert,
  User,
  WifiOff,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ThemeToggle } from "@/components/theme-toggle";
import { useApp } from "@/context";
import { cn } from "@/lib/utils";

// Login.tsx — porta de entrada da UI. O focusguard-web entrega a SPA sem
// exigir sessão (o gate vive aqui): enquanto não houver cookie fg_session
// válido, esta tela é o único conteúdo renderizado.
export function LoginScreen() {
  const { daemonUp, login } = useApp();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    if (!username.trim() || !password) {
      setError("Informe usuário e senha.");
      return;
    }
    setBusy(true);
    setError(null);
    const res = await login(username.trim(), password);
    if (!res.ok) {
      // Erro inline (fixo na tela) — sem toast duplicado.
      setError(res.message);
    }
    // sucesso: o AppProvider troca auth e o gate renderiza a app sozinho.
    setBusy(false);
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background px-4 py-10">
      {/* Fundo decorativo: pontilhado + brilhos suaves */}
      <div
        aria-hidden="true"
        className="absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,color-mix(in_oklch,var(--foreground)_10%,transparent)_1px,transparent_0)] bg-[size:26px_26px] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_45%,black,transparent)]"
      />
      <div
        aria-hidden="true"
        className="absolute -top-32 -left-24 size-96 rounded-full bg-primary/10 blur-3xl"
      />
      <div
        aria-hidden="true"
        className="absolute -right-24 -bottom-32 size-96 rounded-full bg-primary/10 blur-3xl"
      />

      <ThemeToggle className="absolute top-4 right-4" />

      <div className="animate-in fade-in zoom-in-95 relative w-full max-w-sm duration-300">
        <div className="rounded-2xl bg-card p-8 shadow-2xl ring-1 ring-foreground/10">
          <div className="flex flex-col items-center gap-3 text-center">
            <div className="grid size-12 place-items-center rounded-xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
              <ShieldCheck className="size-6" />
            </div>
            <div>
              <h1 className="font-heading text-xl font-semibold">FocusGuard</h1>
              <p className="mt-0.5 text-sm text-muted-foreground">
                Painel de controle — acesso protegido
              </p>
            </div>
          </div>

          {daemonUp === false && (
            <div
              role="alert"
              className="mt-6 flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-600 dark:text-amber-400"
            >
              <WifiOff className="mt-0.5 size-4 shrink-0" />
              <span>
                O daemon está desligado — o login pode falhar até ele voltar
                (inicie o serviço FocusGuard).
              </span>
            </div>
          )}

          <form onSubmit={(e) => void submit(e)} className="mt-6 flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-username">Usuário</Label>
              <div className="relative">
                <User
                  aria-hidden="true"
                  className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
                />
                <Input
                  id="login-username"
                  autoComplete="username"
                  autoFocus
                  placeholder="ex.: admin"
                  className="pl-8"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  disabled={busy}
                />
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-password">Senha</Label>
              <div className="relative">
                <Lock
                  aria-hidden="true"
                  className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
                />
                <Input
                  id="login-password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  placeholder="••••••••"
                  className="pr-9 pl-8"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  disabled={busy}
                />
                <button
                  type="button"
                  aria-label={showPassword ? "Ocultar senha" : "Mostrar senha"}
                  onClick={() => setShowPassword((v) => !v)}
                  className="absolute top-1/2 right-2 grid size-6 -translate-y-1/2 place-items-center rounded text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                >
                  {showPassword ? (
                    <EyeOff className="size-4" />
                  ) : (
                    <Eye className="size-4" />
                  )}
                </button>
              </div>
            </div>

            {error && (
              <div
                role="alert"
                className="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2.5 text-sm text-destructive"
              >
                <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                <span className="break-words">{error}</span>
              </div>
            )}

            <Button type="submit" size="lg" className="w-full" disabled={busy}>
              {busy ? (
                <Loader2 className="animate-spin" />
              ) : (
                <ArrowRight data-icon="inline-end" />
              )}
              {busy ? "Entrando…" : "Entrar"}
            </Button>
          </form>

          <p
            className={cn(
              "mt-6 text-center text-xs text-muted-foreground",
              error && "mt-4",
            )}
          >
            Sessões expiram após 12 horas. Este painel roda apenas neste
            computador.
          </p>
        </div>
      </div>
    </div>
  );
}
