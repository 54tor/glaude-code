import { useEffect, useState, useSyncExternalStore } from "react"

// Mode multi-utilisateur (auth_routes.go, auth_admin.go) : état de connexion partagé, appels vers /auth/* et /admin/*, WebAuthn.

export const RIGHTS = ["mem_read", "mem_write", "config_write", "mem_sync", "usage_read", "sessions_read", "exec"] as const
export type Right = (typeof RIGHTS)[number]
// Ces deux droits font exécuter du code sous le compte système de gld.
export const DANGEROUS: readonly Right[] = ["exec", "config_write"]

export type Factors = { totp: boolean; webauthn: number; recovery: number }
export type AuthUser = { name: string; admin: boolean }
export type AuthState = "loading" | "open" | "anon" | "mfa" | "ok" | "unavailable"
export type Reauth = { password: string; code: string }
export type Auth = {
  state: AuthState
  user?: AuthUser
  csrf: string
  factors?: Factors
  methods: string[] // seconds facteurs proposés : totp, webauthn, recovery
  enroll: boolean // compte sans facteur : enrôlement obligatoire
  expired: boolean // session perdue en cours d'usage : l'application reste montée sous l'écran de connexion
}

let snap: Auth = { state: "loading", csrf: "", methods: [], enroll: false, expired: false }
const subs = new Set<() => void>()

export function setAuth(p: Partial<Auth>) {
  snap = { ...snap, ...p }
  subs.forEach((f) => f())
}
export const getAuth = () => snap
export function useAuth(): Auth {
  return useSyncExternalStore(
    (f) => {
      subs.add(f)
      return () => void subs.delete(f)
    },
    getAuth
  )
}

// Tout appel au serveur passe ici : jeton CSRF sur les requêtes modifiantes, retour à la connexion si la session tombe.
// Un 401 portant Retry-After est un échec de vérification (mot de passe, code), pas une session perdue.
export async function authFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const m = (init.method ?? "GET").toUpperCase()
  if (snap.csrf && m !== "GET" && m !== "HEAD") {
    const h = new Headers(init.headers)
    h.set("X-Gld-CSRF", snap.csrf)
    init = { ...init, headers: h }
  }
  const r = await fetch(url, init)
  if (r.status === 401 && snap.state === "ok" && !r.headers.has("Retry-After")) setAuth({ state: "anon", csrf: "", expired: true })
  return r
}

export type AuthRes<T = object> = { status: number; ok: boolean; body: { ok?: boolean; out?: string } & Partial<T>; retry: number }

export async function authCall<T = object>(url: string, body?: unknown, method = "POST"): Promise<AuthRes<T>> {
  try {
    const r = await authFetch(url, { method, headers: body === undefined ? undefined : { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) })
    const b = (await r.json().catch(() => ({}))) as AuthRes<T>["body"]
    return { status: r.status, ok: r.ok && b.ok !== false, body: b, retry: Number(r.headers.get("Retry-After")) || 0 }
  } catch {
    return { status: 0, ok: false, body: {}, retry: 0 }
  }
}

const methodsOf = (f: Factors) => [...(f.totp ? ["totp"] : []), ...(f.webauthn ? ["webauthn"] : []), ...(f.recovery ? ["recovery"] : [])]

// Au démarrage et après chaque étape de connexion. 404 : mode ouvert, l'application s'affiche comme sans comptes.
export async function boot(): Promise<void> {
  let r: Response
  try {
    r = await fetch("/auth/me")
  } catch {
    return setAuth({ state: "unavailable" })
  }
  if (r.status === 404) return setAuth({ state: "open" })
  if (r.status === 401) {
    // Une base sans compte monte /auth/* sans activer le mode : une route ordinaire servie sans session le révèle.
    const probe = await fetch("/sessions-stamp").catch(() => null)
    if (probe?.ok) return setAuth({ state: "open" })
    return setAuth({ state: "anon", csrf: "", user: undefined })
  }
  if (!r.ok) return setAuth({ state: "unavailable" })
  const b = (await r.json()) as { user: AuthUser; mfa: boolean; csrf: string; factors: Factors }
  const f = b.factors
  setAuth({ state: b.mfa ? "ok" : "mfa", user: b.user, csrf: b.csrf, factors: f, methods: methodsOf(f), enroll: !f.totp && !f.webauthn, ...(b.mfa ? { expired: false } : {}) })
}

export async function logout(): Promise<void> {
  await authCall("/auth/logout")
  setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined, expired: false })
}

// Attente imposée par Retry-After : secondes restantes, et de quoi la déclencher.
export function useWait(): [number, (sec: number) => void] {
  const [until, setUntil] = useState(0)
  const [now, setNow] = useState(0)
  useEffect(() => {
    if (until <= now) return
    const t = setTimeout(() => setNow(Date.now()), 250)
    return () => clearTimeout(t)
  }, [until, now])
  const start = (sec: number) => {
    const n = Date.now()
    setNow(n)
    setUntil(sec > 0 ? n + sec * 1000 : 0)
  }
  return [Math.max(0, Math.ceil((until - now) / 1000)), start]
}

// WebAuthn sans bibliothèque : go-webauthn émet et attend le JSON standard, binaires en base64url.
const dec = (s: string): ArrayBuffer => {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "="))
  return Uint8Array.from(b, (c) => c.charCodeAt(0)).buffer
}
const enc = (buf: ArrayBuffer): string =>
  btoa(String.fromCharCode(...new Uint8Array(buf)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "")
type CredRef = { id: string; type: string; transports?: string[] }
type PKOptions = Record<string, unknown> & { challenge: string; user?: { id: string; name: string; displayName: string }; excludeCredentials?: CredRef[]; allowCredentials?: CredRef[] }
const refs = (l?: CredRef[]) => l?.map((c) => ({ ...c, id: dec(c.id) }))

export const waAvailable = () => typeof window !== "undefined" && window.isSecureContext && "PublicKeyCredential" in window

async function waCreate(o: { publicKey: PKOptions }) {
  const p = o.publicKey
  const c = (await navigator.credentials.create({
    publicKey: { ...p, challenge: dec(p.challenge), user: { ...p.user!, id: dec(p.user!.id) }, excludeCredentials: refs(p.excludeCredentials) } as unknown as PublicKeyCredentialCreationOptions,
  })) as PublicKeyCredential
  const r = c.response as AuthenticatorAttestationResponse
  return {
    id: c.id,
    rawId: enc(c.rawId),
    type: c.type,
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
    clientExtensionResults: c.getClientExtensionResults(),
    response: { attestationObject: enc(r.attestationObject), clientDataJSON: enc(r.clientDataJSON), transports: r.getTransports?.() ?? [] },
  }
}

async function waGet(o: { publicKey: PKOptions }) {
  const p = o.publicKey
  const c = (await navigator.credentials.get({
    publicKey: { ...p, challenge: dec(p.challenge), allowCredentials: refs(p.allowCredentials) } as unknown as PublicKeyCredentialRequestOptions,
  })) as PublicKeyCredential
  const r = c.response as AuthenticatorAssertionResponse
  return {
    id: c.id,
    rawId: enc(c.rawId),
    type: c.type,
    clientExtensionResults: c.getClientExtensionResults(),
    response: { authenticatorData: enc(r.authenticatorData), clientDataJSON: enc(r.clientDataJSON), signature: enc(r.signature), userHandle: r.userHandle ? enc(r.userHandle) : undefined },
  }
}

const failed: AuthRes = { status: 0, ok: false, body: {}, retry: 0 }

// Assertion sur la session courante : second facteur à la connexion, ou réauthentification sans code (cinq minutes).
export async function waAssert(): Promise<AuthRes<{ csrf: string }>> {
  const b = await authCall<{ options: { publicKey: PKOptions } }>("/auth/mfa/webauthn/begin")
  if (!b.ok || !b.body.options) return b
  try {
    return await authCall("/auth/mfa/webauthn/finish", await waGet(b.body.options))
  } catch {
    return failed
  }
}

export async function waRegister(label: string, reauth?: Reauth): Promise<AuthRes<{ csrf: string; recovery: string[] }>> {
  const b = await authCall<{ options: { publicKey: PKOptions } }>("/auth/webauthn/register/begin", reauth ?? {})
  if (!b.ok || !b.body.options) return b
  try {
    return await authCall(`/auth/webauthn/register/finish?label=${encodeURIComponent(label)}`, await waCreate(b.body.options))
  } catch {
    return failed
  }
}
