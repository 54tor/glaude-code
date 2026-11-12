import { useSyncExternalStore } from "react"
import stripAnsi from "strip-ansi"
import { errMsg } from "@/lib/apiError"
import { streamGitPush } from "@/lib/gitPushStream"
import { refreshGit } from "@/hooks/useGit"

// Le push en cours (ou le dernier, tant que son toast n'est pas fermé), gardé hors des composants : réduire la fenêtre ou changer
// de page ne l'interrompt pas, et sa sortie reste relisible. Un seul push à la fois.
export type GitPushStatus = "running" | "succeeded" | "failed" | "error"
export type GitPushRun = {
  dir: string
  remote: string
  branch: string
  status: GitPushStatus
  exitCode?: number
  errorMessage?: string
  isTerminalOpen: boolean
}

const MAX_OUTPUT_BYTES = 1 << 20

let currentRun: GitPushRun | null = null
let outputChunks: Uint8Array[] = []
let outputBytes = 0
const runSubscribers = new Set<() => void>()
const outputSubscribers = new Set<(chunk: Uint8Array) => void>()

const setRun = (nextRun: GitPushRun | null) => {
  currentRun = nextRun
  runSubscribers.forEach((subscriber) => subscriber())
}

const keepOutput = (chunk: Uint8Array) => {
  outputChunks.push(chunk)
  outputBytes += chunk.length
  while (outputBytes > MAX_OUTPUT_BYTES && outputChunks.length > 1) outputBytes -= outputChunks.shift()!.length
  outputSubscribers.forEach((subscriber) => subscriber(chunk))
}

export function startGitPush(dir: string, remote: string, branch: string) {
  if (currentRun?.status === "running") return
  outputChunks = []
  outputBytes = 0
  setRun({ dir, remote, branch, status: "running", isTerminalOpen: true })
  streamGitPush(dir, remote, keepOutput)
    .then(
      (exitCode) => setRun({ ...currentRun!, status: exitCode === 0 ? "succeeded" : "failed", exitCode }),
      (pushError) => setRun({ ...currentRun!, status: "error", errorMessage: errMsg(pushError) })
    )
    .finally(refreshGit)
}

export function setGitPushTerminalOpen(isTerminalOpen: boolean) {
  if (currentRun) setRun({ ...currentRun, isTerminalOpen })
}

// Fermer le toast d'un push terminé. Sans effet tant qu'il tourne.
export function dismissGitPush() {
  if (!currentRun || currentRun.status === "running") return
  outputChunks = []
  outputBytes = 0
  setRun(null)
}

export const readGitPushChunks = (): Uint8Array[] => [...outputChunks]

export function subscribeGitPushOutput(subscriber: (chunk: Uint8Array) => void) {
  outputSubscribers.add(subscriber)
  return () => void outputSubscribers.delete(subscriber)
}

// La sortie en texte, sans séquences de couleur, réduite à ses `maxLines` dernières lignes.
export function gitPushOutputTail(maxLines: number): string {
  const decoder = new TextDecoder()
  const fullText = stripAnsi(outputChunks.map((chunk) => decoder.decode(chunk, { stream: true })).join("")).replace(/\r\n?/g, "\n")
  const lines = fullText.split("\n").filter((line) => line.trim() !== "")
  return lines.slice(-maxLines).join("\n")
}

export function useGitPushRun(): GitPushRun | null {
  return useSyncExternalStore(
    (onChange) => {
      runSubscribers.add(onChange)
      return () => void runSubscribers.delete(onChange)
    },
    () => currentRun
  )
}
