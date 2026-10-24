// Garde du build : aucun cycle d'imports statiques entre chunks (un cycle a déjà cassé Monaco en production :
// un chunk exécutait une classe d'un autre pas encore évalué) ; l'entrée reste assets/app.js et n'embarque pas Monaco.
import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"

const dir = join(process.argv[2] ?? "dist", "assets")
// Les fichiers sources copiés tels quels par Monaco (imports en ../) ne sont pas des chunks.
const files = readdirSync(dir).filter((f) => f.endsWith(".js") && !readFileSync(join(dir, f), "utf8").includes("from '../"))
const src = Object.fromEntries(files.map((f) => [f, readFileSync(join(dir, f), "utf8")]))
// Imports statiques relatifs seulement : import x from"./a.js", import"./a.js", export … from"./a.js" ; pas import().
const re = /(?:^|[;}\s])(?:import|export)\s*(?:[\w$*{][^"'`]*?from\s*)?["']\.\/([^"']+\.js)["']/g
const deps = Object.fromEntries(files.map((f) => [f, [...new Set([...src[f].matchAll(re)].map((m) => m[1]))]]))

const errors = []
// Tarjan : toute composante fortement connexe de plus d'un chunk est un cycle.
let n = 0
const idx = {}, low = {}, on = new Set(), stack = []
const visit = (v) => {
  idx[v] = low[v] = n++
  stack.push(v), on.add(v)
  for (const w of deps[v] ?? []) {
    if (!(w in deps)) errors.push(`${v} importe ${w}, absent`)
    else if (!(w in idx)) visit(w), (low[v] = Math.min(low[v], low[w]))
    else if (on.has(w)) low[v] = Math.min(low[v], idx[w])
  }
  if (low[v] !== idx[v]) return
  const scc = []
  let w
  do (w = stack.pop()), on.delete(w), scc.push(w)
  while (w !== v)
  if (scc.length > 1) errors.push(`cycle entre chunks : ${scc.sort().join(", ")}`)
}
files.forEach((f) => f in idx || visit(f))

if (!src["app.js"]) errors.push("assets/app.js absent")
else if (src["app.js"].length < 10_000) errors.push(`assets/app.js ne fait que ${src["app.js"].length} o : l'application n'est plus dans l'entrée`)
// Rien de Monaco chargé avec la page : app.js et ses imports statiques, transitivement.
const initial = new Set()
const walk = (f) => initial.has(f) || (initial.add(f), (deps[f] ?? []).forEach(walk))
if (src["app.js"]) walk("app.js")
for (const f of initial) if (/monaco|editor\.api|CodeEditor/.test(f) || src[f].includes("MonacoEnvironment")) errors.push(`${f} chargé avec la page contient Monaco`)

if (errors.length) {
  console.error(errors.join("\n"))
  process.exit(1)
}
console.log(`check-chunks : ${files.length} chunks, aucun cycle, entrée app.js (${initial.size} chunks initiaux) sans Monaco`)
