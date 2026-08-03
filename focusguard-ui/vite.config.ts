import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Em dev, o Vite (5173) faz proxy de /api para o focusguard-web (48902):
// o navegador só fala com o Vite (mesma origem, sem CORS) e o frontend não
// precisa conhecer a porta do servidor. Em produção a própria UI é servida
// pelo focusguard-web, então o proxy nem existe.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:48902",
      },
    },
  },
});
