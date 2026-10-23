// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { setLang } from "@/i18n"
import { authFetch, b64u, boot, getAuth, logout, setAuth, useCan } from "@/lib/auth"
import { draftOf, setDraft } from "@/hooks/useDraft"
import { AuthScreen, Login } from "./Login"
import { Mfa } from "./Mfa"
import { Profile } from "./Profile"
import { AdminEvents, AdminUsers } from "./AdminUsers"
import App from "@/App"
import { TooltipProvider } from "@/components/ui/tooltip"

type Reply = { status?: number; body?: unknown; headers?: Record<string, string> }
type Call = { url: string; init: RequestInit }

// Faux serveur : une table url → réponse (ou fonction), et la liste des appels reçus.
function server(routes: Record<string, Reply | ((c: Call) => Reply)>) {
  const calls: Call[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const c = { url, init }
      calls.push(c)
      const key = Object.keys(routes).find((k) => url === k || url.startsWith(k + "?"))
      const r = key ? (typeof routes[key] === "function" ? (routes[key] as (c: Call) => Reply)(c) : (routes[key] as Reply)) : { status: 404, body: {} }
      return new Response(JSON.stringify(r.body ?? {}), { status: r.status ?? 200, headers: r.headers })
    })
  )
  return calls
}
const bodyOf = (c: Call) => JSON.parse(String(c.init.body ?? "{}")) as Record<string, unknown>
const headerOf = (c: Call, h: string) => new Headers(c.init.headers).get(h)

beforeEach(() => {
  setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined, methods: [], enroll: false, expired: false, owner: undefined })
  localStorage.clear()
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  )
})
afterEach(() => {
  cleanup()
  setLang("fr")
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const login = async (name = "alice", pw = "mot de passe") => {
  fireEvent.change(screen.getByLabelText("Nom d'utilisateur"), { target: { value: name } })
  fireEvent.change(screen.getByLabelText("Mot de passe"), { target: { value: pw } })
  await act(async () => void fireEvent.click(screen.getByRole("button", { name: "Se connecter" })))
}

describe("Login", () => {
  it("affiche une seule erreur générique, quel que soit le corps du 401", async () => {
    const bodies = [{ ok: false, out: "utilisateur inconnu" }, { ok: false, out: "mauvais mot de passe" }, {}]
    let i = 0
    server({ "/auth/login": () => ({ status: 401, body: bodies[i++] }) })
    render(<Login />)
    const seen: string[] = []
    for (let n = 0; n < bodies.length; n++) {
      await login()
      seen.push(screen.getByRole("alert").textContent ?? "")
    }
    expect(new Set(seen).size).toBe(1)
    expect(seen[0]).toContain("Identifiants invalides ou compte temporairement bloqué.")
    expect(document.body.textContent).not.toContain("inconnu")
    expect(document.body.textContent).not.toContain("mauvais")
  })

  it("Retry-After désactive l'envoi et affiche le décompte", async () => {
    server({ "/auth/login": { status: 401, body: { ok: false }, headers: { "Retry-After": "30" } } })
    render(<Login />)
    await login()
    expect(screen.getByRole("button", { name: "Se connecter" })).toHaveProperty("disabled", true)
    expect(screen.getByRole("alert").textContent).toContain("30 s")
  })

  it("aucun mot de passe dans l'URL ; un compte sans facteur passe à l'enrôlement", async () => {
    const calls = server({ "/auth/login": { body: { ok: true, mfa: [], enroll: true, csrf: "c1" } } })
    render(<AuthScreen />)
    await login("bob", "secret-tres-long")
    expect(calls[0].url).toBe("/auth/login")
    expect(calls[0].init.method).toBe("POST")
    expect(bodyOf(calls[0])).toEqual({ name: "bob", password: "secret-tres-long" })
    expect(getAuth()).toMatchObject({ state: "mfa", enroll: true, csrf: "c1" })
    expect(screen.getByText("Protégez votre compte")).toBeTruthy()
  })
})

describe("Mfa", () => {
  it("ne propose que les méthodes enrôlées et le champ du code de secours", () => {
    setAuth({ state: "mfa", methods: ["totp", "recovery"] })
    render(<Mfa />)
    const totp = screen.getByLabelText("Code de l'application (6 chiffres)")
    expect(totp.getAttribute("inputmode")).toBe("numeric")
    expect(totp.getAttribute("autocomplete")).toBe("one-time-code")
    expect(screen.getByLabelText("Code de secours")).toBeTruthy()
    expect(screen.queryByText("Utiliser une clé de sécurité")).toBeNull()
  })

  it("sans TOTP enrôlé, pas de champ TOTP", () => {
    setAuth({ state: "mfa", methods: ["recovery"] })
    render(<Mfa />)
    expect(screen.queryByLabelText("Code de l'application (6 chiffres)")).toBeNull()
    expect(screen.getByLabelText("Code de secours")).toBeTruthy()
  })
})

describe("jeton CSRF et session", () => {
  it("part sur les POST une fois connecté, jamais sur les GET", async () => {
    const calls = server({ "/x": { body: { ok: true, out: "" } }, "/api": { body: {} } })
    setAuth({ state: "ok", csrf: "tok-1" })
    await api.post("/x", {})
    await authFetch("/api")
    expect(headerOf(calls[0], "X-Gld-CSRF")).toBe("tok-1")
    expect(headerOf(calls[1], "X-Gld-CSRF")).toBeNull()
  })

  it("en mode ouvert (/auth/me en 404), aucun écran de connexion", async () => {
    server({ "/auth/me": { status: 404 } })
    setAuth({ state: "loading" })
    await boot()
    expect(getAuth().state).toBe("open")
  })

  it("/auth/me en 401 : connexion, sans autre sonde", async () => {
    const calls = server({ "/auth/me": { status: 401 } })
    await boot()
    expect(getAuth().state).toBe("anon")
    expect(calls.map((c) => c.url)).toEqual(["/auth/me"])
  })

  it("session complète : droits, homes et home courant exposés ; useCan suit le home courant", async () => {
    server({
      "/auth/me": { body: { ok: true, user: { name: "alice", admin: false }, mfa: true, csrf: "c", factors: { totp: true, webauthn: 0, recovery: 9, keys: [] }, rights: { h1: ["mem_read"], main: ["exec"] }, homes: [{ id: "h1", name: "perso" }, { id: "main", name: "main" }] } },
      "/homes": { body: { current: "perso", mainId: "main", homes: [{ name: "perso", id: "h1" }] } },
    })
    await boot()
    expect(getAuth()).toMatchObject({ state: "ok", home: "h1", rights: { h1: ["mem_read"] } })
    let can: (r: "exec" | "mem_read") => boolean = () => true
    function Probe() {
      can = useCan()
      return null
    }
    render(<Probe />)
    expect(can("mem_read")).toBe(true)
    expect(can("exec")).toBe(false)
  })

  it("/auth/me en 503 : écran indisponible avec bouton Réessayer", async () => {
    server({ "/auth/me": { status: 503 } })
    await boot()
    render(<AuthScreen />)
    expect(screen.getByText("Service indisponible")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Réessayer" })).toBeTruthy()
  })

  it("un 401 en cours de session ramène à la connexion, sans démonter l'application", async () => {
    server({ "/api": { status: 401, body: { ok: false } }, "/auth/password": { status: 401, headers: { "Retry-After": "2" } } })
    setAuth({ state: "ok", csrf: "tok", user: { name: "alice", admin: false } })
    await authFetch("/auth/password", { method: "POST" })
    expect(getAuth().state).toBe("ok") // un échec de vérification n'est pas une session perdue
    await authFetch("/api")
    expect(getAuth()).toMatchObject({ state: "anon", expired: true, csrf: "" })
    render(<AuthScreen />)
    expect(screen.getByText(/Session expirée/)).toBeTruthy()
  })
})

const EVIL = '<img src=x onerror="alert(1)">'
const USER = {
  id: 2,
  name: "bob",
  admin: false,
  disabled: false,
  created: 0,
  factors: { totp: true, webauthn: 0, recovery: 10 },
  sessions: 1,
  acl: [{ home_id: "main", home_name: "main", rights: ["mem_read"] }],
  tampered: false,
}

describe("App", () => {
  const mount = async () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("fr-FR") // l'application règle la langue sur le navigateur
    vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} }))
    setAuth({ state: "loading" })
    render(
      <TooltipProvider>
        <App />
      </TooltipProvider>
    )
    await act(() => new Promise((r) => setTimeout(r, 30)))
  }

  it("/auth/me en 404 : l'application s'affiche comme avant, sans connexion ni menu de compte", async () => {
    server({ "/auth/me": { status: 404 } })
    await mount()
    expect(screen.getAllByText("Récentes").length).toBeGreaterThan(0)
    expect(screen.queryByText("Connexion")).toBeNull()
    expect(screen.queryByRole("button", { name: /^Compte / })).toBeNull()
  })

  it("/auth/me en 401 : écran de connexion, application non montée", async () => {
    server({ "/auth/me": { status: 401 } })
    await mount()
    expect(screen.getByText("Connexion")).toBeTruthy()
    expect(screen.queryByText("Récentes")).toBeNull()
  })
})

describe("propriétaire des données locales", () => {
  const me = (name: string) => ({ body: { ok: true, user: { name, admin: false }, mfa: true, csrf: "c", factors: { totp: true, webauthn: 0, recovery: 9, keys: [] }, rights: {}, homes: [] } })
  const seed = (owner?: string) => {
    if (owner) localStorage.setItem("gld.owner", owner)
    localStorage.setItem("gld.drafts", JSON.stringify({ s1: { text: "brouillon", queue: [] } }))
    localStorage.setItem("gld.editor.memory", JSON.stringify({ key: "k", text: "x" }))
    localStorage.setItem("gld.open", JSON.stringify({ "/home/a/projet": true }))
    localStorage.setItem("gld.mem.open.Projet", "1")
    localStorage.setItem("ccMemSorts", "{}")
    localStorage.setItem("gld.session-view", "brut")
    localStorage.setItem("ccFont", "inter")
  }
  const userKeys = ["gld.drafts", "gld.editor.memory", "gld.open", "gld.mem.open.Projet", "ccMemSorts", "gld.session-view"]

  it("rechargement puis autre compte : données effacées, nouveau propriétaire enregistré, préférences gardées", async () => {
    seed("alice")
    server({ "/auth/me": me("bob"), "/homes": { body: { current: "", homes: [] } } })
    await boot()
    for (const k of userKeys) expect(localStorage.getItem(k)).toBeNull()
    expect(localStorage.getItem("gld.owner")).toBe("bob")
    expect(localStorage.getItem("ccFont")).toBe("inter")
  })

  it("rechargement puis même compte : données gardées", async () => {
    seed("alice")
    server({ "/auth/me": me("alice"), "/homes": { body: { current: "", homes: [] } } })
    await boot()
    for (const k of userKeys) expect(localStorage.getItem(k)).not.toBeNull()
    expect(localStorage.getItem("gld.owner")).toBe("alice")
  })

  it("données sans propriétaire en mode authentifié : effacées", async () => {
    seed()
    server({ "/auth/me": me("alice"), "/homes": { body: { current: "", homes: [] } } })
    await boot()
    for (const k of userKeys) expect(localStorage.getItem(k)).toBeNull()
    expect(localStorage.getItem("gld.owner")).toBe("alice")
  })

  it("mode ouvert : rien lu, rien écrit, rien effacé", async () => {
    seed()
    server({ "/auth/me": { status: 404 } })
    const get = vi.spyOn(Storage.prototype, "getItem")
    await boot()
    expect(get).not.toHaveBeenCalledWith("gld.owner")
    for (const k of userKeys) expect(localStorage.getItem(k)).not.toBeNull()
    expect(localStorage.getItem("gld.owner")).toBeNull()
  })

  it("déconnexion : données et propriétaire effacés", async () => {
    seed("alice")
    server({ "/auth/logout": { body: { ok: true } } })
    await logout()
    for (const k of [...userKeys, "gld.owner"]) expect(localStorage.getItem(k)).toBeNull()
  })

  it("retour depuis le second facteur pendant une reconnexion : la session partielle tombe, les brouillons restent", async () => {
    seed("alice")
    const calls = server({ "/auth/logout": { body: { ok: true } } })
    setAuth({ state: "mfa", methods: ["totp"], expired: true, owner: "alice", csrf: "c" })
    render(<Mfa />)
    await act(async () => void fireEvent.click(screen.getByRole("button", { name: "Revenir à la connexion" })))
    expect(calls.some((c) => c.url === "/auth/logout")).toBe(true)
    expect(getAuth()).toMatchObject({ state: "anon", expired: true, owner: "alice" })
    for (const k of userKeys) expect(localStorage.getItem(k)).not.toBeNull()
  })
})

describe("base64url", () => {
  it("aller-retour d'un tampon de plus de 200 Ko", () => {
    const u = new Uint8Array(256 * 1024).map((_, i) => (i * 31 + 7) & 255)
    const s = b64u.enc(u.buffer)
    expect(s).not.toMatch(/[+/=]/)
    expect(new Uint8Array(b64u.dec(s))).toEqual(u)
  })
})

describe("Profile", () => {
  it("liste les clés par libellé, marque une clé altérée et retire une clé par son identifiant", async () => {
    const calls = server({ "/auth/sessions": { body: { ok: true, sessions: [] } }, "/auth/factor/remove": { body: { ok: true } }, "/auth/me": { status: 401 } })
    const keys = [
      { id: 7, label: "clé USB", created: 0, bad: false },
      { id: 9, label: EVIL, created: 0, bad: true },
    ]
    setAuth({ state: "ok", csrf: "tok", user: { name: "alice", admin: false }, factors: { totp: true, webauthn: 2, recovery: 9, keys } })
    const { container } = render(<Profile />)
    expect(screen.getByText("clé USB")).toBeTruthy()
    expect(screen.getByText(EVIL)).toBeTruthy()
    expect(container.querySelector("img")).toBeNull()
    expect(screen.getByText("altéré")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Retirer la clé clé USB" }))
    fireEvent.change(screen.getByLabelText("Mot de passe actuel"), { target: { value: "pw" } })
    fireEvent.change(screen.getByLabelText("Code (application ou secours)"), { target: { value: "123456" } })
    await act(async () => void fireEvent.click(screen.getByRole("button", { name: "Confirmer" })))
    expect(bodyOf(calls.find((c) => c.url === "/auth/factor/remove")!)).toEqual({ password: "pw", code: "123456", kind: "webauthn", id: 7 })
  })
})

describe("changement de compte dans l'onglet", () => {
  const me = (name: string) => ({ body: { ok: true, user: { name, admin: false }, mfa: true, csrf: "c-" + name, factors: { totp: true, webauthn: 0, recovery: 9, keys: [] }, rights: { main: ["sessions_read", "exec"] }, homes: [{ id: "main", name: "main" }] } })
  let who = "alice"
  const setup = async () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("fr-FR")
    vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} }))
    server({ "/auth/me": () => me(who), "/homes": { body: { current: "", mainId: "main", homes: [] } }, "/gone": { status: 401 } })
    who = "alice"
    localStorage.clear()
    setAuth({ state: "loading", owner: undefined })
    render(
      <TooltipProvider>
        <App />
      </TooltipProvider>
    )
    await act(() => new Promise((r) => setTimeout(r, 30)))
    setDraft("s1", { text: "secret d'alice" })
    localStorage.setItem("gld.editor.memory", JSON.stringify({ key: "k", text: "brouillon" }))
    localStorage.setItem("ccFont", "inter")
    return screen.getAllByRole("heading", { level: 2 })[0]
  }
  const expireThenLogin = async (name: string) => {
    await act(async () => void (await authFetch("/gone")))
    expect(getAuth()).toMatchObject({ state: "anon", expired: true })
    expect(screen.getByText(/Session expirée/)).toBeTruthy()
    who = name
    await act(() => boot())
    await act(() => new Promise((r) => setTimeout(r, 30)))
  }

  it("même compte après expiration : l'application et les brouillons sont conservés", async () => {
    const head = await setup()
    await expireThenLogin("alice")
    expect(screen.getAllByRole("heading", { level: 2 })[0]).toBe(head)
    expect(draftOf("s1").text).toBe("secret d'alice")
    expect(localStorage.getItem("gld.editor.memory")).not.toBeNull()
  })

  it("autre compte après expiration : application remontée à neuf, brouillons effacés, préférences gardées", async () => {
    const head = await setup()
    await expireThenLogin("bob")
    expect(getAuth().owner).toBe("bob")
    expect(head.isConnected).toBe(false)
    expect(screen.getAllByText("Récentes").length).toBeGreaterThan(0)
    expect(draftOf("s1").text).toBe("")
    expect(localStorage.getItem("gld.drafts")).toBeNull()
    expect(localStorage.getItem("gld.editor.memory")).toBeNull()
    expect(localStorage.getItem("ccFont")).toBe("inter")
  })

  it("déconnexion : application démontée et brouillons effacés", async () => {
    await setup()
    await act(() => logout())
    expect(screen.queryByText("Récentes")).toBeNull()
    expect(screen.getByText("Connexion")).toBeTruthy()
    expect(draftOf("s1").text).toBe("")
    expect(localStorage.getItem("gld.editor.memory")).toBeNull()
  })
})

describe("AdminUsers", () => {
  it("demande confirmation et réauthentification avant d'accorder exec, avec l'avertissement", async () => {
    const calls = server({
      "/admin/users": { body: { ok: true, users: [USER] } },
      "/admin/homes": { body: { ok: true, homes: [{ id: "main", name: "main" }] } },
      "/admin/acl": { body: { ok: true, rights: ["mem_read", "exec"] } },
    })
    setAuth({ state: "ok", csrf: "tok", user: { name: "root", admin: true }, factors: { totp: true, webauthn: 0, recovery: 10 } })
    render(<AdminUsers />)
    fireEvent.click(await screen.findByRole("button", { name: "bob" }))
    expect(screen.getByText(/exec et config_write équivalent au compte système de gld/)).toBeTruthy()
    await act(async () => void fireEvent.click(screen.getByRole("checkbox", { name: "exec sur Dossier principal" })))
    expect(calls.some((c) => c.url === "/admin/acl")).toBe(false)
    expect(screen.getByText("exec sur Dossier principal équivaut au compte système de gld.")).toBeTruthy()

    fireEvent.change(screen.getByLabelText("Mot de passe actuel"), { target: { value: "pw-admin" } })
    fireEvent.change(screen.getByLabelText("Code (application ou secours)"), { target: { value: "123456" } })
    const ok = screen.getByRole("button", { name: "Confirmer" })
    expect(ok).toHaveProperty("disabled", true) // case de confirmation non cochée
    fireEvent.click(screen.getByRole("checkbox", { name: "Je confirme accorder ce droit en connaissance de cause" }))
    await act(async () => void fireEvent.click(ok))
    const acl = calls.find((c) => c.url === "/admin/acl")!
    expect(bodyOf(acl)).toEqual({ reauth: { password: "pw-admin", code: "123456" }, user_id: 2, home_id: "main", grant: ["exec"], confirm: true })
    expect(headerOf(acl, "X-Gld-CSRF")).toBe("tok")
  })

  it("un droit ordinaire s'accorde sans réauthentification", async () => {
    const calls = server({
      "/admin/users": { body: { ok: true, users: [USER] } },
      "/admin/homes": { body: { ok: true, homes: [{ id: "main", name: "main" }] } },
      "/admin/acl": { body: { ok: true, rights: ["mem_read", "mem_write"] } },
    })
    setAuth({ state: "ok", csrf: "tok", user: { name: "root", admin: true } })
    render(<AdminUsers />)
    fireEvent.click(await screen.findByRole("button", { name: "bob" }))
    await act(async () => void fireEvent.click(screen.getByRole("checkbox", { name: "mem_write sur Dossier principal" })))
    expect(bodyOf(calls.find((c) => c.url === "/admin/acl")!)).toEqual({ user_id: 2, home_id: "main", grant: ["mem_write"] })
  })

  it("fermer ses propres sessions déconnecte proprement l'interface", async () => {
    localStorage.setItem("gld.owner", "root")
    localStorage.setItem("gld.drafts", "{}")
    server({
      "/admin/users": { body: { ok: true, users: [{ ...USER, id: 1, name: "root", admin: true }] } },
      "/admin/homes": { body: { ok: true, homes: [{ id: "main", name: "main" }] } },
      "/admin/sessions/revoke": { body: { ok: true } },
    })
    setAuth({ state: "ok", csrf: "tok", user: { name: "root", admin: true }, owner: "root" })
    render(<AdminUsers />)
    const trigger = await screen.findByRole("button", { name: "Actions pour root" })
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: "mouse" })
    await act(async () => void fireEvent.click(await screen.findByRole("menuitem", { name: "Fermer toutes ses sessions" })))
    await waitFor(() => expect(getAuth()).toMatchObject({ state: "anon", owner: undefined, expired: false }))
    expect(localStorage.getItem("gld.owner")).toBeNull()
    expect(localStorage.getItem("gld.drafts")).toBeNull()
  })

  it("rend les chaînes venues d'un attaquant en texte", async () => {
    server({
      "/admin/users": { body: { ok: true, users: [{ ...USER, name: EVIL }] } },
      "/admin/homes": { body: { ok: true, homes: [{ id: "main", name: "main" }] } },
      "/admin/events": { body: { ok: true, events: [{ id: 1, at: 0, user_id: 0, user: "", name_tried: EVIL, ip: "1.2.3.4", kind: "login", outcome: EVIL }] } },
    })
    setAuth({ state: "ok", csrf: "tok", user: { name: "root", admin: true } })
    const { container } = render(
      <>
        <AdminUsers />
        <AdminEvents />
      </>
    )
    await waitFor(() => expect(screen.getAllByText(EVIL).length).toBe(3))
    expect(container.querySelector("img")).toBeNull()
  })
})
