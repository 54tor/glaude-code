import { authFetch } from "@/lib/auth"
import { netError, readResponse, v1 } from "@/lib/apiError"

const NDJSON_MEDIA_TYPE = "application/x-ndjson"
const PUSH_TIMEOUT_MS = 16 * 60 * 1000 // un peu plus que le plafond du serveur (15 min)

type PushFrame = { out?: string; exit?: number }

const bytesOfBase64 = (encoded: string) => Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))

// Pousse la branche et rend le code de retour de git ; la sortie (hooks compris) est transmise à onOutput au fil de l'eau.
export async function streamGitPush(dir: string, remote: string, onOutput: (chunk: Uint8Array) => void, abortSignal?: AbortSignal): Promise<number> {
  let response: Response
  try {
    response = await authFetch(v1("/git/pushes"), {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: NDJSON_MEDIA_TYPE },
      body: JSON.stringify({ dir, remote }),
      signal: AbortSignal.any([AbortSignal.timeout(PUSH_TIMEOUT_MS), ...(abortSignal ? [abortSignal] : [])]),
    })
    if (!response.ok || !response.body) return await readResponse<never>(response)
  } catch (requestError) {
    throw netError(requestError)
  }
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let pendingText = ""
  let exitCode: number | undefined
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    pendingText += value
    const lines = pendingText.split("\n")
    pendingText = lines.pop() ?? ""
    for (const line of lines) {
      if (!line) continue
      const frame: PushFrame = JSON.parse(line)
      if (frame.out) onOutput(bytesOfBase64(frame.out))
      if (frame.exit !== undefined) exitCode = frame.exit
    }
  }
  if (exitCode === undefined) throw new Error("push stream ended without exit code")
  return exitCode
}
