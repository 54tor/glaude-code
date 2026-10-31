// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type McpServer, type Session } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import type { Route } from "@/lib/route"
import * as utils from "@/lib/utils"
import { ActionsCtx, type Actions, type Row } from "./actions"
import { Dashboard } from "./Dashboard"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const HOME = "/home/bob"
const NOW = 1_800_000_000
const flush = () => act(async () => {})
const session = (id: string, overrides: Partial<Session> = {}): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: HOME, mtime: NOW - 120, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 }, ...overrides })
const row = (id: string, overrides: Partial<Session> = {}): Row => ({ s: session(id, overrides), g: { project: `${HOME}/Projets/a`, missing: false, sessions: [] } })
const server = (name: string): McpServer => ({ name, scope: "user", type: "stdio" })
const update = (overrides: Partial<Awaited<ReturnType<typeof api.updateInfo>>> = {}) => ({ current: "v0.9.0", latest: "v0.9.0", url: "https://github.com/example/glaude-code/releases/tag/v0.9.1", newer: false, ...overrides })

type Props = { open?: Row[]; closed?: Row[]; waiting?: string[] }
const mount = async (props: Props = {}) => {
  const go = vi.fn<(route: Route) => void>()
  const read = vi.fn()
  const actions = { home: HOME, favs: new Set(), picked: new Set(), selected: [], live: new Set(), vgroups: [], inGroup: null, caps: () => ({}) } as unknown as Actions
  render(
    <ActionsCtx.Provider value={actions}>
      <Dashboard open={props.open ?? []} closed={props.closed ?? []} waiting={new Set(props.waiting ?? [])} now={NOW} home={HOME} go={go} read={read} />
    </ActionsCtx.Provider>
  )
  await flush()
  return { go, read }
}

beforeEach(() => {
  vi.spyOn(api, "configMcp").mockResolvedValue({ servers: [] })
  vi.spyOn(api, "configMcpCheck").mockResolvedValue({ status: {} })
  vi.spyOn(api, "updateInfo").mockResolvedValue(update())
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Dashboard — sessions", () => {
  it("cartes vides : messages dédiés et compteurs à zéro", async () => {
    await mount()
    expect(screen.getByText("Aucune session ouverte.")).toBeTruthy()
    expect(screen.getByText("Aucune session récente au cache.")).toBeTruthy()
    expect(screen.getByText(/Sessions ouvertes \(0\)/)).toBeTruthy()
  })

  it("une session : titre, dossier abrégé, ancienneté ; un clic la lit", async () => {
    const { read } = await mount({ open: [row("1", { title: "Migration REST", mtime: NOW - 7200 })] })
    const sessionButton = screen.getByRole("button", { name: /Migration REST/ })
    expect(sessionButton.textContent).toContain("~/Projets/a")
    fireEvent.click(sessionButton)
    expect(read).toHaveBeenCalledWith(expect.objectContaining({ id: "1" }))
  })

  it("session qui attend : pastille « attend »", async () => {
    await mount({ open: [row("1"), row("2")], waiting: ["2"] })
    expect(screen.getAllByText("attend")).toHaveLength(1)
    expect(within(screen.getByRole("button", { name: /Titre 2/ })).getByText("attend")).toBeTruthy()
  })

  it("limite à huit lignes par carte, le compteur garde le total", async () => {
    const many = Array.from({ length: 11 }, (_, index) => row(`s${index}`))
    await mount({ closed: many })
    expect(screen.getByText(/Dernières fermées \(11\)/)).toBeTruthy()
    expect(screen.getAllByRole("button", { name: /Titre s/ })).toHaveLength(8)
  })

  it("« Tout voir » : sessions ouvertes → active, fermées → recent", async () => {
    const { go } = await mount()
    const seeAll = screen.getAllByRole("button", { name: "Tout voir" })
    fireEvent.click(seeAll[0])
    expect(go).toHaveBeenLastCalledWith({ kind: "active" })
    fireEvent.click(seeAll[1])
    expect(go).toHaveBeenLastCalledWith({ kind: "recent" })
  })

  it("clic droit sur une session : son menu complet", async () => {
    await mount({ open: [row("1")] })
    fireEvent.contextMenu(screen.getByRole("button", { name: /Titre 1/ }))
    expect(await screen.findByText("Lire la conversation")).toBeTruthy()
  })
})

describe("Dashboard — serveurs MCP", () => {
  it("attente de la liste : indicateur de chargement, pas de message « aucun »", async () => {
    vi.mocked(api.configMcp).mockReturnValue(new Promise(() => {}))
    await mount()
    expect(screen.queryByText("Aucun serveur MCP.")).toBeNull()
  })

  it("aucun serveur : message dédié", async () => {
    await mount()
    expect(screen.getByText("Aucun serveur MCP.")).toBeTruthy()
  })

  it("liste fusionnée configurés + vérifiés, triée, sans doublon", async () => {
    vi.mocked(api.configMcp).mockResolvedValue({ servers: [server("zeta"), server("alpha")] })
    vi.mocked(api.configMcpCheck).mockResolvedValue({ status: { alpha: "✔ Connected", extra: "✗ Failed" } })
    await mount()
    expect(screen.getByText(/Serveurs MCP \(3\)/)).toBeTruthy()
    const names = screen.getAllByRole("listitem").map((item) => item.textContent)
    expect(names).toEqual(["●alpha", "●extra", "●zeta"])
  })

  it("couleur selon l'état : connecté, en échec, jamais vérifié", async () => {
    vi.mocked(api.configMcp).mockResolvedValue({ servers: [server("ok"), server("ko"), server("inconnu")] })
    vi.mocked(api.configMcpCheck).mockResolvedValue({ status: { ok: "✔ Connected", ko: "✗ Failed to connect" } })
    await mount()
    const dot = (name: string) => screen.getByText(name).previousElementSibling as HTMLElement
    expect(dot("ok").className).toContain("text-success")
    expect(dot("ko").className).toContain("text-warning")
    expect(dot("inconnu").className).toContain("text-warning")
  })

  it("sans réponse de vérification : points neutres tant que l'état est inconnu", async () => {
    vi.mocked(api.configMcp).mockResolvedValue({ servers: [server("a")] })
    vi.mocked(api.configMcpCheck).mockReturnValue(new Promise(() => {}))
    await mount()
    expect((screen.getByText("a").previousElementSibling as HTMLElement).className).toContain("text-muted-foreground")
  })

  it("échec de la vérification : l'état devient vide, pas d'exception", async () => {
    vi.mocked(api.configMcp).mockResolvedValue({ servers: [server("a")] })
    vi.mocked(api.configMcpCheck).mockRejectedValue(new ApiError(500, "boom", "x"))
    await mount()
    expect(screen.getByText("a")).toBeTruthy()
  })

  it("échec de la lecture des serveurs : liste vide", async () => {
    vi.mocked(api.configMcp).mockRejectedValue(new ApiError(500, "boom", "x"))
    await mount()
    expect(screen.getByText("Aucun serveur MCP.")).toBeTruthy()
  })

  it("le bouton de revérification relance la vérification et se désactive pendant", async () => {
    let resolveCheck: (value: { status: Record<string, string> }) => void = () => {}
    vi.mocked(api.configMcpCheck).mockReturnValueOnce(Promise.resolve({ status: {} })).mockReturnValueOnce(new Promise((resolve) => (resolveCheck = resolve)))
    await mount()
    const recheck = screen.getByRole("button", { name: "Revérifier l'état" }) as HTMLButtonElement
    expect(recheck.disabled).toBe(false)
    fireEvent.click(recheck)
    expect(api.configMcpCheck).toHaveBeenCalledTimes(2)
    expect(recheck.disabled).toBe(true)
    resolveCheck({ status: {} })
    await flush()
    expect(recheck.disabled).toBe(false)
  })

  it("menu d'un serveur : ouvrir dans Extensions, copier le nom", async () => {
    vi.mocked(api.configMcp).mockResolvedValue({ servers: [server("alpha")] })
    const copySpy = vi.spyOn(utils, "copyText").mockResolvedValue(undefined as never)
    const toastSuccess = vi.spyOn(toast, "success").mockReturnValue("")
    const { go } = await mount()
    fireEvent.contextMenu(screen.getByText("alpha"))
    fireEvent.click(await screen.findByText("Ouvrir dans Extensions"))
    expect(go).toHaveBeenCalledWith({ kind: "extensions" })
    fireEvent.contextMenu(screen.getByText("alpha"))
    fireEvent.click(await screen.findByText("Copier le nom"))
    await flush()
    expect(copySpy).toHaveBeenCalledWith("alpha")
    expect(toastSuccess).toHaveBeenCalledWith("Copié")
  })

  it("« Tout voir » du bloc MCP mène aux extensions", async () => {
    const { go } = await mount()
    fireEvent.click(screen.getAllByRole("button", { name: "Tout voir" })[2])
    expect(go).toHaveBeenCalledWith({ kind: "extensions" })
  })
})

describe("Dashboard — mises à jour", () => {
  it("version inconnue tant que la réponse n'est pas là", async () => {
    vi.mocked(api.updateInfo).mockReturnValue(new Promise(() => {}))
    await mount()
    expect(screen.getByText("Version non vérifiée.")).toBeTruthy()
  })

  it("à jour : version courante", async () => {
    await mount()
    expect(screen.getByText("À jour : v0.9.0")).toBeTruthy()
  })

  it("version plus récente : message et lien sûr vers la release", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue(update({ newer: true, latest: "v0.9.1" }))
    await mount()
    expect(screen.getByText(/Nouvelle version disponible : v0.9.1 \(vous avez v0.9.0\)/)).toBeTruthy()
    const releaseLink = screen.getByRole("link", { name: "Voir" })
    expect(releaseLink.getAttribute("href")).toContain("/releases/tag/v0.9.1")
    expect(releaseLink.getAttribute("rel")).toContain("noopener")
  })

  it("Claude Code : à jour, périmé, ou absent", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue(update({ claudeCurrent: "2.1.0", claudeLatest: "2.1.0", claudeNewer: false }))
    await mount()
    expect(screen.getByText("Claude Code à jour : 2.1.0")).toBeTruthy()
    cleanup()
    vi.mocked(api.updateInfo).mockResolvedValue(update({ claudeCurrent: "2.0.0", claudeLatest: "2.1.0", claudeNewer: true }))
    await mount()
    expect(screen.getByText(/Claude Code 2.0.0 n'est pas à jour \(2.1.0 disponible\)/)).toBeTruthy()
    cleanup()
    vi.mocked(api.updateInfo).mockResolvedValue(update())
    await mount()
    expect(screen.queryByText(/Claude Code/)).toBeNull()
  })

  it("« Vérifier » force la relecture et remplace l'information", async () => {
    await mount()
    vi.mocked(api.updateInfo).mockResolvedValue(update({ current: "v0.9.0", latest: "v0.9.2", newer: true }))
    fireEvent.click(screen.getByRole("button", { name: /Vérifier/ }))
    await flush()
    expect(api.updateInfo).toHaveBeenLastCalledWith(true)
    expect(screen.getByText(/Nouvelle version disponible : v0.9.2/)).toBeTruthy()
  })

  it("menu de la carte : copier « claude update » seulement si Claude Code est périmé", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue(update({ claudeCurrent: "2.0.0", claudeLatest: "2.1.0", claudeNewer: true }))
    const copySpy = vi.spyOn(utils, "copyText").mockResolvedValue(undefined as never)
    vi.spyOn(toast, "success").mockReturnValue("")
    await mount()
    fireEvent.contextMenu(screen.getByText("Mise à jour"))
    fireEvent.click(await screen.findByText("Copier « claude update »"))
    await flush()
    expect(copySpy).toHaveBeenCalledWith("claude update")
  })

  it("menu de la carte à jour : ni lien de release ni copie", async () => {
    await mount()
    fireEvent.contextMenu(screen.getByText("Mise à jour"))
    await screen.findAllByText("Vérifier")
    expect(screen.queryByText("Voir")).toBeNull()
    expect(screen.queryByText("Copier « claude update »")).toBeNull()
  })

  it("menu de la carte : ouvrir la release dans un nouvel onglet", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue(update({ newer: true, latest: "v0.9.1" }))
    const openSpy = vi.spyOn(window, "open").mockReturnValue(null)
    await mount()
    fireEvent.contextMenu(screen.getByText("Mise à jour"))
    const items = await screen.findAllByText("Voir")
    fireEvent.click(items[items.length - 1])
    expect(openSpy).toHaveBeenCalledWith(expect.stringContaining("/releases/tag/v0.9.1"), "_blank", "noopener")
  })
})
