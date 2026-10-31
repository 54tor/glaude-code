import { i18n } from "@/i18n"

// Contrat d'erreur unique de l'API /v1 : {"error":{"code","message"}} avec le vrai statut HTTP.
export class ApiError extends Error {
  status: number
  code: string
  retry: number // Retry-After en secondes, 0 si absent
  net: boolean // pas de réponse (réseau, délai) : l'action a pu avoir lieu
  etag?: string // 409 d'une sauvegarde : etag du disque (vide si supprimé)
  constructor(status: number, code: string, message: string, options: { retry?: number; net?: boolean; etag?: string } = {}) {
    super(message)
    this.status = status
    this.code = code
    this.retry = options.retry ?? 0
    this.net = options.net ?? false
    this.etag = options.etag
  }
}

export const errMsg = (error: unknown) => {
  if (error instanceof Error) return error.message
  return String(error)
}

export const V1 = "/v1"
export const v1 = (path: string) => (path.startsWith(V1 + "/") ? path : V1 + path)
// Segment de chemin : encode aussi « / » (clés mémoire u/…, p/<dossier>/…).
export const seg = (segment: string) => encodeURIComponent(segment)

// ?a=1&item=x&item=y : tableaux répétés, undefined/""/false ignorés, true = "1".
export function qs(params: Record<string, string | number | boolean | undefined | (string | number)[]>): string {
  const searchParams = new URLSearchParams()
  for (const [paramName, paramValue] of Object.entries(params)) {
    const isOmitted = paramValue === undefined || paramValue === "" || paramValue === false
    if (isOmitted) continue
    if (paramValue === true) {
      searchParams.append(paramName, "1")
      continue
    }
    const values = Array.isArray(paramValue) ? paramValue : [paramValue]
    for (const value of values) searchParams.append(paramName, String(value))
  }
  const queryString = searchParams.toString()
  if (!queryString) return ""
  return "?" + queryString
}

// 2xx : JSON analysé, undefined pour 204 ou corps vide. Autre statut : ApiError, corps non JSON toléré.
const HTTP_NO_CONTENT = 204

export async function readResponse<T>(response: Response): Promise<T> {
  const retry = Number(response.headers.get("Retry-After")) || 0
  const text = response.status === HTTP_NO_CONTENT ? "" : await response.text().catch(() => "")
  let parsedBody: unknown
  let isInvalidJson = false
  try {
    parsedBody = text ? JSON.parse(text) : undefined
  } catch {
    isInvalidJson = true
  }
  if (response.ok) {
    if (isInvalidJson) throw new ApiError(response.status, "bad_response", i18n.t("shell.api.error", { msg: "JSON" })) // page HTML d'une passerelle, pas la réponse de l'API
    return parsedBody as T
  }
  const errorBody = (parsedBody as { error?: { code?: string; message?: string; etag?: string } } | undefined)?.error
  const code = errorBody?.code || `http_${response.status}`
  const message = errorBody?.message || i18n.t("shell.api.error", { msg: String(response.status) })
  throw new ApiError(response.status, code, message, { retry, etag: errorBody?.etag })
}

export function netError(error: unknown): ApiError {
  if (error instanceof ApiError) return error
  return new ApiError(0, "net", i18n.t("shell.api.error", { msg: errMsg(error) }), { net: true })
}
