import { useEffect, useRef, type RefObject } from "react"
import { api } from "@/lib/api"
import type { FsIndexEntry, IndexState } from "./useIndex"

const MAX_FILE = 100 * 1024
const MAX_FILES = 300
const MAX_TOTAL = 5 * 1024 * 1024
const BATCH = 50
const FEEDABLE = /\.(tsx?|jsx?|json)$/

const dirSegs = (path: string) => path.split("/").slice(0, -1)
const distance = (fromSegments: string[], toSegments: string[]) => {
  let commonLength = 0
  while (
    commonLength < fromSegments.length &&
    commonLength < toSegments.length &&
    fromSegments[commonLength] === toSegments[commonLength]
  )
    commonLength++
  return fromSegments.length + toSegments.length - 2 * commonLength
}
const compareStrings = (left: string, right: string) => {
  if (left < right) return -1
  if (left > right) return 1
  return 0
}

// Sources données au service TypeScript de Monaco : les plus proches du fichier actif d'abord.
export function pickFeed(files: { path: string; size: number }[], near: string): string[] {
  const here = dirSegs(near)
  const eligible = files.filter((file) => file.size <= MAX_FILE && FEEDABLE.test(file.path))
  const distanceByPath = new Map(eligible.map((file) => [file.path, distance(dirSegs(file.path), here)]))
  eligible.sort(
    (left, right) => distanceByPath.get(left.path)! - distanceByPath.get(right.path)! || compareStrings(left.path, right.path)
  )
  const out: string[] = []
  let total = 0
  for (const file of eligible) {
    if (out.length >= MAX_FILES) break
    if (total + file.size > MAX_TOTAL) continue
    total += file.size
    out.push(file.path)
  }
  return out
}

// Lecture par lots ; la taille annoncée par l'index peut avoir grandi depuis, d'où le second plafond sur le contenu reçu.
export async function runFeed(dir: string, paths: string[], sink: (path: string, content: string) => void, signal: AbortSignal) {
  let total = 0
  for (let batchStart = 0; batchStart < paths.length; batchStart += BATCH) {
    if (signal.aborted) return
    const response = await api.fsFiles(dir, paths.slice(batchStart, batchStart + BATCH)).catch(() => null)
    if (signal.aborted || !response) return
    for (const file of response.files ?? []) {
      total += file.content.length
      if (total > MAX_TOTAL) return
      sink(file.path, file.content)
    }
  }
}

// live : onglet actif lu au moment de l'alimentation, sans relancer le calcul à chaque changement d'onglet.
export function useTsFeed(dir: string, files: FsIndexEntry[], state: IndexState, live: RefObject<{ active: string }>) {
  const fed = useRef(new Map<string, string>()) // chemin → « mtime:taille » de l'index au moment de l'alimentation
  // Alimentation du service TypeScript : seulement les fichiers de l'index, et seulement ceux qui ont changé depuis.
  useEffect(() => {
    if (state === "building") return
    const stamp = new Map(files.map((file) => [file.path, `${file.mtime}:${file.size}`]))
    const picked = pickFeed(files, live.current.active)
    const keep = new Set(picked)
    const gone = [...fed.current.keys()].filter((path) => !keep.has(path))
    const todo = picked.filter((path) => fed.current.get(path) !== stamp.get(path))
    if (!gone.length && !todo.length) return
    const abortController = new AbortController()
    import("./CodeEditor")
      .then(({ setTsLib }) => {
        if (abortController.signal.aborted) return
        for (const path of gone) {
          setTsLib(path, null)
          fed.current.delete(path)
        }
        const feedOne = (path: string, content: string) => {
          setTsLib(path, content)
          fed.current.set(path, stamp.get(path) ?? "")
        }
        return runFeed(dir, todo, feedOne, abortController.signal)
      })
      .catch(() => {})
    return () => abortController.abort()
  }, [dir, files, state, live])
  useEffect(() => {
    const fedAtMount = fed.current
    return () => {
      if (fedAtMount.size) import("./CodeEditor").then((editorModule) => editorModule.clearTsLibs(), () => {})
      fedAtMount.clear()
    }
  }, [dir])
}
