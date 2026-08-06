import { useEffect, useState } from "react";
import {
  Download,
  KeyRound,
  Loader2,
  Plus,
  RefreshCw,
  Settings,
  ShieldCheck,
  Target,
  Trash2,
  UserRound,
  Users,
} from "lucide-react";
import { api, DaemonError, execAction } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Screen, ScreenHeader } from "@/components/screen";
import { useAuth, useData } from "@/context";
import { formatMinutes } from "@/hooks/useCountdown";
import { toast } from "@/lib/toast";
import { cn } from "@/lib/utils";
const GOALS = [
  { label: "2 h", minutes: 120 },
  { label: "4 h", minutes: 240 },
  { label: "6 h", minutes: 360 },
  { label: "8 h", minutes: 480 },
];

export function Configuracoes() {
  const { status, daemonUp, refresh } = useData();
  const { auth } = useAuth();
  const [customGoal, setCustomGoal] = useState("");
  const [busy, setBusy] = useState(false);
  const [channel, setChannel] = useState("stable");
  const [confirmUpdate, setConfirmUpdate] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [checking, setChecking] = useState(false);
  const [updateInfo, setUpdateInfo] = useState<string | null>(null);
  const [updateError, setUpdateError] = useState<string | null>(null);

  const goalNs = status?.goal ?? 0;

  const setGoal = async (minutes: number) => {
    setBusy(true);
    try {
      const resp = await api.goalSet(minutes);
      if (!resp.success) {
        toast(resp.message ?? "Falha ao definir a meta.", "err");
      } else {
        toast("Meta diária atualizada!");
        await refresh();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Erro ao definir a meta.", "err");
    } finally {
      setBusy(false);
    }
  };

  const saveCustom = () => {
    const n = Number(customGoal);
    if (!Number.isFinite(n) || n <= 0 || n > 1440) {
      toast("Meta inválida (1 a 1440 minutos).", "err");
      return;
    }
    void setGoal(Math.round(n));
  };

  // checkUpdate consulta o GitHub na hora (update-check, sem aplicar) e mostra
  // o resultado na tela — com loading enquanto espera e erro inline se falhar.
  const checkUpdate = async () => {
    setChecking(true);
    setUpdateError(null);
    setUpdateInfo(null);
    try {
      const resp = await api.updateCheck(channel);
      if (!resp.success) {
        setUpdateError(resp.message ?? "Falha ao verificar atualizações.");
        return;
      }
      setUpdateInfo(
        resp.update_available
          ? `Nova versão ${resp.update_version} disponível (atual: ${resp.current_version}).`
          : "Nenhuma atualização disponível.",
      );
      await refresh();
    } catch (e) {
      setUpdateError(
        e instanceof DaemonError ? e.message : "Erro ao verificar atualizações.",
      );
    } finally {
      setChecking(false);
    }
  };

  const applyUpdate = async () => {
    setUpdating(true);
    setUpdateError(null);
    setUpdateInfo(null);
    try {
      const res = await execAction({ action: "update", channel });
      if (res.ok) {
        // Fallback move-on-reboot: o daemon segue rodando a versão antiga e a
        // troca dos binários acontece no próximo reinício (binário travado).
        setUpdateInfo(
          res.updatePendingReboot
            ? "Atualização preparada — os binários em uso serão substituídos no próximo reinício do computador."
            : res.message || "Atualização aplicada — o daemon reinicia ao final.",
        );
      } else {
        setUpdateError(res.message || "Falha ao atualizar.");
      }
      toast(
        res.message || (res.ok ? "Atualização aplicada." : "Falha ao atualizar."),
        res.ok ? "ok" : "err",
      );
      await refresh();
    } catch (e) {
      const msg = e instanceof DaemonError ? e.message : "Erro ao atualizar.";
      setUpdateError(msg);
      toast(msg, "err");
    } finally {
      setUpdating(false);
      setConfirmUpdate(false);
    }
  };

  return (
    <Screen>
      <ScreenHeader title="Configurações" subtitle="Meta diária, atualizações e estado do sistema." />

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-5 px-5 py-5">
          <div className="flex items-center gap-2">
            <Target className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Meta diária de foco</h3>
          </div>
          <p className="-mt-3 text-sm text-muted-foreground">
            {goalNs > 0 ? `Atual: ${formatMinutes(goalNs)} por dia` : "Nenhuma meta definida ainda."}
          </p>
          <div className="flex flex-wrap gap-2">
            {GOALS.map((g) => (
              <Button
                key={g.minutes}
                type="button"
                variant={goalNs > 0 && goalNs === g.minutes * 6e10 ? "default" : "outline"}
                onClick={() => void setGoal(g.minutes)}
                disabled={busy}
                className="h-7"
              >
                {g.label}
              </Button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Input
              type="number"
              min={1}
              max={1440}
              placeholder="minutos personalizados"
              className="max-w-40"
              value={customGoal}
              onChange={(e) => setCustomGoal(e.target.value)}
            />
            <Button variant="secondary" onClick={saveCustom} disabled={busy}>
              Definir
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-5 px-5 py-5">
          <div className="flex items-center gap-2">
            <Download className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Atualizações</h3>
          </div>
          {updateError ? (
            <p
              className="-mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
              role="alert"
            >
              {updateError}
            </p>
          ) : updateInfo ? (
            <p className="-mt-3 text-sm text-muted-foreground">{updateInfo}</p>
          ) : status?.update_available ? (
            <p className="-mt-3 text-sm">
              Nova versão <strong>{status.update_version}</strong> disponível (atual:{" "}
              {status.current_version}).
            </p>
          ) : (
            <p className="-mt-3 text-sm text-muted-foreground">
              Você está na versão mais recente
              {status?.current_version ? ` (${status.current_version})` : ""}.
            </p>
          )}
          <div className="flex max-w-60 flex-col gap-2">
            <Label htmlFor="channel">Canal</Label>
            <Select value={channel} onValueChange={setChannel} disabled={checking || updating}>
              <SelectTrigger id="channel" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="stable">stable (recomendado)</SelectItem>
                <SelectItem value="beta">beta (prereleases)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={() => void checkUpdate()}
              disabled={checking || updating || !daemonUp}
            >
              <RefreshCw className={cn(checking && "animate-spin")} />
              {checking ? "Verificando…" : "Verificar"}
            </Button>
            <Button
              disabled={!status?.update_available || checking || updating}
              onClick={() => setConfirmUpdate(true)}
            >
              <Download /> Aplicar atualização
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-3 px-5 py-5">
          <div className="flex items-center gap-2">
            <ShieldCheck className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Proteção do sistema</h3>
          </div>
          {status?.protection_error ? (
            <p className="text-sm text-muted-foreground">
              Não foi possível consultar o firewall: {status.protection_error}
            </p>
          ) : (
            <dl className="divide-y">
              <Row label="Regras de firewall (FocusGuard)">
                <Badge variant="secondary">{status?.firewall_rules ?? 0}</Badge>
              </Row>
              <Row label="Proteção DoH/DoT">
                <Badge variant={status?.doh_active ? "default" : "outline"}>
                  {status?.doh_active ? "ATIVA" : "inativa"}
                </Badge>
              </Row>
            </dl>
          )}
        </CardContent>
      </Card>

      {auth && <UsersCard />}

      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-3 px-5 py-5">
          <div className="flex items-center gap-2">
            <Settings className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">Sobre</h3>
          </div>
          <dl className="divide-y">
            <Row label="Interface web">FocusGuard UI</Row>
            <Row label="Versão do sistema">{status?.current_version ?? "—"}</Row>
            <Row label="Daemon">
              <span
                className={cn(
                  "font-medium",
                  daemonUp ? "text-emerald-500" : "text-muted-foreground",
                )}
              >
                {daemonUp ? "ativo" : "offline"}
              </span>
            </Row>
          </dl>
          <p className="text-xs text-muted-foreground">
            A interface web fica em <code className="rounded bg-muted px-1">http://127.0.0.1:48902</code>{" "}
            e conversa com o daemon apenas via localhost.
          </p>
        </CardContent>
      </Card>

      <Dialog open={confirmUpdate} onOpenChange={setConfirmUpdate}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Aplicar atualização?</DialogTitle>
            <DialogDescription asChild>
              <p>
                Baixar e aplicar <strong>{status?.update_version}</strong> no canal{" "}
                <code className="rounded bg-muted px-1">{channel}</code>? O daemon atualiza os
                binários e reinicia ao final — a interface pode ficar indisponível por alguns
                instantes.
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmUpdate(false)} disabled={updating}>
              Cancelar
            </Button>
            <Button onClick={() => void applyUpdate()} disabled={updating}>
              <Download className={cn(updating && "animate-pulse")} />
              {updating ? "Atualizando…" : "Atualizar"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{children}</dd>
    </div>
  );
}

// UsersCard — gestão de contas da interface web. O admin lista, cria, remove e
// troca senhas de qualquer usuário; um usuário comum só vê a própria conta
// (a troca de senha própria também passa pelo user-set-password).
function UsersCard() {
  const { auth } = useAuth();
  const isAdmin = auth?.isAdmin === true;
  const self = auth?.username ?? "";

  const [users, setUsers] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Novo usuário
  const [addOpen, setAddOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newPass, setNewPass] = useState("");

  // Trocar senha
  const [pwTarget, setPwTarget] = useState<string | null>(null);
  const [pw1, setPw1] = useState("");
  const [pw2, setPw2] = useState("");

  // Remover
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await api.usersList();
      if (resp.success) setUsers(resp.users ?? []);
      else setError(resp.message ?? "Falha ao listar usuários.");
    } catch (e) {
      setError(e instanceof DaemonError ? e.message : "Falha ao listar usuários.");
    } finally {
      setLoading(false);
    }
  };
  // Só o admin lista usuários (user-list é 403 para o resto).
  useEffect(() => {
    if (isAdmin) void load();
  }, [isAdmin]);

  const addUser = async () => {
    const name = newName.trim().toLowerCase();
    if (!name) {
      toast("Informe um nome de usuário.", "err");
      return;
    }
    if (newPass.length < 8) {
      toast("A senha precisa de ao menos 8 caracteres.", "err");
      return;
    }
    setBusy(true);
    try {
      const resp = await api.userAdd(name, newPass);
      toast(
        resp.message ?? (resp.success ? "Usuário criado." : "Falha ao criar usuário."),
        resp.success ? "ok" : "err",
      );
      if (resp.success) {
        setAddOpen(false);
        setNewName("");
        setNewPass("");
        void load();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Falha ao criar usuário.", "err");
    } finally {
      setBusy(false);
    }
  };

  const changePassword = async () => {
    if (!pwTarget) return;
    if (pw1.length < 8) {
      toast("A senha precisa de ao menos 8 caracteres.", "err");
      return;
    }
    if (pw1 !== pw2) {
      toast("As senhas não conferem.", "err");
      return;
    }
    setBusy(true);
    try {
      const resp = await api.userSetPassword(pwTarget, pw1);
      toast(
        resp.message ?? (resp.success ? "Senha atualizada." : "Falha ao atualizar a senha."),
        resp.success ? "ok" : "err",
      );
      if (resp.success) {
        setPwTarget(null);
        setPw1("");
        setPw2("");
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Falha ao atualizar a senha.", "err");
    } finally {
      setBusy(false);
    }
  };

  const removeUser = async () => {
    if (!removeTarget) return;
    setBusy(true);
    try {
      const resp = await api.userRemove(removeTarget);
      toast(
        resp.message ?? (resp.success ? "Usuário removido." : "Falha ao remover usuário."),
        resp.success ? "ok" : "err",
      );
      if (resp.success) {
        setRemoveTarget(null);
        void load();
      }
    } catch (e) {
      toast(e instanceof DaemonError ? e.message : "Falha ao remover usuário.", "err");
    } finally {
      setBusy(false);
    }
  };

  const openPassword = (target: string) => {
    setPwTarget(target);
    setPw1("");
    setPw2("");
  };

  return (
    <>
      <Card className="max-w-2xl">
        <CardContent className="flex flex-col gap-3 px-5 py-5">
          <div className="flex items-center gap-2">
            <Users className="size-4 text-muted-foreground" />
            <h3 className="font-heading text-base font-semibold">
              {isAdmin ? "Usuários" : "Minha conta"}
            </h3>
            {isAdmin && (
              <Button
                size="sm"
                className="ml-auto"
                onClick={() => setAddOpen(true)}
                disabled={busy}
              >
                <Plus /> Novo usuário
              </Button>
            )}
          </div>

          {isAdmin ? (
            loading ? (
              <p className="text-sm text-muted-foreground">Carregando usuários…</p>
            ) : error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : (
              <ul className="divide-y">
                {users.map((u) => (
                  <li key={u} className="flex items-center gap-3 py-2.5 text-sm">
                    <UserRound className="size-4 shrink-0 text-muted-foreground" />
                    <span className="font-medium">{u}</span>
                    {u === "admin" && <Badge variant="secondary">admin</Badge>}
                    {u === self && <Badge variant="outline">você</Badge>}
                    <div className="ml-auto flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={`Trocar senha de ${u}`}
                        title="Trocar senha"
                        onClick={() => openPassword(u)}
                        disabled={busy}
                      >
                        <KeyRound />
                      </Button>
                      {u !== "admin" && (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Remover ${u}`}
                          title="Remover usuário"
                          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                          onClick={() => setRemoveTarget(u)}
                          disabled={busy}
                        >
                          <Trash2 />
                        </Button>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )
          ) : (
            <div className="flex items-center gap-3 py-1 text-sm">
              <UserRound className="size-4 shrink-0 text-muted-foreground" />
              <span className="font-medium">{self}</span>
              <Badge variant="outline">você</Badge>
              <Button
                size="sm"
                variant="secondary"
                className="ml-auto"
                onClick={() => openPassword(self)}
                disabled={busy}
              >
                <KeyRound /> Trocar minha senha
              </Button>
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            {isAdmin
              ? "O usuário admin é único e não pode ser removido."
              : "Altere sua senha de acesso à interface web."}
          </p>
        </CardContent>
      </Card>

      {/* Novo usuário */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Novo usuário</DialogTitle>
            <DialogDescription asChild>
              <p>
                Crie uma conta para outra pessoa acessar a interface. A senha
                precisa de ao menos 8 caracteres.
              </p>
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new-username">Usuário</Label>
              <Input
                id="new-username"
                autoComplete="off"
                placeholder="ex.: maria"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                disabled={busy}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="new-password">Senha</Label>
              <Input
                id="new-password"
                type="password"
                autoComplete="new-password"
                placeholder="mínimo 8 caracteres"
                value={newPass}
                onChange={(e) => setNewPass(e.target.value)}
                disabled={busy}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setAddOpen(false)} disabled={busy}>
              Cancelar
            </Button>
            <Button onClick={() => void addUser()} disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <Plus />} Criar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Trocar senha */}
      <Dialog
        open={pwTarget !== null}
        onOpenChange={(o) => {
          if (!o) setPwTarget(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {pwTarget === self ? "Trocar minha senha" : `Trocar senha de ${pwTarget}`}
            </DialogTitle>
            <DialogDescription asChild>
              <p>Defina uma nova senha (mínimo 8 caracteres).</p>
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="pw1">Nova senha</Label>
              <Input
                id="pw1"
                type="password"
                autoComplete="new-password"
                value={pw1}
                onChange={(e) => setPw1(e.target.value)}
                disabled={busy}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="pw2">Confirmar nova senha</Label>
              <Input
                id="pw2"
                type="password"
                autoComplete="new-password"
                value={pw2}
                onChange={(e) => setPw2(e.target.value)}
                disabled={busy}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setPwTarget(null)} disabled={busy}>
              Cancelar
            </Button>
            <Button onClick={() => void changePassword()} disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <KeyRound />} Salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Remover usuário */}
      <Dialog
        open={removeTarget !== null}
        onOpenChange={(o) => {
          if (!o) setRemoveTarget(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remover {removeTarget}?</DialogTitle>
            <DialogDescription asChild>
              <p>O usuário perderá o acesso à interface web imediatamente.</p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRemoveTarget(null)} disabled={busy}>
              Cancelar
            </Button>
            <Button variant="destructive" onClick={() => void removeUser()} disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <Trash2 />} Remover
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
