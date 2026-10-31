// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Hit, type Session, type VGroup } from "@/lib/api"
import { resetShares } from "@/hooks/useShares"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ActionsCtx, type Actions, type Row } from "./actions"
import { BgBadge, Live, SessionList } from "./SessionList"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

type ObserverCallback = (entries: { isIntersecting: boolean }[]) => void
let observerCallbacks: ObserverCallback[] = []
class IntersectionObserverStub {
  constructor(callback: ObserverCallback) {
    observerCallbacks.push(callback)
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.IntersectionObserver = IntersectionObserverStub as unknown as typeof IntersectionObserver

const nowSeconds = () => Math.floor(Date.now() / 1000)
const session = (id: string, overrides: Partial<Session> = {}): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: "/home/bob/p", mtime: nowSeconds() - 60, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 }, ...overrides })
const row = (id: string, overrides: Partial<Session> = {}, project = "/home/bob/p", missing = false): Row => ({ s: session(id, overrides), g: { project, missing, sessions: [] } })

type Overrides = Partial<Record<keyof Actions, unknown>>
const buildActions = (overrides: Overrides = {}) => {
  const calls = { read: vi.fn(), open: vi.fn(), pick: vi.fn() }
  const actions = {
    ...calls,
    home: "/home/bob",
    live: new Set<string>(),
    rc: new Set<string>(),
    background: new Set<string>(),
    waiting: new Set<string>(),
    asking: new Set<string>(),
    favs: new Set<string>(),
    picked: new Set<string>(),
    selected: [] as Row[],
    vgroups: [] as VGroup[],
    inGroup: null,
    hit: () => undefined as Hit | undefined,
    hotLeft: () => null as number | null,
    ...overrides,
  } as unknown as Actions
  return { actions, calls }
}

const mount = (items: Row[], options: { showProject?: boolean; flat?: boolean; showCache?: boolean; actions?: Overrides } = {}) => {
  const built = buildActions(options.actions)
  const view = render(
    <ActionsCtx.Provider value={built.actions}>
      <TooltipProvider>
        <SessionList items={items} showProject={options.showProject ?? false} flat={options.flat} showCache={options.showCache} />
      </TooltipProvider>
    </ActionsCtx.Provider>
  )
  return { ...built, ...view }
}

beforeEach(() => {
  observerCallbacks = []
  resetShares()
  vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("SessionList", () => {
  it("regroupe par période, la plus récente d'abord", () => {
    const day = 86_400
    mount([row("a", { mtime: nowSeconds() - 30 }), row("b", { mtime: nowSeconds() - 40 * day })])
    const headings = screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)
    expect(headings).toEqual(["Aujourd'hui", "Plus ancien"])
    expect(within(screen.getByText("Plus ancien").closest("section") as HTMLElement).getByText("Titre b")).toBeTruthy()
  })

  it("le tri par période suit la date de la dernière question quand elle existe", () => {
    const day = 86_400
    mount([row("a", { mtime: nowSeconds() - 40 * day, ask: nowSeconds() - 10 })])
    expect(screen.getByRole("heading", { level: 3 }).textContent).toBe("Aujourd'hui")
  })

  it("mode à plat : pas d'en-têtes de période", () => {
    mount([row("a"), row("b")], { flat: true })
    expect(screen.queryByRole("heading")).toBeNull()
    expect(screen.getAllByRole("listitem")).toHaveLength(2)
  })

  it("un clic sur le titre ouvre la lecture de la session", () => {
    const { calls } = mount([row("a")], { flat: true })
    fireEvent.click(screen.getByText("Titre a"))
    expect(calls.read).toHaveBeenCalledWith(expect.objectContaining({ id: "a" }))
  })

  it("« Reprendre dans un terminal » ouvre dir+id ; absent pour un dossier disparu", () => {
    const { calls } = mount([row("a"), row("b", {}, "/home/bob/gone", true)], { flat: true })
    const resumeButtons = screen.getAllByRole("button", { name: "Reprendre dans un terminal" })
    expect(resumeButtons).toHaveLength(1)
    fireEvent.click(resumeButtons[0])
    expect(calls.open).toHaveBeenCalledWith({ dir: "dir-a", id: "a" })
  })

  it("case à cocher : le clic transmet l'état de Maj pour la sélection par plage", () => {
    const { calls } = mount([row("a")], { flat: true })
    fireEvent.click(screen.getByRole("checkbox", { name: "Sélectionner" }), { shiftKey: true })
    expect(calls.pick).toHaveBeenCalledWith("a", true)
    fireEvent.click(screen.getByRole("checkbox", { name: "Sélectionner" }))
    expect(calls.pick).toHaveBeenLastCalledWith("a", false)
  })

  it("session cochée : case cochée", () => {
    mount([row("a")], { flat: true, actions: { picked: new Set(["a"]) } })
    expect(screen.getByRole("checkbox", { name: "Sélectionner" }).getAttribute("aria-checked")).toBe("true")
  })

  it("session ouverte : « en cours », ou « attend » si elle attend votre message ; badge tâche de fond ; remote control", () => {
    mount([row("a"), row("b"), row("c")], { flat: true, actions: { live: new Set(["a", "b"]), waiting: new Set(["b"]), background: new Set(["a"]), rc: new Set(["b"]) } })
    expect(screen.getAllByText("en cours")).toHaveLength(1)
    expect(screen.getAllByText("attend")).toHaveLength(1)
    expect(screen.getAllByText("tâche de fond")).toHaveLength(1)
    expect(screen.getAllByLabelText("Remote control actif")).toHaveLength(1)
  })

  it("favori : étoile accessible", () => {
    mount([row("a"), row("b")], { flat: true, actions: { favs: new Set(["a"]) } })
    expect(screen.getAllByLabelText("favori")).toHaveLength(1)
  })

  it("jetons : total abrégé et détail dans l'infobulle ; rien à zéro", () => {
    mount([row("a", { tok: { in: 1000, out: 2000, cw: 500, cr: 99 } }), row("b")], { flat: true })
    const tokens = screen.getByText("3,5 k tokens")
    expect(tokens.getAttribute("title")).toContain("cache lu 99")
    expect(screen.getAllByText(/tokens$/)).toHaveLength(1)
  })

  it("showProject : le dossier de travail est abrégé en ~", () => {
    mount([row("a", {}, "/home/bob/Projets/x")], { flat: true, showProject: true })
    expect(screen.getByText("~/Projets/x")).toBeTruthy()
  })

  it("showCache : minutes de cache restantes seulement si le cache est chaud", () => {
    mount([row("a"), row("b")], { flat: true, showCache: true, actions: { hotLeft: (target: Session) => (target.id === "a" ? 42 : null) } })
    expect(screen.getAllByText("cache 42 min")).toHaveLength(1)
  })

  it("sans showCache : la durée de cache n'est pas affichée", () => {
    mount([row("a")], { flat: true, actions: { hotLeft: () => 42 } })
    expect(screen.queryByText(/cache 42/)).toBeNull()
  })

  it("résultat de recherche : nombre de messages et extrait avec correspondances surlignées", () => {
    const hit: Hit = { dir: "d", id: "a", n: 3, snip: "avant \u0002cherché\u0003 après" }
    mount([row("a")], { flat: true, actions: { hit: (id: string) => (id === "a" ? hit : undefined) } })
    expect(screen.getByText("3 messages")).toBeTruthy()
    const marked = screen.getByText("cherché")
    expect(marked.tagName).toBe("MARK")
    expect(marked.parentElement?.textContent).toBe("avant cherché après")
  })

  it("groupes : n'affiche que les autres groupes de la session, pas celui déjà affiché", () => {
    const vgroups: VGroup[] = [
      { name: "Refonte", project: "/home/bob/p", ids: ["a"] },
      { name: "Affiché", project: "/home/bob/p", ids: ["a"] },
      { name: "Autre dossier", project: "/home/bob/q", ids: ["a"] },
    ]
    mount([row("a")], { flat: true, actions: { vgroups, inGroup: vgroups[1] } })
    expect(screen.getByText("Refonte")).toBeTruthy()
    expect(screen.queryByText("Affiché")).toBeNull()
    expect(screen.queryByText("Autre dossier")).toBeNull()
  })

  describe("sous-sessions", () => {
    it("rangées sous leur parente, repliées par défaut puis dépliées au clic", () => {
      mount([row("p"), row("k1", { parent: "p" }), row("k2", { parent: "p" })], { flat: true })
      expect(screen.queryByText("Titre k1")).toBeNull()
      const toggle = screen.getByRole("button", { name: "2 sous-sessions" })
      expect(toggle.getAttribute("aria-expanded")).toBe("false")
      fireEvent.click(toggle)
      expect(screen.getByText("Titre k1")).toBeTruthy()
      expect(screen.getByText("Titre k2")).toBeTruthy()
      expect(toggle.getAttribute("aria-expanded")).toBe("true")
      fireEvent.click(toggle)
      expect(screen.queryByText("Titre k1")).toBeNull()
    })

    it("une sous-session dont la parente n'est pas dans la liste reste affichée en haut", () => {
      mount([row("o", { parent: "disparue" })], { flat: true })
      expect(screen.getByText("Titre o")).toBeTruthy()
      expect(screen.queryByRole("button", { name: /sous-session/ })).toBeNull()
    })
  })

  describe("chargement progressif", () => {
    const many = (count: number) => Array.from({ length: count }, (_, index) => row(`s${index}`))

    it("150 sessions : 100 rendues d'abord, la suite quand la fin de liste approche", () => {
      mount(many(150), { flat: true })
      expect(screen.getAllByRole("button", { name: "Plus d'actions" })).toHaveLength(100)
      act(() => observerCallbacks.forEach((callback) => callback([{ isIntersecting: true }])))
      expect(screen.getAllByRole("button", { name: "Plus d'actions" })).toHaveLength(150)
    })

    it("une entrée non visible ne déclenche pas la suite", () => {
      mount(many(150), { flat: true })
      act(() => observerCallbacks.forEach((callback) => callback([{ isIntersecting: false }])))
      expect(screen.getAllByRole("button", { name: "Plus d'actions" })).toHaveLength(100)
    })

    it("100 sessions ou moins : pas de sentinelle", () => {
      mount(many(100), { flat: true })
      expect(observerCallbacks).toHaveLength(0)
    })

    it("en mode groupé aussi", () => {
      mount(many(150))
      expect(screen.getAllByRole("button", { name: "Plus d'actions" })).toHaveLength(100)
      act(() => observerCallbacks.forEach((callback) => callback([{ isIntersecting: true }])))
      expect(screen.getAllByRole("button", { name: "Plus d'actions" })).toHaveLength(150)
    })
  })
})

describe("Live", () => {
  it("variante « attend » en couleur d'avertissement, sans pulsation", () => {
    render(<Live waiting />)
    const badge = screen.getByText("attend")
    expect(badge.className).toContain("text-warning")
    expect(badge.querySelector(".animate-pulse")).toBeNull()
  })

  it("variante « en cours » pulsante", () => {
    render(<Live />)
    expect(screen.getByText("en cours").querySelector(".animate-pulse")).toBeTruthy()
  })
})

describe("BgBadge", () => {
  it("seulement pour une tâche de fond", () => {
    const { actions } = buildActions({ background: new Set(["a"]) })
    const { container, rerender } = render(<ActionsCtx.Provider value={actions}><BgBadge id="a" /></ActionsCtx.Provider>)
    expect(container.textContent).toBe("tâche de fond")
    rerender(<ActionsCtx.Provider value={actions}><BgBadge id="b" /></ActionsCtx.Provider>)
    expect(container.textContent).toBe("")
  })
})
