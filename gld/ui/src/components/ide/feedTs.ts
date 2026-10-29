import { useEffect, useRef, type RefObject } from "react"
import { api } from "@/lib/api"
import type { FsIndexEntry, IndexState } from "./useIndex"

const MAX_FILE = 100 * 1024
const MAX_FILES = 300
const MAX_TOTAL = 5 * 1024 * 1024
const BATCH = 50

const dirSegs = (p: string) => p.split("/").slice(0, -1)
const distance = (a: string[], b: string[]) => {
  let i = 0
  while (i < a.length && i < b.length && a[i] === b[i]) i++
  return a.length + b.length - 2 * i
}

// Sources données au service TypeScript de Monaco : les plus proches du fichier actif d'abord.
export function pickFeed(files: { path: string; size: number }[], near: string): string[] {
  const here = dirSegs(near)
  const ok = files.filter((f) => f.size <= MAX_FILE && /\.(tsx?|jsx?|json)$/.test(f.path))
  const d = new Map(ok.map((f) => [f.path, distance(dirSegs(f.path), here)]))
  ok.sort((a, b) => d.get(a.path)! - d.get(b.path)! || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
  const out: string[] = []
  let total = 0
  for (const f of ok) {
    if (out.length >= MAX_FILES) break
    if (total + f.size > MAX_TOTAL) continue
    total += f.size
    out.push(f.path)
  }
  return out
}

// Lecture par lots ; la taille annoncée par l'index peut avoir grandi depuis, d'où le second plafond sur le contenu reçu.
export async function runFeed(dir: string, paths: string[], sink: (path: string, content: string) => void, signal: AbortSignal) {
  let total = 0
  for (let i = 0; i < paths.length; i += BATCH) {
    if (signal.aborted) return
    const r = await api.fsFiles(dir, paths.slice(i, i + BATCH)).catch(() => null)
    if (signal.aborted || !r) return
    for (const f of r.files ?? []) {
      total += f.content.length
      if (total > MAX_TOTAL) return
      sink(f.path, f.content)
    }
  }
}

// live : onglet actif lu au moment de l'alimentation, sans relancer le calcul à chaque changement d'onglet.
export function useTsFeed(dir: string, files: FsIndexEntry[], state: IndexState, live: RefObject<{ active: string }>) {
  const fed = useRef(new Map<string, string>()) // chemin → « mtime:taille » de l'index au moment de l'alimentation
  // Alimentation du service TypeScript : seulement les fichiers de l'index, et seulement ceux qui ont changé depuis.
  useEffect(() => {
    if (state === "building") return
    const stamp = new Map(files.map((f) => [f.path, `${f.mtime}:${f.size}`]))
    const picked = pickFeed(files, live.current.active)
    const keep = new Set(picked)
    const gone = [...fed.current.keys()].filter((p) => !keep.has(p))
    const todo = picked.filter((p) => fed.current.get(p) !== stamp.get(p))
    if (!gone.length && !todo.length) return
    const ac = new AbortController()
    import("./CodeEditor")
      .then(({ setTsLib }) => {
        if (ac.signal.aborted) return
        for (const p of gone) {
          setTsLib(p, null)
          fed.current.delete(p)
        }
        return runFeed(dir, todo, (p, c) => (setTsLib(p, c), fed.current.set(p, stamp.get(p) ?? "")), ac.signal)
      })
      .catch(() => {})
    return () => ac.abort()
  }, [dir, files, state, live])
  useEffect(() => {
    const f = fed.current
    return () => {
      if (f.size) import("./CodeEditor").then((m) => m.clearTsLibs(), () => {})
      f.clear()
    }
  }, [dir])
}
