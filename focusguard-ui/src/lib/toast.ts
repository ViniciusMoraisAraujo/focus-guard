import { toast as sonnerToast } from "sonner";

export type ToastKind = "ok" | "err";

/**
 * toast exibe uma notificação sonner no padrão do app: "ok" = sucesso
 * (verde), "err" = erro. Vive num módulo próprio (e não no contexto) porque é
 * uma função pura do sonner — não precisa de provider nem de re-render de
 * árvore (F1 do plano de refatoração: tirar responsabilidades do AppProvider).
 */
export function toast(msg: string, kind: ToastKind = "ok") {
  if (kind === "err") {
    sonnerToast.error(msg);
  } else {
    sonnerToast.success(msg);
  }
}
