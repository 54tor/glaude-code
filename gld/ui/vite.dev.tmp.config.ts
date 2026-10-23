import { mergeConfig } from "vite"
import base from "./vite.config"
// Fichier temporaire de session : tout ce qui n'est pas servi par Vite part vers le gld qui tourne sur :8799.
export default mergeConfig(base, {
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      "/": {
        target: "http://127.0.0.1:8799", changeOrigin: true,
        bypass: (req: { url?: string; headers: Record<string, string | undefined> }) => {
          const u = req.url ?? ""
          if (u.startsWith("/@") || u.startsWith("/src/") || u.startsWith("/node_modules/") || u.includes("?import") || u.startsWith("/__")) return u
          if ((req.headers.accept ?? "").includes("text/html")) return "/index.html"
          return null
        },
      },
    },
  },
})
