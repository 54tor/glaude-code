import { useSyncExternalStore } from "react"
import stripAnsi from "strip-ansi"
import { errMsg } from "@/lib/apiError"
import { streamGitPush } from "@/lib/gitPushStream"
import { refreshGit } from "@/hooks/useGit"

// Les pushes en cours (et les derniers, tant que leur toast n'est pas fermé), gardés hors des composants : réduire la fenêtre ou
// changer de page ne les interrompt pas, et leur sortie reste relisible. Plusieurs pushes tournent en parallèle ; le serveur n'en
// sérialise qu'un par dépôt, donc l'interface refuse de relancer un dépôt dont un push tourne encore.
export type GitPushStatus = "running" | "succeeded" | "failed" | "error"
export type GitPushRun = {
  id: number
  dir: string
  remote: string
  branch: string
  status: GitPushStatus
  exitCode?: number
  errorMessage?: string
  isTerminalOpen: boolean
}

type RunOutput = { chunks: Uint8Array[]; bytes: number }

const MAX_OUTPUT_BYTES = 1 << 20

let nextRunId = 1
let currentRuns: GitPushRun[] = []
const outputByRun = new Map<number, RunOutput>()
const runSubscribers = new Set<() => void>()
const outputSubscribers = new Map<number, Set<(chunk: Uint8Array) => void>>()

const setRuns = (nextRuns: GitPushRun[]) => {
  currentRuns = nextRuns
  runSubscribers.forEach((subscriber) => subscriber())
}

const updateRun = (runId: number, changes: Partial<GitPushRun>) => {
  setRuns(currentRuns.map((run) => (run.id === runId ? { ...run, ...changes } : run)))
}

const keepOutput = (runId: number, chunk: Uint8Array) => {
  const output = outputByRun.get(runId)
  if (!output) return
  output.chunks.push(chunk)
  output.bytes += chunk.length
  while (output.bytes > MAX_OUTPUT_BYTES && output.chunks.length > 1) output.bytes -= output.chunks.shift()!.length
  outputSubscribers.get(runId)?.forEach((subscriber) => subscriber(chunk))
}

export const listGitPushRuns = (): GitPushRun[] => currentRuns

export const isGitPushRunning = (dir: string) => currentRuns.some((run) => run.dir === dir && run.status === "running")

export function startGitPush(dir: string, remote: string, branch: string) {
  if (isGitPushRunning(dir)) return
  const runId = nextRunId++
  outputByRun.set(runId, { chunks: [], bytes: 0 })
  const openedRun: GitPushRun = { id: runId, dir, remote, branch, status: "running", isTerminalOpen: true }
  setRuns([...currentRuns.map((run) => ({ ...run, isTerminalOpen: false })), openedRun])
  streamGitPush(dir, remote, (chunk) => keepOutput(runId, chunk))
    .then(
      (exitCode) => updateRun(runId, { status: exitCode === 0 ? "succeeded" : "failed", exitCode }),
      (pushError) => updateRun(runId, { status: "error", errorMessage: errMsg(pushError) })
    )
    .finally(refreshGit)
}

// Un seul terminal est ouvert à la fois : en ouvrir un referme les autres.
export function setGitPushTerminalOpen(runId: number, isTerminalOpen: boolean) {
  setRuns(currentRuns.map((run) => (run.id === runId ? { ...run, isTerminalOpen } : isTerminalOpen ? { ...run, isTerminalOpen: false } : run)))
}

// Fermer le toast d'un push terminé. Sans effet tant qu'il tourne.
export function dismissGitPush(runId: number) {
  const run = currentRuns.find((candidate) => candidate.id === runId)
  if (!run || run.status === "running") return
  outputByRun.delete(runId)
  outputSubscribers.delete(runId)
  setRuns(currentRuns.filter((candidate) => candidate.id !== runId))
}

export const readGitPushChunks = (runId: number): Uint8Array[] => [...(outputByRun.get(runId)?.chunks ?? [])]

export function subscribeGitPushOutput(runId: number, subscriber: (chunk: Uint8Array) => void) {
  const subscribers = outputSubscribers.get(runId) ?? new Set()
  subscribers.add(subscriber)
  outputSubscribers.set(runId, subscribers)
  return () => void subscribers.delete(subscriber)
}

// La sortie d'un push en texte, sans séquences de couleur, réduite à ses `maxLines` dernières lignes.
export function gitPushOutputTail(runId: number, maxLines: number): string {
  const decoder = new TextDecoder()
  const chunks = outputByRun.get(runId)?.chunks ?? []
  const fullText = stripAnsi(chunks.map((chunk) => decoder.decode(chunk, { stream: true })).join("")).replace(/\r\n?/g, "\n")
  const lines = fullText.split("\n").filter((line) => line.trim() !== "")
  return lines.slice(-maxLines).join("\n")
}

export function useGitPushRuns(): GitPushRun[] {
  return useSyncExternalStore(
    (onChange) => {
      runSubscribers.add(onChange)
      return () => void runSubscribers.delete(onChange)
    },
    () => currentRuns
  )
}
