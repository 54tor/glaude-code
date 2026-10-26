// Garde du build : aucun cycle d'imports statiques entre chunks (un cycle a déjà cassé Monaco en production :
// un chunk exécutait une classe d'un autre pas encore évalué) ; l'entrée (app-<hash>.js, lue dans index.html) n'embarque pas Monaco ;
// aucun chunk (hors workers) au-dessus de MAX octets, pour que la dérive du découpage de Monaco soit bruyante.
// `node scripts/check-chunks.mjs --self-test` rejoue le garde sur des jeux de chunks synthétiques.
import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"

const MAX = 700_000
// Imports statiques relatifs seulement : import x from"./a.js", import"./a.js" (même collé après ) ou */), export … from"./a.js" ; pas import().
const re = /(?:^|[;}\s)/])(?:import|export)\s*(?:[\w$*{][^"'`]*?from\s*)?["']\.\/([^"']+\.js)["']/g

// src : { "app.js": "contenu", … }, entry : le nom de l'entrée ; retourne les erreurs et le nombre de chunks initiaux.
function check(src, entry = "app.js") {
  const files = Object.keys(src)
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

  for (const f of files) {
    const o = Buffer.byteLength(src[f])
    if (!/\.worker[-.]/.test(f) && o > MAX) errors.push(`${f} fait ${o} o, au-dessus de la limite de ${MAX} o`)
  }
  if (!src[entry]) errors.push(`assets/${entry} absent`)
  else if (src[entry].length < 10_000) errors.push(`assets/${entry} ne fait que ${src[entry].length} o : l'application n'est plus dans l'entrée`)
  // Rien de Monaco chargé avec la page : app.js et ses imports statiques, transitivement.
  const initial = new Set()
  const walk = (f) => initial.has(f) || (initial.add(f), (deps[f] ?? []).forEach(walk))
  if (src[entry]) walk(entry)
  for (const f of initial) if (/monaco|editor\.api|CodeEditor/.test(f) || src[f].includes("MonacoEnvironment")) errors.push(`${f} chargé avec la page contient Monaco`)
  return { errors, files, initial }
}

if (process.argv[2] === "--self-test") {
  const app = (extra = "") => "x".repeat(10_000) + ";" + extra
  const cases = [
    ["propre", { "app.js": app('import"./a.js";'), "a.js": "" }, 0],
    ["import dynamique ignoré", { "app.js": app('import("./a.js")'), "a.js": 'import("./app.js")' }, 0],
    ["cycle", { "app.js": app('import"./a.js";'), "a.js": 'import{b}from"./b.js";', "b.js": 'import{a}from"./a.js";' }, 1],
    ["cycle collé après )", { "app.js": app('import"./a.js";'), "a.js": 'f();import{b}from"./b.js";', "b.js": 'g()/*x*/import"./a.js";' }, 1],
    ["chunk trop gros", { "app.js": app(), "big.js": "x".repeat(MAX + 1) }, 1],
    ["worker gros toléré", { "app.js": app(), "ts.worker.js": "x".repeat(MAX + 1), "ts.worker-Ab12.js": "x".repeat(MAX + 1) }, 0],
    ["app.js creux", { "app.js": "x" }, 1],
    ["app.js absent", { "a.js": "" }, 1],
    ["Monaco dans l'entrée", { "app.js": app('import"./editor.api.js";'), "editor.api.js": "" }, 1],
  ]
  let bad = 0
  for (const [name, src, want] of cases) {
    const got = check(src).errors.length ? 1 : 0
    if (got !== want) (bad++, console.error(`auto-test ko : ${name} (attendu ${want}, obtenu ${got})`))
  }
  if (bad) process.exit(1)
  console.log(`check-chunks : auto-test ok (${cases.length} cas)`)
} else {
  const dir = join(process.argv[2] ?? "dist", "assets")
  // Les fichiers sources copiés tels quels par Monaco (imports en ../) ne sont pas des chunks.
  const src = {}
  for (const f of readdirSync(dir).filter((f) => f.endsWith(".js"))) {
    const c = readFileSync(join(dir, f), "utf8")
    if (!c.includes("from '../")) src[f] = c
  }
  const entry = /src="\/assets\/([^"]+\.js)"/.exec(readFileSync(join(dir, "..", "index.html"), "utf8"))?.[1] ?? "app.js"
  const { errors, files, initial } = check(src, entry)
  if (errors.length) {
    console.error(errors.join("\n"))
    process.exit(1)
  }
  console.log(`check-chunks : ${files.length} chunks, aucun cycle, aucun chunk > ${MAX} o hors workers, entrée ${entry} (${initial.size} chunks initiaux) sans Monaco`)
}
