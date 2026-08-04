import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ComponentProps } from "react";

// Tema fixo escuro: o FocusGuard é um app de foco (uso noturno), então o
// modo dark é travado — evita flash de tema claro e dispensa toggle.
export function ThemeProvider(props: ComponentProps<typeof NextThemesProvider>) {
  return <NextThemesProvider {...props} />;
}
