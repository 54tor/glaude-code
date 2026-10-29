import { useEffect, useState, useSyncExternalStore } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { clearDrafts, guardDrafts } from "@/hooks/useDraft"
import { resetGit } from "@/hooks/useGit"
import { resetWarm } from "@/hooks/useKeepWarm"
import { resetOpen } from "@/hooks/useOpen"
import { v1 } from "@/lib/apiError"
import { resetTrust } from "@/lib/trust"

// Mode multi-utilisateur (auth_routes.go, auth_admin.go) : état de connexion partagé, appels vers /auth/* et /admin/*, WebAuthn.

export const RIGHTS = ["mem_read", "mem_write", "config_write", "mem_sync", "usage_read", "sessions_read", "exec", "fs_read", "fs_write"] as const
export type Right = (typeof RIGHTS)[number]
// exec, config_write et fs_write équivalent au compte système de gld.
export const DANGEROUS: readonly Right[] = ["exec", "config_write", "fs_write"]

export type Key = { id: number; label: string; created: number; bad: boolean }
export type Factors = { totp: boolean; webauthn: number; recovery: number; keys?: Key[] }
export type HomeRef = { id: string; name: string }
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
  owner?: string // compte dont l'application montée porte les données ; un autre compte la fait remonter à neuf
  rights?: Record<string, Right[]> // par identifiant de home, seulement ceux où le compte a un droit
  homes?: HomeRef[]
  share?: string // invité : identifiant du partage ouvert dans cet onglet, joint à chaque requête (X-Gld-Share)
  home?: string // identifiant du home servi à cette page (le choix de home recharge la page)
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
// Hors session établie, seules les routes de connexion modifient : une file d'attente de l'application restée montée ne part pas
// avec le jeton d'un compte en cours de connexion.
export async function authFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const m = (init.method ?? "GET").toUpperCase()
  if (m !== "GET" && m !== "HEAD" && snap.state !== "ok" && snap.state !== "open" && !url.startsWith("/v1/auth/") && !url.startsWith("/auth/")) throw new Error(i18n.t("auth.notReady"))
  if (snap.csrf && m !== "GET" && m !== "HEAD") {
    const h = new Headers(init.headers)
    h.set("X-Gld-CSRF", snap.csrf)
    init = { ...init, headers: h }
  }
  if (snap.share) {
    const h = new Headers(init.headers)
    h.set("X-Gld-Share", snap.share)
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
// Une panne pendant un rafraîchissement (session établie) laisse l'application en place : seul un démarrage mène à « indisponible ».
export async function boot(): Promise<void> {
  const down = () => (snap.state === "ok" ? void toast.error(i18n.t("auth.refreshFailed")) : setAuth({ state: "unavailable" }))
  let r: Response
  try {
    r = await fetch("/auth/me")
  } catch {
    return down()
  }
  if (r.status === 404) return setAuth({ state: "open" })
  if (r.status === 401) {
    return setAuth({ state: "anon", csrf: "", user: undefined })
  }
  if (!r.ok) return down()
  let b: { user: AuthUser; mfa: boolean; csrf: string; factors: Factors; rights?: Record<string, Right[]>; homes?: HomeRef[] }
  try {
    b = await r.json()
  } catch {
    return down()
  }
  const f = b.factors
  const base = { user: b.user, csrf: b.csrf, factors: f, methods: methodsOf(f), enroll: !f.totp && !f.webauthn }
  if (!b.mfa) return setAuth({ ...base, state: "mfa" })
  adopt(b.user.name)
  setAuth({ ...base, state: "ok", expired: false, owner: b.user.name, rights: b.rights ?? {}, homes: b.homes ?? [], home: await currentHome() })
}

// /homes donne le nom du home servi ("" : le principal) ; les droits sont rangés par identifiant.
async function currentHome(): Promise<string | undefined> {
  try {
    const r = await fetch(v1("/homes"))
    if (!r.ok) return undefined
    const h = (await r.json()) as { current: string; mainId?: string; homes: { name: string; id?: string }[] }
    return h.current ? h.homes.find((x) => x.name === h.current)?.id : (h.mainId ?? "main")
  } catch {
    return undefined
  }
}

// Ce que ce navigateur garde d'un compte : brouillons (texte de conversation), et état d'affichage qui nomme ses dossiers et sa mémoire.
const OWNER = "gld.owner"
const userKey = (k: string) =>
  k === "gld.drafts" || k.startsWith("gld.editor.") || k === "gld.open" || k.startsWith("gld.mem.open.") || k === "ccMemSorts" || k === "gld.session-view"
const storedKeys = (): string[] => {
  try {
    return Object.keys(localStorage)
  } catch {
    return []
  }
}

// Le propriétaire est gardé dans le navigateur : un rechargement ne doit pas faire hériter un autre compte des données locales.
// Données sans propriétaire connu (antérieures au mode multi-utilisateur) : effacées par prudence.
function adopt(name: string) {
  const prev = storedOwner()
  // L'onglet compte aussi : un autre onglet a pu inscrire le nouveau compte comme propriétaire pendant que celui-ci gardait en
  // mémoire les données du précédent.
  const tabOther = snap.owner !== undefined && snap.owner !== name
  if (tabOther || (prev !== name && (prev !== null || storedKeys().some(userKey)))) clearUserData()
  try {
    localStorage.setItem(OWNER, name)
  } catch {
    /* stockage indisponible : la comparaison retombe sur l'onglet seul */
  }
}

const storedOwner = (): string | null => {
  try {
    return localStorage.getItem(OWNER)
  } catch {
    return null
  }
}

// Un onglet resté sur un compte n'écrit plus les brouillons quand un autre onglet a inscrit un autre propriétaire.
guardDrafts(() => snap.state === "open" || (snap.owner !== undefined && storedOwner() === snap.owner))

// Autre onglet connecté sous un autre compte (ou déconnecté) : celui-ci lâche les données en mémoire sans les réécrire et revient
// à la connexion.
if (typeof window !== "undefined")
  window.addEventListener("storage", (e) => {
    if (e.key !== OWNER && e.key !== null) return
    if (snap.owner === undefined || (e.key === null ? null : e.newValue) === snap.owner) return
    clearDrafts()
    resetGit()
    resetWarm()
    resetOpen()
    resetTrust()
    setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined, expired: false, owner: undefined, rights: undefined, homes: undefined, home: undefined })
  })

export function clearUserData() {
  clearDrafts()
  resetGit()
  resetWarm()
  resetOpen()
  resetTrust()
  for (const k of storedKeys()) if (userKey(k)) localStorage.removeItem(k)
}

// Fin de session voulue (déconnexion, mot de passe changé, session courante révoquée) : l'application est démontée et vidée.
export function signedOut() {
  clearUserData()
  try {
    localStorage.removeItem(OWNER)
  } catch {
    /* stockage indisponible */
  }
  setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined, expired: false, owner: undefined, rights: undefined, homes: undefined, home: undefined })
}

export async function logout(): Promise<void> {
  await authCall("/auth/logout")
  signedOut()
}

// Abandon d'une connexion en cours (retour depuis le second facteur) : la session partielle tombe, les données locales restent,
// car elles appartiennent au compte propriétaire, que la prochaine connexion départagera.
export async function abandon(): Promise<void> {
  await authCall("/auth/logout")
  setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined })
}

// Droit sur le home courant. Mode ouvert, ou home inconnu : permis, le refus du serveur (403) reste le dernier mot.
export function useCan(): (r: Right) => boolean {
  const a = useAuth()
  return (r) => a.state !== "ok" || !a.rights || !a.home || !!a.rights[a.home]?.includes(r)
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
// Par tranches : étaler tout le tampon dans fromCharCode dépasse la pile pour une grosse attestation.
const enc = (buf: ArrayBuffer): string => {
  const u = new Uint8Array(buf)
  let s = ""
  for (let i = 0; i < u.length; i += 0x8000) s += String.fromCharCode(...u.subarray(i, i + 0x8000))
  return btoa(s)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "")
}
export const b64u = { enc, dec }
type CredRef = { id: string; type: string; transports?: string[] }
type PKOptions = Record<string, unknown> & { challenge: string; user?: { id: string; name: string; displayName: string }; excludeCredentials?: CredRef[]; allowCredentials?: CredRef[] }
const refs = (l?: CredRef[]) => l?.map((c) => ({ ...c, id: dec(c.id) }))

// Le serveur déclare le nom d'hôte comme identifiant WebAuthn : une adresse IP n'en est jamais un, même si le navigateur la tient
// pour sûre (127.0.0.1).
const isIP = (h: string) => /^[\d.]+$/.test(h) || h.includes(":")
export const waAvailable = (host = location.hostname) =>
  typeof window !== "undefined" && window.isSecureContext && "PublicKeyCredential" in window && (host === "localhost" || !isIP(host))

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
