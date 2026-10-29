import { authFetch } from "@/lib/auth"
import { netError, readResponse, v1 } from "@/lib/apiError"

export { ApiError, qs, seg, v1 } from "@/lib/apiError"
export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE"

// Appel JSON de l'API /v1 : chemin sans préfixe (« /sessions/x/process »), corps JSON optionnel. Lève ApiError.
export async function request<T = void>(method: Method, path: string, body?: unknown, timeoutMs?: number): Promise<T> {
  try {
    const r = await authFetch(v1(path), {
      method,
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: timeoutMs ? AbortSignal.timeout(timeoutMs) : undefined,
    })
    return await readResponse<T>(r)
  } catch (e) {
    throw netError(e)
  }
}

export const http = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T = void>(path: string, body?: unknown, timeoutMs?: number) => request<T>("POST", path, body, timeoutMs),
  put: <T = void>(path: string, body?: unknown, timeoutMs?: number) => request<T>("PUT", path, body, timeoutMs),
  patch: <T = void>(path: string, body?: unknown, timeoutMs?: number) => request<T>("PATCH", path, body, timeoutMs),
  del: <T = void>(path: string, body?: unknown, timeoutMs?: number) => request<T>("DELETE", path, body, timeoutMs),
}
