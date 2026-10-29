import { i18n } from "@/i18n"

// Contrat d'erreur unique de l'API /v1 : {"error":{"code","message"}} avec le vrai statut HTTP.
export class ApiError extends Error {
  status: number
  code: string
  retry: number // Retry-After en secondes, 0 si absent
  net: boolean // pas de réponse (réseau, délai) : l'action a pu avoir lieu
  constructor(status: number, code: string, message: string, o: { retry?: number; net?: boolean } = {}) {
    super(message)
    this.status = status
    this.code = code
    this.retry = o.retry ?? 0
    this.net = o.net ?? false
  }
}

export const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

export const V1 = "/v1"
export const v1 = (path: string) => (path.startsWith(V1 + "/") ? path : V1 + path)
// Segment de chemin : encode aussi « / » (clés mémoire u/…, p/<dossier>/…).
export const seg = (s: string) => encodeURIComponent(s)

// ?a=1&item=x&item=y : tableaux répétés, undefined/""/false ignorés, true = "1".
export function qs(o: Record<string, string | number | boolean | undefined | (string | number)[]>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(o)) {
    if (v === undefined || v === "" || v === false) continue
    for (const x of Array.isArray(v) ? v : [v]) p.append(k, v === true ? "1" : String(x))
  }
  const s = p.toString()
  return s ? "?" + s : ""
}

// 2xx : JSON analysé, undefined pour 204 ou corps vide. Autre statut : ApiError, corps non JSON toléré.
export async function readResponse<T>(r: Response): Promise<T> {
  const retry = Number(r.headers.get("Retry-After")) || 0
  const text = r.status === 204 ? "" : await r.text().catch(() => "")
  let j: unknown
  let bad = false
  try {
    j = text ? JSON.parse(text) : undefined
  } catch {
    bad = true
  }
  if (r.ok) {
    if (bad) throw new ApiError(r.status, "bad_response", i18n.t("shell.api.error", { msg: "JSON" })) // page HTML d'une passerelle, pas la réponse de l'API
    return j as T
  }
  const e = (j as { error?: { code?: string; message?: string } } | undefined)?.error
  throw new ApiError(r.status, e?.code || `http_${r.status}`, e?.message || i18n.t("shell.api.error", { msg: String(r.status) }), { retry })
}

export function netError(e: unknown): ApiError {
  if (e instanceof ApiError) return e
  return new ApiError(0, "net", i18n.t("shell.api.error", { msg: e instanceof Error ? e.message : String(e) }), { net: true })
}
