import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

type ChunkCtx = { getModuleInfo(id: string): { importedIds: string[]; importers: string[]; code: string | null } | null }
const isMonaco = (id: string) => /node_modules[\\/]monaco-editor[\\/]/.test(id)
const monacoIds: string[] = []
let slices: Map<string, string> | undefined

// Modules de Monaco rangés dans l'ordre où ESM les exécute (parcours en profondeur, composantes fortement connexes
// gardées ensemble), puis coupés en tranches consécutives : un chunk n'importe que des chunks antérieurs, jamais de cycle.
// Les modules que Monaco n'atteint qu'en import() (définitions de langages) gardent leurs chunks paresseux.
// Budget en octets de source transformée : ≈ 520 ko une fois minifié. ponytail: calcul fait une fois par processus, à refaire en mode --watch.
function monacoSlice(id: string, ctx: ChunkCtx) {
  if (!slices) {
    const map = new Map<string, string>()
    const set = new Set(monacoIds)
    const info = (m: string) => ctx.getModuleInfo(m)
    let n = 0, part = 1, size = 0
    const idx = new Map<string, number>(), low = new Map<string, number>(), stack: string[] = [], on = new Set<string>()
    const visit = (v: string) => {
      idx.set(v, n)
      low.set(v, n++)
      stack.push(v)
      on.add(v)
      for (const w of info(v)?.importedIds ?? []) {
        if (!set.has(w)) continue
        if (!idx.has(w)) {
          visit(w)
          low.set(v, Math.min(low.get(v)!, low.get(w)!))
        } else if (on.has(w)) low.set(v, Math.min(low.get(v)!, idx.get(w)!))
      }
      if (low.get(v) !== idx.get(v)) return
      const scc: string[] = []
      let w: string
      do {
        w = stack.pop()!
        on.delete(w)
        scc.push(w)
      } while (w !== v)
      const sz = scc.reduce((a, m) => a + (info(m)?.code?.length ?? 0), 0)
      if (size && size + sz > 1_400_000) {
        part++
        size = 0
      }
      size += sz
      for (const m of scc) map.set(m, `monaco${part}`)
    }
    for (const r of monacoIds) if (!idx.has(r) && info(r)?.importers.some((i) => !set.has(i))) visit(r)
    slices = map
  }
  return slices.get(id) ?? null
}

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
    // Servi en local par le binaire Go : le poids du bundle principal (≈ 600 ko) ne coûte rien sur le réseau ; xterm est déjà chargé à la demande.
    chunkSizeWarningLimit: 750,
    // Noms hachés : servis sous /assets/ avec un cache permanent (navigateur, service worker, CDN) ; une nouvelle version change
    // les noms, index.html (jamais mis en cache) pointe vers les nouveaux.
    rolldownOptions: {
      output: {
        // Monaco (≈ 4 Mo) découpé en morceaux < 700 ko, tous chargés à la demande avec l'éditeur ; app-<hash>.js reste l'entrée.
        // Pas de maxSize : il coupait Monaco en chunks qui s'importaient mutuellement, page blanche en production.
        codeSplitting: {
          groups: [{ name: monacoSlice, debugName: "monaco", test: (id) => isMonaco(id) && !!monacoIds.push(id), includeDependenciesRecursively: false }],
        },
        entryFileNames: "assets/app-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
  // Workers de Monaco : hachés eux aussi (ts.worker-<hash>.js…), servis par gld, même origine que la CSP.
  worker: {
    format: "es",
    rolldownOptions: {
      output: {
        entryFileNames: "assets/[name]-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
})
