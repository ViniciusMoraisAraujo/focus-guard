import { useEffect, useState } from "react";
import { Ban, Plus, Trash2 } from "lucide-react";
import { api, execAction } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { EmptyCard, Screen, ScreenHeader, SectionTitle } from "@/components/screen";
import { useApp } from "@/context";

export function Apps() {
  const { daemonUp, toast } = useApp();
  const [apps, setApps] = useState<string[] | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    api
      .appsList()
      .then((r) => setApps(r.success ? (r.apps ?? []) : []))
      .catch(() => setApps([]));
  };

  useEffect(() => {
    load();
  }, []);

  const add = async () => {
    const proc = name.trim();
    if (!proc) {
      toast("Informe o nome do processo (ex: spotify.exe).", "err");
      return;
    }
    setBusy(true);
    try {
      const res = await execAction({ action: "apps-add", app_name: proc });
      toast(res.message || (res.ok ? "Processo adicionado!" : "Falha ao adicionar."), res.ok ? "ok" : "err");
      if (res.ok) {
        load();
        setName("");
      }
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao adicionar.", "err");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (proc: string) => {
    setBusy(true);
    try {
      const res = await execAction({ action: "apps-remove", app_name: proc });
      toast(res.message || (res.ok ? "Processo removido." : "Falha ao remover."), res.ok ? "ok" : "err");
      if (res.ok) load();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Erro ao remover.", "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScreenHeader
        title="Apps (denylist)"
        subtitle="Processos encerrados enquanto uma sessão de foco estiver ativa."
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para gerenciar os processos.</p>
        </EmptyCard>
      ) : (
        <>
          <Card className="max-w-2xl">
            <CardContent className="flex flex-col gap-4 px-5 py-5">
              <h3 className="font-heading text-base font-semibold">Adicionar processo</h3>
              <div className="flex gap-2">
                <Input
                  type="text"
                  placeholder="ex: spotify.exe"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && void add()}
                />
                <Button onClick={() => void add()} disabled={busy}>
                  <Plus /> {busy ? "Adicionando…" : "Adicionar"}
                </Button>
              </div>
            </CardContent>
          </Card>

          <SectionTitle count={apps?.length ?? 0}>Processos da denylist</SectionTitle>
          {apps === null ? (
            <div className="flex flex-col gap-3" aria-label="Carregando processos">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-12 w-full rounded-xl" />
              ))}
            </div>
          ) : apps.length === 0 ? (
            <EmptyCard>
              <p>Nenhum processo na denylist — o guard está inativo.</p>
            </EmptyCard>
          ) : (
            <div className="flex flex-col gap-3">
              {apps.map((a) => (
                <Card
                  key={a}
                  size="sm"
                  className="transition-colors hover:ring-foreground/20"
                >
                  <CardContent className="flex items-center justify-between gap-3 px-4 py-3">
                    <span className="flex items-center gap-2 text-sm font-semibold">
                      <Ban className="size-4 text-muted-foreground" />
                      <code className="rounded bg-muted px-1.5 py-0.5">{a}</code>
                    </span>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Remover ${a} da denylist`}
                          onClick={() => void remove(a)}
                        >
                          <Trash2 />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Remover processo</TooltipContent>
                    </Tooltip>
                  </CardContent>
                </Card>
              ))}
            </div>
          )}
        </>
      )}
    </Screen>
  );
}
