import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * ThemeToggle alterna entre tema claro e escuro. O tema escolhido é
 * persistido pelo next-themes (localStorage "theme") e aplicado como classe
 * `dark` no <html> — o CSS já define as variáveis para os dois temas em
 * index.css (:root = claro, .dark = escuro).
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme();
  // resolvedTheme é undefined no primeiro render (o next-themes o resolve num
  // efeito); como o default é dark, tratamos undefined como escuro para não
  // piscar o ícone errado no primeiro frame.
  const isDark = resolvedTheme !== "light";

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={isDark ? "Ativar tema claro" : "Ativar tema escuro"}
      title={isDark ? "Ativar tema claro" : "Ativar tema escuro"}
      onClick={() => setTheme(isDark ? "light" : "dark")}
      className={cn("text-muted-foreground", className)}
    >
      {isDark ? <Sun /> : <Moon />}
    </Button>
  );
}
