// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { createRef } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Project, type Session, type VGroup } from "@/lib/api"
import { resetGit } from "@/hooks/useGit"
import { resetOpen } from "@/hooks/useOpen"
import { resetShares } from "@/hooks/useShares"
import { resetWarm } from "@/hooks/useKeepWarm"
import type { Route } from "@/lib/route"
import { buildTree } from "@/lib/tree"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ActionsCtx, type Actions, type Row } from "./actions"
import { Sidebar, type SidebarProps } from "./Sidebar"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const HOME = "/home/bob"
const NOW = 1_800_000_000
const TTL = 3600
const flush = () => act(async () => {})

const stubViewport = (isWide: boolean) =>
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: isWide, media: query, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {} }))

const session = (id: string, overrides: Partial<Session> = {}): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: HOME, mtime: NOW - 60, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 }, ...overrides })
const row = (id: string, overrides: Partial<Session> = {}, project = `${HOME}/Projets/a`): Row => ({ s: session(id, overrides), g: { project, missing: false, sessions: [] } })

type ActionOverrides = Partial<Record<keyof Actions, unknown>>
const buildActions = (overrides: ActionOverrides = {}) => {
  const calls = { read: vi.fn(), addDir: vi.fn(), addDirPath: vi.fn() }
  const actions = {
    ...calls,
    home: HOME,
    live: new Set<string>(),
    waiting: new Set<string>(),
    asking: new Set<string>(),
    rc: new Set<string>(),
    background: new Set<string>(),
    projects: [] as Project[],
    vgroups: [] as VGroup[],
    registeredDirs: [] as string[],
    favs: new Set<string>(),
    picked: new Set<string>(),
    selected: [] as Row[],
    inGroup: null,
    projectActs: {},
    groupActs: { rows: () => [] },
    caps: () => ({ hotCache: true, keepWarm: true, config: true, memory: true, tuiPrompts: true, resume: true }),
    hotLeft: () => null,
    ...overrides,
  } as unknown as Actions
  return { actions, calls }
}

const baseProps = (overrides: Partial<SidebarProps> = {}): SidebarProps => ({
  open: true,
  onClose: vi.fn(),
  loading: false,
  total: 12,
  searchRef: createRef<HTMLInputElement>(),
  route: { kind: "all" },
  go: vi.fn(),
  counts: { all: 12, active: 2 },
  reload: vi.fn(async () => {}),
  carousel: { n: 3, on: false, start: vi.fn() },
  favRows: [],
  favGroups: [],
  groupCount: () => 4,
  read: vi.fn(),
  tree: [],
  hot: [],
  now: NOW,
  ttl: TTL,
  snippets: null,
  auto: { n: 0, purge: vi.fn(), on: false },
  args: "",
  ...overrides,
})

const mount = async (props: Partial<SidebarProps> = {}, actionOverrides: ActionOverrides = {}) => {
  const built = buildActions(actionOverrides)
  const fullProps = baseProps(props)
  render(
    <ActionsCtx.Provider value={built.actions}>
      <TooltipProvider>
        <Sidebar {...fullProps} />
      </TooltipProvider>
    </ActionsCtx.Provider>
  )
  await flush()
  return { ...built, props: fullProps }
}

beforeEach(() => {
  stubViewport(true)
  localStorage.clear()
  resetOpen()
  resetGit()
  resetShares()
  resetWarm()
  vi.spyOn(api, "gitStatus").mockResolvedValue({ status: {}, repos: {} })
  vi.spyOn(api, "notes").mockResolvedValue({ notes: {} })
  vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
  vi.spyOn(api, "keepWarm").mockResolvedValue({ items: {}, ttl: 0, below: 0 })
  vi.spyOn(api, "cleanupWarning").mockResolvedValue({ sessions: [] } as never)
  vi.spyOn(api, "homes").mockResolvedValue({ current: "", main: "", homes: [] })
  vi.spyOn(api, "updateInfo").mockRejectedValue(new Error("hors ligne"))
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("Sidebar — navigation", () => {
  it("chaque entrée de navigation mène à sa route", async () => {
    const { props } = await mount({ snippets: 7 })
    const expectedRoutes: [string, Route][] = [
      ["Accueil", { kind: "home" }],
      ["Toutes les sessions", { kind: "all" }],
      ["Workspaces sauvegardés", { kind: "tabs" }],
      ["Skills, Extensions et Plugins", { kind: "extensions" }],
      ["Teams", { kind: "teams" }],
      ["Configuration", { kind: "config", tab: "" }],
      ["Mémoire", { kind: "memory", key: "" }],
      ["Snippets", { kind: "snippets", path: "" }],
    ]
    for (const [label, route] of expectedRoutes) {
      fireEvent.click(screen.getByRole("link", { name: new RegExp(label) }))
      expect(props.go).toHaveBeenLastCalledWith(route)
    }
  })

  it("les entrées sont de vrais liens : Ctrl+clic ne navigue pas dans l'application", async () => {
    const { props } = await mount()
    const allLink = screen.getByRole("link", { name: /Toutes les sessions/ })
    expect(allLink.getAttribute("href")).toBeTruthy()
    fireEvent.click(allLink, { ctrlKey: true })
    fireEvent.click(allLink, { button: 1 })
    expect(props.go).not.toHaveBeenCalled()
  })

  it("la route courante est marquée, une seule à la fois", async () => {
    await mount({ route: { kind: "memory", key: "" } })
    const current = screen.getAllByRole("link").filter((link) => link.getAttribute("aria-current") === "page")
    expect(current).toHaveLength(1)
    expect(current[0].textContent).toContain("Mémoire")
  })

  it("« Skills » reste actif pour la route skills", async () => {
    await mount({ route: { kind: "skills", key: "u/x" } })
    expect(screen.getByRole("link", { name: /Skills, Extensions/ }).getAttribute("aria-current")).toBe("page")
  })

  it("compteurs : toutes les sessions et snippets ; snippets masqués si la bibliothèque n'est pas configurée", async () => {
    await mount({ counts: { all: 42, active: 0 }, snippets: 7 })
    expect(screen.getByRole("link", { name: /Toutes les sessions/ }).textContent).toContain("42")
    expect(screen.getByRole("link", { name: /Snippets/ }).textContent).toContain("7")
  })

  it("snippets null : pas d'entrée", async () => {
    await mount({ snippets: null })
    expect(screen.queryByRole("link", { name: /Snippets/ })).toBeNull()
  })

  it("carrousel : compte les sessions en attente et démarre au clic", async () => {
    const start = vi.fn()
    await mount({ carousel: { n: 3, on: false, start } })
    const carouselButton = screen.getByRole("button", { name: /Carrousel/ })
    expect(carouselButton.textContent).toContain("3")
    fireEvent.click(carouselButton)
    expect(start).toHaveBeenCalledTimes(1)
  })

  it("en-tête : nombre de sessions, ou « chargement » tant que la liste charge", async () => {
    const { props } = await mount({ loading: true })
    expect(screen.queryByText("12 sessions")).toBeNull()
    cleanup()
    await mount({ ...props, loading: false })
    expect(screen.getByText("12 sessions")).toBeTruthy()
  })
})

describe("Sidebar — recherche de navbar", () => {
  it("filtre les entrées de navigation par libellé", async () => {
    await mount({ snippets: 3 })
    fireEvent.change(screen.getByLabelText(/Rechercher dans les titres/), { target: { value: "mémo" } })
    expect(screen.getByRole("link", { name: /Mémoire/ })).toBeTruthy()
    expect(screen.queryByRole("link", { name: /Configuration/ })).toBeNull()
    expect(screen.queryByRole("link", { name: /Snippets/ })).toBeNull()
  })

  it("Échap vide la recherche et rend le focus", async () => {
    await mount()
    const search = screen.getByLabelText(/Rechercher dans les titres/) as HTMLInputElement
    search.focus()
    fireEvent.change(search, { target: { value: "xyz" } })
    fireEvent.keyDown(search, { key: "Escape" })
    expect(search.value).toBe("")
    expect(document.activeElement).not.toBe(search)
  })

  it("le bouton d'effacement apparaît avec du texte et vide la recherche", async () => {
    await mount()
    expect(screen.queryByRole("button", { name: "Effacer la recherche" })).toBeNull()
    const search = screen.getByLabelText(/Rechercher dans les titres/) as HTMLInputElement
    fireEvent.change(search, { target: { value: "xyz" } })
    fireEvent.click(screen.getByRole("button", { name: "Effacer la recherche" }))
    expect(search.value).toBe("")
  })

  it("filtre l'arbre : garde un dossier dont le nom correspond avec ses ancêtres", async () => {
    const tree = buildTree(
      [
        { g: { project: `${HOME}/Projets/caddy`, missing: false, sessions: [] }, ss: [session("1")] },
        { g: { project: `${HOME}/Projets/blog`, missing: false, sessions: [] }, ss: [session("2")] },
      ],
      HOME
    )
    await mount({ tree })
    expect(screen.getByRole("button", { name: /^blog/ })).toBeTruthy()
    fireEvent.change(screen.getByLabelText(/Rechercher dans les titres/), { target: { value: "cad" } })
    expect(screen.getByRole("button", { name: /^caddy/ })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /^blog/ })).toBeNull()
  })

  it("filtre les favoris par titre et masque les sessions ouvertes pendant la recherche", async () => {
    await mount({ favRows: [row("1", { title: "Migration REST" }), row("2", { title: "Audit sécurité" })], hot: [row("3")] }, { live: new Set(["3"]) })
    expect(screen.getByText("Sessions ouvertes")).toBeTruthy()
    fireEvent.change(screen.getByLabelText(/Rechercher dans les titres/), { target: { value: "audit" } })
    expect(screen.queryByText("Migration REST")).toBeNull()
    expect(screen.getByText("Audit sécurité")).toBeTruthy()
    expect(screen.queryByText("Sessions ouvertes")).toBeNull()
  })
})

describe("Sidebar — favoris", () => {
  it("session favorite : un clic la lit", async () => {
    const { props } = await mount({ favRows: [row("1", { title: "Migration REST" })] })
    expect(screen.getByText("Favoris")).toBeTruthy()
    fireEvent.click(screen.getByText("Migration REST"))
    expect(props.read).toHaveBeenCalledWith(expect.objectContaining({ id: "1" }))
  })

  it("groupe favori : compteur et navigation vers le groupe, marqué quand il est affiché", async () => {
    const favoriteGroup: VGroup = { name: "Refonte", project: `${HOME}/Projets/a`, ids: ["1"], fav: true }
    const { props } = await mount({ favGroups: [{ vg: favoriteGroup, key: "~/Projets/a" }], groupCount: () => 5, route: { kind: "group", name: "Refonte", key: "~/Projets/a" } })
    const groupLink = screen.getByRole("link", { name: /Refonte/ })
    expect(groupLink.textContent).toContain("5")
    expect(groupLink.getAttribute("aria-current")).toBe("page")
    fireEvent.click(groupLink)
    expect(props.go).toHaveBeenCalledWith({ kind: "group", name: "Refonte", key: "~/Projets/a" })
  })

  it("une sous-session favorite se range sous sa parente favorite, repliée par défaut", async () => {
    await mount({ favRows: [row("p", { title: "Parente" }), row("k", { title: "Enfant", parent: "p" })] })
    expect(screen.queryByText("Enfant")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "1 sous-session" }))
    expect(screen.getByText("Enfant")).toBeTruthy()
  })

  it("aucun favori : pas d'en-tête", async () => {
    await mount()
    expect(screen.queryByText("Favoris")).toBeNull()
  })
})

describe("Sidebar — sessions ouvertes et tièdes", () => {
  it("sépare les ouvertes des fermées au cache encore chaud, et ignore les fermées au cache expiré", async () => {
    const hot = [row("open1", { title: "Ouverte" }), row("warm1", { title: "Tiède" }), row("cold1", { title: "Froide", mtime: NOW - TTL - 10 })]
    await mount({ hot }, { live: new Set(["open1"]) })
    expect(screen.getByText("Sessions ouvertes")).toBeTruthy()
    expect(screen.getByText("Ouverte")).toBeTruthy()
    expect(screen.getByText("Tiède")).toBeTruthy()
    expect(screen.queryByText("Froide")).toBeNull()
    expect(screen.getByText("Sessions tièdes")).toBeTruthy()
  })

  it("une ligne affiche les minutes de cache restantes et alerte sous 10 min", async () => {
    await mount({ hot: [row("a", { title: "Loin", mtime: NOW - 600 }), row("b", { title: "Proche", mtime: NOW - TTL + 300 })] }, { live: new Set(["a", "b"]), waiting: new Set(["a", "b"]) })
    expect(screen.getByText("50 min").className).not.toContain("text-warning")
    expect(screen.getByText("5 min").className).toContain("text-warning")
    expect(screen.getByLabelText("cache bientôt expiré")).toBeTruthy()
  })

  it("session ouverte qui travaille : icône de travail ; qui pose une question : icône de question", async () => {
    await mount({ hot: [row("a", { title: "Travaille" }), row("b", { title: "Question" })] }, { live: new Set(["a", "b"]), waiting: new Set(["b"]), asking: new Set(["b"]) })
    expect(screen.getAllByLabelText("Claude travaille")).toHaveLength(1)
    expect(screen.getAllByLabelText("attend une réponse à une question")).toHaveLength(1)
  })

  it("un clic lit la session ; un double clic ouvre son dossier", async () => {
    const { calls, props } = await mount({ hot: [row("a", { title: "Ouverte" })] }, { live: new Set(["a"]) })
    const sessionButton = screen.getByText("Ouverte").closest("button") as HTMLElement
    fireEvent.click(sessionButton)
    expect(calls.read).toHaveBeenCalledWith(expect.objectContaining({ id: "a" }))
    fireEvent.doubleClick(sessionButton)
    expect(props.go).toHaveBeenCalledWith({ kind: "dir", key: "~/Projets/a" })
  })

  it("regroupées par dossier de travail, par ordre alphabétique", async () => {
    await mount({ hot: [row("1", { title: "Un" }, `${HOME}/Projets/zeta`), row("2", { title: "Deux" }, `${HOME}/Projets/alpha`)] }, { live: new Set(["1", "2"]) })
    const folderLabels = Array.from(document.querySelectorAll("summary")).map((summary) => summary.textContent)
    expect(folderLabels[0]).toContain("alpha")
    expect(folderLabels[1]).toContain("zeta")
  })

  it("la section se replie et l'état est retenu dans le navigateur", async () => {
    await mount({ hot: [row("a", { title: "Ouverte" })] }, { live: new Set(["a"]) })
    const foldButton = screen.getByRole("button", { name: /Sessions ouvertes/ })
    expect(foldButton.getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(foldButton)
    expect(screen.queryByText("Ouverte")).toBeNull()
    expect(localStorage.getItem("gld.hot.openFolded")).toBe("1")
    fireEvent.click(foldButton)
    expect(screen.getByText("Ouverte")).toBeTruthy()
  })

  it("sous-sessions rangées sous leur parente", async () => {
    await mount({ hot: [row("p", { title: "Parente" }), row("k", { title: "Enfant", parent: "p" })] }, { live: new Set(["p", "k"]) })
    expect(screen.queryByText("Enfant")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "1 sous-session" }))
    expect(screen.getByText("Enfant")).toBeTruthy()
  })
})

describe("Sidebar — projets, dossiers, pied de menu", () => {
  it("la liste des projets apparaît seulement s'il y en a, et se replie", async () => {
    await mount({}, { projects: [{ name: "Refonte", dirs: [`${HOME}/Projets/a`] }] })
    const foldButton = screen.getByRole("button", { name: "Projets" })
    expect(screen.getByText("Refonte")).toBeTruthy()
    fireEvent.click(foldButton)
    expect(screen.queryByText("Refonte")).toBeNull()
    expect(localStorage.getItem("gld.projects.folded")).toBe("1")
  })

  it("aucun projet : pas de section", async () => {
    await mount()
    expect(screen.queryByRole("button", { name: "Projets" })).toBeNull()
  })

  it("« Ajouter un dossier » appelle l'action", async () => {
    const { calls } = await mount()
    fireEvent.click(screen.getByRole("button", { name: "Ajouter un dossier" }))
    expect(calls.addDir).toHaveBeenCalledTimes(1)
  })

  it("chargement : squelettes à la place de l'arbre ; l'arbre revient ensuite", async () => {
    const tree = buildTree([{ g: { project: `${HOME}/Projets/a`, missing: false, sessions: [] }, ss: [session("1")] }, { g: { project: `${HOME}/Projets/b`, missing: false, sessions: [] }, ss: [session("2")] }], HOME)
    await mount({ tree, loading: true })
    expect(screen.queryByRole("button", { name: /^a/ })).toBeNull()
    cleanup()
    await mount({ tree, loading: false })
    expect(screen.getByRole("button", { name: /^a/ })).toBeTruthy()
  })

  it("section « Sessions locales » repliable", async () => {
    const tree = buildTree([{ g: { project: `${HOME}/Projets/a`, missing: false, sessions: [] }, ss: [session("1")] }, { g: { project: `${HOME}/Projets/b`, missing: false, sessions: [] }, ss: [session("2")] }], HOME)
    await mount({ tree })
    fireEvent.click(screen.getByRole("button", { name: "Sessions locales" }))
    expect(screen.queryByRole("button", { name: /^a/ })).toBeNull()
    expect(localStorage.getItem("gld.local.folded")).toBe("1")
  })

  it("dossier choisi dans l'arbre : navigue vers sa clé", async () => {
    const tree = buildTree([{ g: { project: `${HOME}/Projets/a`, missing: false, sessions: [] }, ss: [session("1")] }, { g: { project: `${HOME}/Projets/b`, missing: false, sessions: [] }, ss: [session("2")] }], HOME)
    const { props } = await mount({ tree })
    fireEvent.click(screen.getByRole("button", { name: /^b/ }))
    expect(props.go).toHaveBeenCalledWith({ kind: "dir", key: "~/Projets/b" })
  })

  it("revues automatiques : lien avec compteur et bouton de nettoyage", async () => {
    const purge = vi.fn()
    const { props } = await mount({ auto: { n: 4, purge, on: false } })
    const reviewsLink = screen.getByRole("link", { name: /Revues de sécurité/ })
    expect(reviewsLink.textContent).toContain("4")
    fireEvent.click(reviewsLink)
    expect(props.go).toHaveBeenCalledWith({ kind: "auto" })
    fireEvent.click(screen.getByRole("button", { name: /Nettoyer/ }))
    expect(purge).toHaveBeenCalledTimes(1)
  })

  it("aucune revue : pas de pied de menu", async () => {
    await mount()
    expect(screen.queryByRole("button", { name: /Nettoyer/ })).toBeNull()
  })

  it("arguments de lancement rappelés en bas", async () => {
    await mount({ args: "--model opus" })
    expect(screen.getByText("--model opus")).toBeTruthy()
  })

  it("version : affichée avec un lien de mise à jour seulement si une release plus récente existe", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue({ current: "v0.9.0", latest: "v0.9.1", url: "https://github.com/example/glaude-code/releases/tag/v0.9.1", newer: true })
    await mount()
    expect(screen.getByText("v0.9.0")).toBeTruthy()
    const updateLink = screen.getByRole("link", { name: "Mettre à jour : v0.9.1" })
    expect(updateLink.getAttribute("rel")).toContain("noopener")
  })

  it("version à jour : pas de lien", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue({ current: "v0.9.1", latest: "v0.9.1", url: "x", newer: false })
    await mount()
    expect(screen.getByText("v0.9.1")).toBeTruthy()
    expect(screen.queryByRole("link", { name: /Mettre à jour/ })).toBeNull()
  })

  it("version indisponible : pas de pied de version", async () => {
    await mount()
    expect(document.querySelector("aside")?.textContent).not.toMatch(/v\d/)
  })
})

describe("Sidebar — petit écran", () => {
  beforeEach(() => stubViewport(false))

  it("tiroir modal ouvert : le menu est dans un dialogue", async () => {
    await mount({ open: true })
    const drawer = screen.getByRole("dialog")
    expect(within(drawer).getByRole("link", { name: /Toutes les sessions/ })).toBeTruthy()
    expect(document.querySelector("aside")).toBeNull()
  })

  it("tiroir fermé : rien n'est monté", async () => {
    await mount({ open: false })
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("Échap ferme le tiroir", async () => {
    const { props } = await mount({ open: true })
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" })
    expect(props.onClose).toHaveBeenCalled()
  })

  it("écran large : colonne latérale, pas de dialogue", async () => {
    stubViewport(true)
    cleanup()
    await mount({ open: false })
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(document.querySelector("aside#menu")).toBeTruthy()
  })
})
