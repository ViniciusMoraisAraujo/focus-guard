import { useState } from "react";
import { DaemonError, execAction } from "@/api/client";
import type { ApiRequest } from "@/api/types";
import { toast } from "@/lib/toast";

export interface ActionResult {
  ok: boolean;
  message: string;
  /** Código de erro estável do daemon (Response.Code) — para a UI ramificar sem depender do texto. */
  code?: string;
  updatePendingReboot?: boolean;
}

export interface ActionFallbacks {
  /** Texto exibido quando o daemon responde ok sem message (ex.: "Preset criado!"). */
  success?: string;
  /** Texto exibido quando o daemon responde success:false sem message. */
  error?: string;
}

export interface UseAction {
  /** true enquanto uma ação está em voo (para disabled/spinner dos botões). */
  busy: boolean;
  /**
   * Executa uma ação e mostra o toast de resultado/erro no padrão do app.
   * Fallbacks só valem quando o daemon não devolve `message`.
   */
  run: (req: ApiRequest, fallbacks?: ActionFallbacks) => Promise<ActionResult>;
}

/**
 * useAction encapsula o padrão repetido nas telas (DRY — F3 do plano de
 * refatoração): busy local + execAction + toast de resultado/erro. O erro de
 * conectividade (DaemonError, que inclui a subclasse SessionExpiredError)
 * continua mostrando a mensagem amigável que as telas mostravam.
 */
export function useAction(): UseAction {
  const [busy, setBusy] = useState(false);

  const run = async (req: ApiRequest, fallbacks?: ActionFallbacks): Promise<ActionResult> => {
    setBusy(true);
    try {
      const res = await execAction(req);
      if (res.ok) {
        toast(res.message || fallbacks?.success || "Operação concluída.");
      } else {
        toast(res.message || fallbacks?.error || "Falha ao executar a ação.", "err");
      }
      return res;
    } catch (e) {
      toast(
        e instanceof DaemonError ? e.message : "Erro inesperado ao executar a ação.",
        "err",
      );
      return { ok: false, message: e instanceof Error ? e.message : "Erro inesperado." };
    } finally {
      setBusy(false);
    }
  };

  return { busy, run };
}
