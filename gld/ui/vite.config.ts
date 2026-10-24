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
    // Servi en local par le binaire Go : le poids du bundle principal (≈ 650 ko) ne coûte rien sur le réseau ; xterm est déjà chargé à la demande.
    chunkSizeWarningLimit: 700,
    // Noms stables : le binaire Go embarque dist/ au make install et le sert sous /assets/ ; un nom prévisible se vérifie et se cache simplement.
    rolldownOptions: {
      output: {
        entryFileNames: "assets/app.js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
  // Workers de Monaco : noms stables eux aussi (ts.worker.js, editor.worker.js…), servis par gld, même origine que la CSP.
  worker: {
    format: "es",
    rolldownOptions: {
      output: {
        entryFileNames: "assets/[name].js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
})
