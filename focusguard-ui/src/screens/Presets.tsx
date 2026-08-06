import { useState } from "react";
import { Folder, FolderPlus, Trash2 } from "lucide-react";
import type { Preset } from "@/api/types";
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { EmptyCard, Screen, ScreenHeader, SectionTitle } from "@/components/screen";
import { useData } from "@/context";
import { useAction } from "@/hooks/use-action";
import { toast } from "@/lib/toast";

export function Presets() {
  const { presets, refresh, daemonUp } = useData();
  const { busy, run } = useAction();

  const [name, setName] = useState("");
  const [label, setLabel] = useState("");
  const [domains, setDomains] = useState("");
  const [toRemove, setToRemove] = useState<Preset | null>(null);

  const add = async () => {
    const n = name.trim().toLowerCase().replace(/\s+/g, "-");
    const doms = domains
      .split(",")
      .map((d) => d.trim().toLowerCase())
      .filter(Boolean);
    if (!n || doms.length === 0) {
      toast("Informe o nome e ao menos um domínio.", "err");
      return;
    }
    const res = await run(
      {
        action: "preset-add",
        preset_name: n,
        preset_label: label.trim() || name.trim(),
        preset_domains: doms,
      },
      { success: "Preset criado!", error: "Falha ao criar." },
    );
    if (res.ok) {
      await refresh();
      setName("");
      setLabel("");
      setDomains("");
    }
  };

  const remove = async () => {
    if (!toRemove) return;
    const res = await run(
      { action: "preset-remove", preset_name: toRemove.name },
      { success: "Preset removido.", error: "Presets embutidos não podem ser removidos." },
    );
    if (res.ok) await refresh();
    setToRemove(null);
  };

  return (
    <Screen>
      <ScreenHeader
        title="Presets"
        subtitle="Categorias de domínios para bloqueio e pomodoro."
      />

      {daemonUp === false ? (
        <EmptyCard>
          <p>O daemon está desligado — inicie o serviço para gerenciar os presets.</p>
        </EmptyCard>
      ) : (
        <>
          <Card className="max-w-2xl">
            <CardContent className="flex flex-col gap-5 px-5 py-5">
              <h3 className="font-heading text-base font-semibold">Criar preset personalizado</h3>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="preset-name">Nome</Label>
                  <Input
                    id="preset-name"
                    type="text"
                    placeholder="ex: estudos"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="preset-label">Rótulo (opcional)</Label>
                  <Input
                    id="preset-label"
                    type="text"
                    placeholder="ex: Estudos profundos"
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                  />
                </div>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="preset-domains">Domínios (separados por vírgula)</Label>
                <Input
                  id="preset-domains"
                  type="text"
                  placeholder="ex: reddit.com, news.ycombinator.com"
                  value={domains}
                  onChange={(e) => setDomains(e.target.value)}
                />
              </div>
              <Button onClick={() => void add()} disabled={busy} size="lg" className="w-full">
                <FolderPlus /> {busy ? "Criando…" : "Criar preset"}
              </Button>
            </CardContent>
          </Card>

          <SectionTitle count={presets.length}>Catálogo</SectionTitle>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {presets.map((p) => (
              <Card key={p.name} className="gap-2">
                <CardContent className="flex flex-col gap-3 px-4 py-4">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <Folder className="size-4 text-muted-foreground" />
                      <div>
                        <div className="text-sm font-semibold">{p.label}</div>
                        <code className="text-xs text-muted-foreground">{p.name}</code>
                      </div>
                    </div>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label="Remover preset"
                          onClick={() => setToRemove(p)}
                        >
                          <Trash2 />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>Remover preset</TooltipContent>
                    </Tooltip>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {p.domains.map((d) => (
                      <span
                        key={d}
                        className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                      >
                        {d}
                      </span>
                    ))}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </>
      )}

      <Dialog open={toRemove !== null} onOpenChange={(o) => !o && setToRemove(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remover preset?</DialogTitle>
            <DialogDescription asChild>
              <p>
                <strong>{toRemove?.label}</strong> ({toRemove?.name}). Presets embutidos do sistema
                são recusados pelo daemon.
              </p>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setToRemove(null)} disabled={busy}>
              Cancelar
            </Button>
            <Button
              variant="destructive"
              onClick={() => void remove()}
              disabled={busy}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {busy ? "Removendo…" : "Remover"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Screen>
  );
}
