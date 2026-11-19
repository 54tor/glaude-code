import { i18n } from "@/i18n"
import { ApiError } from "@/lib/apiError"
import { http, qs, seg } from "@/lib/http"

// Les poussées tournent côté serveur, dans leur propre session tmux : le navigateur (ou gld) peut s'arrêter sans les interrompre.
// Ici : les créer, les relister après un rechargement, et suivre leur sortie par sondage du journal.
type PushRemoteState = {
  name: string
  state: "pending" | "running" | "succeeded" | "failed"
  exit?: number
}
export type PushRunInfo = {
  id: string
  dir: string
  branch: string
  remotes: PushRemoteState[]
  done: boolean
  interrupted?: boolean
  started: number
}
type OutputPage = { out: string | null; next: number; run: PushRunInfo }

const POLL_INTERVAL_MS = 500
const RETRY_INTERVAL_MS = 1500 // gld redémarre : la poussée continue, on retente
const SERVER_ERROR_STATUS = 500

const bytesOfBase64 = (encoded: string) =>
  Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
const sleep = (milliseconds: number) =>
  new Promise((resolve) => setTimeout(resolve, milliseconds))
const isTransient = (error: unknown) =>
  error instanceof ApiError &&
  (error.net || error.status >= SERVER_ERROR_STATUS)

export const listPushRuns = () =>
  http
    .get<{ runs: PushRunInfo[] }>("/git/push-runs")
    .then((response) => response.runs)
export const deletePushRun = (runId: string) =>
  http.del(`/git/push-runs/${seg(runId)}`)

// Premier code de retour non nul des remotes, 0 si tous ont réussi.
const exitCodeOfRun = (run: PushRunInfo) =>
  run.remotes.find((remote) => remote.exit !== undefined && remote.exit !== 0)
    ?.exit ?? 0

async function createPushRun(
  dir: string,
  remotes: string[],
  tags: string[]
): Promise<PushRunInfo> {
  const response = await http.post<{ run: PushRunInfo }>("/git/push-runs", {
    dir,
    remotes,
    tags,
  })
  return response.run
}

// Suit la poussée runId jusqu'à sa fin et rend le code de retour ; la sortie (hooks compris) est transmise à onOutput.
export async function followGitPush(
  runId: string,
  onOutput: (chunk: Uint8Array) => void,
  abortSignal?: AbortSignal
): Promise<number> {
  let offset = 0
  for (;;) {
    if (abortSignal?.aborted) throw new Error("aborted")
    let page: OutputPage
    try {
      page = await http.get<OutputPage>(
        `/git/push-runs/${seg(runId)}/output${qs({ offset })}`
      )
    } catch (requestError) {
      if (!isTransient(requestError)) throw requestError
      await sleep(RETRY_INTERVAL_MS)
      continue
    }
    if (page.out) onOutput(bytesOfBase64(page.out))
    offset = page.next
    if (page.out) continue
    if (page.run.done) return exitCodeOfRun(page.run)
    if (page.run.interrupted)
      throw new Error(i18n.t("conversation.gitPush.interrupted"))
    await sleep(POLL_INTERVAL_MS)
  }
}

// Lance la poussée vers remotes (l'un après l'autre, côté serveur) et la suit ; onStarted reçoit son identifiant dès la création.
export async function streamGitPush(
  dir: string,
  remotes: string[],
  onOutput: (chunk: Uint8Array) => void,
  onStarted?: (runId: string) => void,
  tags: string[] = []
): Promise<number> {
  const run = await createPushRun(dir, remotes, tags)
  onStarted?.(run.id)
  return followGitPush(run.id, onOutput)
}
