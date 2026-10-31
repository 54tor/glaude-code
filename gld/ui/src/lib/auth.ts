import { useEffect, useState, useSyncExternalStore } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { clearDrafts, guardDrafts } from "@/hooks/useDraft"
import { resetGit } from "@/hooks/useGit"
import { resetWarm } from "@/hooks/useKeepWarm"
import { resetOpen } from "@/hooks/useOpen"
import { ApiError, netError, qs, readResponse, v1 } from "@/lib/apiError"
import { http } from "@/lib/http"
import { resetTrust } from "@/lib/trust"

// Mode multi-utilisateur (auth_routes.go, auth_admin.go) : état de connexion partagé, appels vers /v1/auth/* et /v1/admin/*, WebAuthn.

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

let authSnapshot: Auth = { state: "loading", csrf: "", methods: [], enroll: false, expired: false }
const subscribers = new Set<() => void>()

export function setAuth(changes: Partial<Auth>) {
  authSnapshot = { ...authSnapshot, ...changes }
  subscribers.forEach((notify) => notify())
}
export const getAuth = () => authSnapshot
export function useAuth(): Auth {
  return useSyncExternalStore(
    (notify) => {
      subscribers.add(notify)
      return () => void subscribers.delete(notify)
    },
    getAuth
  )
}

// Tout appel au serveur passe ici : jeton CSRF sur les requêtes modifiantes, retour à la connexion si la session tombe.
// Un 401 portant Retry-After est un échec de vérification (mot de passe, code), pas une session perdue.
// Hors session établie, seules les routes de connexion modifient : une file d'attente de l'application restée montée ne part pas
// avec le jeton d'un compte en cours de connexion.
const HTTP_UNAUTHORIZED = 401

export async function authFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const method = (init.method ?? "GET").toUpperCase()
  const isModifying = method !== "GET" && method !== "HEAD"
  const hasEstablishedSession = authSnapshot.state === "ok" || authSnapshot.state === "open"
  const isLoginRoute = url.startsWith("/v1/auth/")
  if (isModifying && !hasEstablishedSession && !isLoginRoute) throw new Error(i18n.t("auth.notReady"))
  if (authSnapshot.csrf && isModifying) {
    const headers = new Headers(init.headers)
    headers.set("X-Gld-CSRF", authSnapshot.csrf)
    init = { ...init, headers }
  }
  if (authSnapshot.share) {
    const headers = new Headers(init.headers)
    headers.set("X-Gld-Share", authSnapshot.share)
    init = { ...init, headers }
  }
  const response = await fetch(url, init)
  const isSessionLost = response.status === HTTP_UNAUTHORIZED && authSnapshot.state === "ok" && !response.headers.has("Retry-After")
  if (isSessionLost) setAuth({ state: "anon", csrf: "", expired: true })
  return response
}

const methodsOf = (factors: Factors) => {
  const methods: string[] = []
  if (factors.totp) methods.push("totp")
  if (factors.webauthn) methods.push("webauthn")
  if (factors.recovery) methods.push("recovery")
  return methods
}

// Au démarrage et après chaque étape de connexion. 404 : mode ouvert, l'application s'affiche comme sans comptes.
// Une panne pendant un rafraîchissement (session établie) laisse l'application en place : seul un démarrage mène à « indisponible ».
export async function boot(): Promise<void> {
  const reportServerDown = () => {
    if (authSnapshot.state === "ok") {
      toast.error(i18n.t("auth.refreshFailed"))
      return
    }
    setAuth({ state: "unavailable" })
  }
  let me: { user: AuthUser; mfa: boolean; csrf: string; factors: Factors; rights?: Record<string, Right[]>; homes?: HomeRef[] }
  try {
    me = await readResponse<typeof me>(await fetch(v1("/auth/me")))
  } catch (bootError) {
    const status = bootError instanceof ApiError ? bootError.status : 0
    if (status === 404) return setAuth({ state: "open" })
    if (status === HTTP_UNAUTHORIZED) return setAuth({ state: "anon", csrf: "", user: undefined })
    return reportServerDown()
  }
  const factors = me.factors
  const signedInState = { user: me.user, csrf: me.csrf, factors, methods: methodsOf(factors), enroll: !factors.totp && !factors.webauthn }
  if (!me.mfa) return setAuth({ ...signedInState, state: "mfa" })
  adopt(me.user.name)
  setAuth({ ...signedInState, state: "ok", expired: false, owner: me.user.name, rights: me.rights ?? {}, homes: me.homes ?? [], home: await currentHome() })
}

// /homes donne le nom du home servi ("" : le principal) ; les droits sont rangés par identifiant.
async function currentHome(): Promise<string | undefined> {
  try {
    const response = await fetch(v1("/homes"))
    if (!response.ok) return undefined
    const homesInfo = (await response.json()) as { current: string; mainId?: string; homes: { name: string; id?: string }[] }
    if (!homesInfo.current) return homesInfo.mainId ?? "main"
    return homesInfo.homes.find((home) => home.name === homesInfo.current)?.id
  } catch {
    return undefined
  }
}

// Ce que ce navigateur garde d'un compte : brouillons (texte de conversation), et état d'affichage qui nomme ses dossiers et sa mémoire.
const OWNER = "gld.owner"
const USER_STORAGE_KEYS = ["gld.drafts", "gld.open", "ccMemSorts", "gld.session-view"]
const USER_STORAGE_PREFIXES = ["gld.editor.", "gld.mem.open.", "gld.ide."]
const userKey = (storageKey: string) =>
  USER_STORAGE_KEYS.includes(storageKey) || USER_STORAGE_PREFIXES.some((prefix) => storageKey.startsWith(prefix))
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
  const previousOwner = storedOwner()
  // L'onglet compte aussi : un autre onglet a pu inscrire le nouveau compte comme propriétaire pendant que celui-ci gardait en
  // mémoire les données du précédent.
  const tabHeldOtherAccount = authSnapshot.owner !== undefined && authSnapshot.owner !== name
  const hasUnownedOrForeignData = previousOwner !== name && (previousOwner !== null || storedKeys().some(userKey))
  if (tabHeldOtherAccount || hasUnownedOrForeignData) clearUserData()
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
guardDrafts(() => authSnapshot.state === "open" || (authSnapshot.owner !== undefined && storedOwner() === authSnapshot.owner))

// Autre onglet connecté sous un autre compte (ou déconnecté) : celui-ci lâche les données en mémoire sans les réécrire et revient
// à la connexion.
if (typeof window !== "undefined")
  window.addEventListener("storage", (storageEvent) => {
    const isClear = storageEvent.key === null
    if (storageEvent.key !== OWNER && !isClear) return
    const newOwner = isClear ? null : storageEvent.newValue
    if (authSnapshot.owner === undefined || newOwner === authSnapshot.owner) return
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
  for (const storageKey of storedKeys()) {
    if (userKey(storageKey)) localStorage.removeItem(storageKey)
  }
  try {
    for (const storageKey of Object.keys(sessionStorage)) {
      if (storageKey.startsWith("gld.ide.")) sessionStorage.removeItem(storageKey)
    }
  } catch {
    /* stockage indisponible */
  }
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
  await http.del("/auth/sessions/current").catch(() => {})
  signedOut()
}

// Abandon d'une connexion en cours (retour depuis le second facteur) : la session partielle tombe, les données locales restent,
// car elles appartiennent au compte propriétaire, que la prochaine connexion départagera.
export async function abandon(): Promise<void> {
  await http.del("/auth/sessions/current").catch(() => {})
  setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined })
}

// Droit sur le home courant. Mode ouvert, ou home inconnu : permis, le refus du serveur (403) reste le dernier mot.
export function useCan(): (right: Right) => boolean {
  const auth = useAuth()
  return (right) => {
    if (auth.state !== "ok" || !auth.rights || !auth.home) return true
    return !!auth.rights[auth.home]?.includes(right)
  }
}

const WAIT_TICK_MS = 250

// Attente imposée par Retry-After : secondes restantes, et de quoi la déclencher.
export function useWait(): [number, (sec: number) => void] {
  const [until, setUntil] = useState(0)
  const [now, setNow] = useState(0)
  useEffect(() => {
    if (until <= now) return
    const tickTimer = setTimeout(() => setNow(Date.now()), WAIT_TICK_MS)
    return () => clearTimeout(tickTimer)
  }, [until, now])
  const startWait = (sec: number) => {
    const startedAt = Date.now()
    setNow(startedAt)
    setUntil(sec > 0 ? startedAt + sec * 1000 : 0)
  }
  const remainingSeconds = Math.max(0, Math.ceil((until - now) / 1000))
  return [remainingSeconds, startWait]
}

// WebAuthn sans bibliothèque : go-webauthn émet et attend le JSON standard, binaires en base64url.
const BASE64_BLOCK_LENGTH = 4
const dec = (encoded: string): ArrayBuffer => {
  const paddedLength = Math.ceil(encoded.length / BASE64_BLOCK_LENGTH) * BASE64_BLOCK_LENGTH
  const binary = atob(encoded.replace(/-/g, "+").replace(/_/g, "/").padEnd(paddedLength, "="))
  return Uint8Array.from(binary, (character) => character.charCodeAt(0)).buffer
}
// Par tranches : étaler tout le tampon dans fromCharCode dépasse la pile pour une grosse attestation.
const FROM_CHAR_CODE_CHUNK = 0x8000
const enc = (buffer: ArrayBuffer): string => {
  const bytes = new Uint8Array(buffer)
  let binary = ""
  for (let chunkStart = 0; chunkStart < bytes.length; chunkStart += FROM_CHAR_CODE_CHUNK) {
    binary += String.fromCharCode(...bytes.subarray(chunkStart, chunkStart + FROM_CHAR_CODE_CHUNK))
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "")
}
export const b64u = { enc, dec }
type CredRef = { id: string; type: string; transports?: string[] }
type PKOptions = Record<string, unknown> & { challenge: string; user?: { id: string; name: string; displayName: string }; excludeCredentials?: CredRef[]; allowCredentials?: CredRef[] }
const decodeCredentialRefs = (credentialRefs?: CredRef[]) => credentialRefs?.map((credentialRef) => ({ ...credentialRef, id: dec(credentialRef.id) }))

// Le serveur déclare le nom d'hôte comme identifiant WebAuthn : une adresse IP n'en est jamais un, même si le navigateur la tient
// pour sûre (127.0.0.1).
const isIP = (hostname: string) => /^[\d.]+$/.test(hostname) || hostname.includes(":")
export const waAvailable = (host = location.hostname) =>
  typeof window !== "undefined" && window.isSecureContext && "PublicKeyCredential" in window && (host === "localhost" || !isIP(host))

async function waCreate(options: { publicKey: PKOptions }) {
  const publicKey = options.publicKey
  const creationOptions = {
    ...publicKey,
    challenge: dec(publicKey.challenge),
    user: { ...publicKey.user!, id: dec(publicKey.user!.id) },
    excludeCredentials: decodeCredentialRefs(publicKey.excludeCredentials),
  } as unknown as PublicKeyCredentialCreationOptions
  const credential = (await navigator.credentials.create({ publicKey: creationOptions })) as PublicKeyCredential
  const attestation = credential.response as AuthenticatorAttestationResponse
  return {
    id: credential.id,
    rawId: enc(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: { attestationObject: enc(attestation.attestationObject), clientDataJSON: enc(attestation.clientDataJSON), transports: attestation.getTransports?.() ?? [] },
  }
}

async function waGet(options: { publicKey: PKOptions }) {
  const publicKey = options.publicKey
  const requestOptions = {
    ...publicKey,
    challenge: dec(publicKey.challenge),
    allowCredentials: decodeCredentialRefs(publicKey.allowCredentials),
  } as unknown as PublicKeyCredentialRequestOptions
  const credential = (await navigator.credentials.get({ publicKey: requestOptions })) as PublicKeyCredential
  const assertion = credential.response as AuthenticatorAssertionResponse
  return {
    id: credential.id,
    rawId: enc(credential.rawId),
    type: credential.type,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      authenticatorData: enc(assertion.authenticatorData),
      clientDataJSON: enc(assertion.clientDataJSON),
      signature: enc(assertion.signature),
      userHandle: assertion.userHandle ? enc(assertion.userHandle) : undefined,
    },
  }
}

// Les deux cérémonies lèvent ApiError ; un refus ou une annulation côté navigateur devient une ApiError sans statut (réseau).
// Assertion sur la session courante : second facteur à la connexion, ou réauthentification sans code (cinq minutes).
export async function waAssert(): Promise<{ csrf: string }> {
  const challenge = await http.post<{ options: { publicKey: PKOptions } }>("/auth/mfa/webauthn/challenges")
  let credential: Awaited<ReturnType<typeof waGet>>
  try {
    credential = await waGet(challenge.options)
  } catch (ceremonyError) {
    throw netError(ceremonyError)
  }
  return http.post("/auth/mfa/webauthn/assertions", credential)
}

export async function waRegister(label: string, reauth?: Reauth): Promise<{ csrf: string; recovery?: string[] }> {
  const registration = await http.post<{ options: { publicKey: PKOptions } }>("/auth/webauthn/registrations", reauth ?? {})
  let credential: Awaited<ReturnType<typeof waCreate>>
  try {
    credential = await waCreate(registration.options)
  } catch (ceremonyError) {
    throw netError(ceremonyError)
  }
  return http.put(`/auth/webauthn/registrations/current${qs({ label })}`, credential)
}
