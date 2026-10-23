import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  test: { setupFiles: ["./src/test/setup.ts"] },
  resolve: {
    alias: {
      "@": resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    // Jamais de data: dans la CSS (petites polices) : la CSP de gld n'autorise que les fichiers servis.
    assetsInlineLimit: 0,
    // Servi en local par le binaire Go : le poids du bundle principal (≈ 540 ko) ne coûte rien sur le réseau ; xterm est déjà chargé à la demande.
    chunkSizeWarningLimit: 700,
    // Noms stables : dist/ est versionné (le binaire Go l'embarque), autant éviter le bruit dans l'historique.
    rolldownOptions: {
      output: {
        entryFileNames: "assets/app.js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
})
