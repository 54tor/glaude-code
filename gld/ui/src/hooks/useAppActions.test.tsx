// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Data, type Group, type Project, type Session, type VGroup } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import type { Ask } from "@/components/Confirm"
import type { ComposeAsk } from "@/components/Compose"
import type { Pick } from "@/components/DirPicker"
import type { Ask as PromptAsk } from "@/components/Prompt"
import type { Row } from "@/components/actions"
import type { Route } from "@/lib/route"
import * as utils from "@/lib/utils"
import { useAppActions } from "./useAppActions"

const HOME = "/home/bob"
const NOW = 1_800_000_000
const CHOICE = { model: "", effort: "" } as never
const noToast = () => ""

const session = (id: string, overrides: Partial<Session> = {}): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: `${HOME}/p`, mtime: NOW - 60, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 }, ...overrides })
const group = (project: string, sessions: Session[], missing = false): Group => ({ project, missing, sessions })
const rowOf = (sessionValue: Session, folder: Group): Row => ({ s: sessionValue, g: folder })

type Fixture = {
  data: Data
  live: string[]
  vgroups: VGroup[]
  favs: string[]
  projectsList: Project[]
  searchQuery: string
}

// Contexte fourni au hook : l'état est tenu dans cette variable, setData applique les mises à jour fonctionnelles comme React.
function setup(overrides: Partial<Fixture> = {}) {
  const folderA = group(`${HOME}/Projets/a`, [session("a1", { mtime: NOW - 300 }), session("a2", { mtime: NOW - 100 })])
  const folderB = group(`${HOME}/Projets/b`, [session("b1")])
  const folderMissing = group(`${HOME}/Projets/gone`, [session("g1")], true)
  const state = {
    data: {
      home: HOME,
      root: `${HOME}/.claude/projects`,
      groups: [folderA, folderB, folderMissing],
      dirs: [],
      live: [] as string[],
      waiting: [] as string[],
      vgroups: [] as VGroup[],
      favs: [] as string[],
      projects: [] as Project[],
      providers: [{ id: "claude", name: "Claude", caps: { hotCache: true, keepWarm: true, config: true, memory: true, tuiPrompts: true, resume: true } }],
      ...overrides.data,
    } as Data,
    picked: new Set<string>(),
  }
  const everyId = new Map<string, Row>()
  for (const folder of state.data.groups) for (const sessionValue of folder.sessions) everyId.set(sessionValue.id, rowOf(sessionValue, folder))
  const vgroups = overrides.vgroups ?? []
  const favs = new Set(overrides.favs ?? [])
  const live = new Set(overrides.live ?? [])
  const spies = {
    reload: vi.fn(async () => {}),
    go: vi.fn<(route: Route) => void>(),
    read: vi.fn(),
    setAsk: vi.fn<(ask: Ask | null) => void>(),
    setPrompt: vi.fn<(ask: PromptAsk | null) => void>(),
    setCompose: vi.fn<(ask: ComposeAsk | null) => void>(),
    setInfo: vi.fn(),
    setShare: vi.fn(),
    setPicker: vi.fn<(pick: Pick | null) => void>(),
  }
  const setData = vi.fn((update: ((previous: Data | null) => Data | null) | Data | null) => {
    state.data = (typeof update === "function" ? update(state.data) : update) as Data
  })
  const setPicked = vi.fn((update: ((previous: Set<string>) => Set<string>) | Set<string>) => {
    state.picked = typeof update === "function" ? update(state.picked) : update
  })
  const hook = renderHook(() =>
    useAppActions({
      data: state.data,
      setData: setData as never,
      reload: spies.reload,
      home: HOME,
      q: overrides.searchQuery ?? "",
      go: spies.go,
      read: spies.read,
      now: NOW,
      live,
      waiting: new Set(),
      hitMap: null,
      vgroups,
      favs,
      vg: undefined,
      picked: state.picked,
      setPicked: setPicked as never,
      selected: [],
      everyId,
      view: { autoItems: [{ dir: "d", id: "auto1" }], akb: 2048 } as never,
      args: "--base",
      choice: CHOICE,
      snippets: [],
      setAsk: spies.setAsk,
      setPrompt: spies.setPrompt,
      setCompose: spies.setCompose,
      setInfo: spies.setInfo,
      setShare: spies.setShare,
      setPicker: spies.setPicker,
    })
  )
  return { ...hook, ...spies, state, setData, folderA, folderB, everyId }
}

const lastCall = <T,>(spy: { mock: { calls: T[][] } }) => spy.mock.calls[spy.mock.calls.length - 1][0]

beforeEach(() => {
  vi.spyOn(toast, "loading").mockReturnValue("t1")
  vi.spyOn(toast, "success").mockImplementation(noToast)
  vi.spyOn(toast, "error").mockImplementation(noToast)
  vi.spyOn(toast, "info").mockImplementation(noToast)
  vi.spyOn(api, "vgroups").mockResolvedValue(undefined as never)
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("lecture de l'état", () => {
  it("hotLeft : minutes de cache restantes bornées entre 1 et 60, null passé une heure", () => {
    const { result } = setup()
    expect(result.current.actions.hotLeft({ mtime: NOW })).toBe(60)
    expect(result.current.actions.hotLeft({ mtime: NOW - 60 })).toBe(59)
    expect(result.current.actions.hotLeft({ mtime: NOW - 1800 })).toBe(30)
    expect(result.current.actions.hotLeft({ mtime: NOW - 3599 })).toBe(1)
    expect(result.current.actions.hotLeft({ mtime: NOW - 3600 })).toBeNull()
    expect(result.current.actions.hotLeft({ mtime: NOW - 99999 })).toBeNull()
  })

  it("caps : celles du fournisseur de la session, « claude » par défaut, rien pour un fournisseur inconnu", () => {
    const { result } = setup()
    expect(result.current.actions.caps(session("x")).keepWarm).toBe(true)
    expect(result.current.actions.caps(session("x", { provider: "claude" })).keepWarm).toBe(true)
    expect(result.current.actions.caps(session("x", { provider: "inconnu" }))).toEqual({ hotCache: false, keepWarm: false, config: false, memory: false, tuiPrompts: false, resume: false })
  })

  it("lignes triées par dernier message, du plus récent au plus ancien", () => {
    const { result } = setup()
    expect(result.current.actions.rows.map((row) => row.s.id)).toEqual(["b1", "g1", "a2", "a1"])
  })

  it("folder : retrouve un dossier par son chemin", () => {
    const { result } = setup()
    expect(result.current.actions.folder(`${HOME}/Projets/b`)?.sessions[0].id).toBe("b1")
    expect(result.current.actions.folder("/nulle/part")).toBeUndefined()
  })

  it("openPerms : réglages du projet si une session existe, sinon l'onglet permissions", () => {
    const { result, go } = setup()
    result.current.actions.openPerms(`${HOME}/Projets/a`)
    expect(go).toHaveBeenLastCalledWith({ kind: "projectConfig", dir: "dir-a1" })
    result.current.actions.openPerms("/nulle/part")
    expect(go).toHaveBeenLastCalledWith({ kind: "config", tab: "permissions" })
  })

  it("targets : dossiers existants triés par nom ; sans session, lancement par cwd", () => {
    const { result } = setup({ data: { groups: [group(`${HOME}/z`, [session("z1")]), group(`${HOME}/a`, []), group(`${HOME}/m`, [session("m1")], true)] } as never })
    const targets = result.current.targets()
    expect(targets.map((target) => target.label)).toEqual(["~/a", "~/z"])
    expect(targets[0]).toMatchObject({ project: `${HOME}/a`, dir: "", id: "", cwd: `${HOME}/a` })
    expect(targets[1]).toMatchObject({ dir: "dir-z1", id: "z1" })
    expect(targets[1].cwd).toBeUndefined()
  })

  it("targets : la session la plus récemment interrogée sert de point d'ancrage", () => {
    const { result } = setup()
    const target = result.current.targets().find((candidate) => candidate.project === `${HOME}/Projets/a`)
    expect(target?.id).toBe("a2")
  })
})

describe("sélection", () => {
  const rowsInDom = (ids: string[]) => {
    document.body.innerHTML = `<main>${ids.map((id) => `<button data-row data-id="${id}"></button>`).join("")}</main>`
  }
  afterEach(() => {
    document.body.innerHTML = ""
  })

  it("pick bascule une session", () => {
    const { result, state } = setup()
    act(() => void result.current.actions.pick("a1"))
    expect([...state.picked]).toEqual(["a1"])
    act(() => void result.current.actions.pick("a1"))
    expect([...state.picked]).toEqual([])
  })

  it("Maj+clic coche toute la plage visible entre la dernière case et celle-ci", () => {
    rowsInDom(["s1", "s2", "s3", "s4", "s5"])
    const { result, state } = setup()
    act(() => void result.current.actions.pick("s2"))
    act(() => void result.current.actions.pick("s4", true))
    expect([...state.picked].sort()).toEqual(["s2", "s3", "s4"])
  })

  it("plage dans l'autre sens", () => {
    rowsInDom(["s1", "s2", "s3", "s4"])
    const { result, state } = setup()
    act(() => void result.current.actions.pick("s4"))
    act(() => void result.current.actions.pick("s2", true))
    expect([...state.picked].sort()).toEqual(["s2", "s3", "s4"])
  })

  it("Maj+clic sans point de départ visible : simple bascule", () => {
    rowsInDom(["s1", "s2"])
    const { result, state } = setup()
    act(() => void result.current.actions.pick("s2", true))
    expect([...state.picked]).toEqual(["s2"])
  })

  it("groupActs.select ajoute les sessions du groupe à la sélection existante", () => {
    const { result, state } = setup()
    state.picked = new Set(["autre"])
    act(() => void result.current.groupActs.select({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2", "inconnue"] }))
    expect([...state.picked].sort()).toEqual(["a1", "a2", "autre"])
  })
})

describe("favoris", () => {
  it("ajoute si au moins une session n'est pas favorite, et enregistre côté serveur", async () => {
    const { result, state } = setup({ favs: ["a1"] })
    act(() => void result.current.actions.toggleFav(["a1", "a2"]))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: ["a1", "a2"] })
    expect(state.data.favs).toEqual(["a1", "a2"])
  })

  it("retire quand toutes sont déjà favorites", async () => {
    const { result } = setup({ favs: ["a1", "a2", "b1"] })
    act(() => void result.current.actions.toggleFav(["a1", "a2"]))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: ["b1"] })
  })

  it("échec d'enregistrement : toast d'erreur et rechargement pour revenir à l'état serveur", async () => {
    vi.mocked(api.vgroups).mockRejectedValue(new ApiError(500, "boom", "écriture refusée"))
    const { result, reload } = setup()
    act(() => void result.current.actions.toggleFav(["a1"]))
    await act(async () => {})
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("écriture refusée"))
    expect(reload).toHaveBeenCalled()
  })
})

describe("groupes", () => {
  const existing: VGroup = { name: "G", project: `${HOME}/Projets/a`, ids: ["a1"] }

  it("group : ajoute à un groupe existant sans doublon", async () => {
    const { result } = setup({ vgroups: [existing] })
    act(() => void result.current.actions.group(["a1", "a2"], `${HOME}/Projets/a`, "G"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ ...existing, ids: ["a1", "a2"] }], favs: [] })
  })

  it("group : crée le groupe s'il n'existe pas, avec un toast dont l'action ouvre le groupe", async () => {
    const { result, go } = setup()
    act(() => void result.current.actions.group(["a1"], `${HOME}/Projets/a`, "Nouveau"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ name: "Nouveau", project: `${HOME}/Projets/a`, ids: ["a1"] }], favs: [] })
    const options = vi.mocked(toast.success).mock.calls[0][1] as { action: { onClick: () => void } }
    options.action.onClick()
    expect(go).toHaveBeenCalledWith({ kind: "group", name: "Nouveau", key: "~/Projets/a" })
  })

  it("group avec nom null : demande le nom puis ajoute", async () => {
    const { result, setPrompt } = setup()
    act(() => void result.current.actions.group(["a1"], `${HOME}/Projets/a`, null))
    const prompt = lastCall(setPrompt) as PromptAsk
    expect(prompt.title).toContain("~/Projets/a")
    await act(async () => void prompt.run("Saisi"))
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ name: "Saisi", project: `${HOME}/Projets/a`, ids: ["a1"] }], favs: [] })
  })

  it("deux groupes de même nom dans deux dossiers restent distincts", async () => {
    const other: VGroup = { name: "G", project: `${HOME}/Projets/b`, ids: ["b1"] }
    const { result } = setup({ vgroups: [existing, other] })
    act(() => void result.current.actions.group(["a2"], `${HOME}/Projets/a`, "G"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ ...existing, ids: ["a1", "a2"] }, other], favs: [] })
  })

  it("ungroup retire les sessions du groupe donné et ne fait rien sans groupe affiché", async () => {
    const { result } = setup({ vgroups: [{ ...existing, ids: ["a1", "a2"] }] })
    act(() => void result.current.actions.ungroup(["a1"], existing))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ ...existing, ids: ["a2"] }], favs: [] })
    vi.mocked(api.vgroups).mockClear()
    act(() => void result.current.actions.ungroup(["a2"]))
    expect(api.vgroups).not.toHaveBeenCalled()
  })

  it("fav bascule le favori du groupe", async () => {
    const { result } = setup({ vgroups: [existing] })
    act(() => void result.current.groupActs.fav(existing))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ ...existing, fav: true }], favs: [] })
  })

  it("rename : refuse un nom déjà pris dans le dossier, accepte un nom libre", async () => {
    const sibling: VGroup = { name: "Pris", project: `${HOME}/Projets/a`, ids: [] }
    const { result, setPrompt } = setup({ vgroups: [existing, sibling] })
    act(() => void result.current.groupActs.rename(existing))
    const prompt = lastCall(setPrompt) as PromptAsk
    expect(prompt.value).toBe("G")
    await act(async () => void prompt.run("Pris"))
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("Pris"))
    expect(api.vgroups).not.toHaveBeenCalled()
    await act(async () => void prompt.run("Libre"))
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [{ ...existing, name: "Libre" }, sibling], favs: [] })
  })

  it("rename au même nom : rien", async () => {
    const { result, setPrompt } = setup({ vgroups: [existing] })
    act(() => void result.current.groupActs.rename(existing))
    await act(async () => void (lastCall(setPrompt) as PromptAsk).run("G"))
    expect(api.vgroups).not.toHaveBeenCalled()
  })

  it("remove : confirmation puis suppression du groupe", async () => {
    const { result, setAsk } = setup({ vgroups: [existing] })
    act(() => void result.current.groupActs.remove(existing))
    const ask = lastCall(setAsk) as Ask
    expect(api.vgroups).not.toHaveBeenCalled()
    await act(async () => void ask.run())
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [] })
  })

  it("rows : sessions encore présentes, les plus récentes d'abord", () => {
    const { result } = setup()
    const rows = result.current.groupActs.rows({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2", "disparue"] })
    expect(rows.map((row) => row.s.id)).toEqual(["a2", "a1"])
  })

  it("show : route du groupe avec la clé de son dossier", () => {
    const { result, go } = setup()
    result.current.groupActs.show(existing)
    expect(go).toHaveBeenCalledWith({ kind: "group", name: "G", key: "~/Projets/a" })
  })

  it("refs : copie les références (titre, identifiant, chemin du transcript) des sessions du groupe", async () => {
    const copySpy = vi.spyOn(utils, "copyText").mockResolvedValue(undefined as never)
    const { result } = setup()
    result.current.groupActs.refs({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2"] })
    await act(async () => {})
    const copied = copySpy.mock.calls[0][0]
    expect(copied).toContain("Titre a2")
    expect(copied).toContain(`${HOME}/.claude/projects/dir-a1/a1.jsonl`)
    expect(copied.indexOf("a2")).toBeLessThan(copied.indexOf("a1.jsonl"))
    expect(toast.success).toHaveBeenCalled()
  })

  it("refs : échec du presse-papiers signalé", async () => {
    vi.spyOn(utils, "copyText").mockRejectedValue(new Error("refusé"))
    const { result } = setup()
    result.current.groupActs.refs(existing)
    await act(async () => {})
    expect(toast.error).toHaveBeenCalled()
  })

  it("prompt : ouvre la saisie avec les références, et lance dans la première session du groupe", async () => {
    const openSpy = vi.spyOn(api, "newSession").mockResolvedValue({ message: "Lancée" })
    const { result, setCompose } = setup()
    result.current.groupActs.prompt({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2"] })
    const compose = lastCall(setCompose) as ComposeAsk
    expect(compose.initial).toContain("Titre a1")
    await act(async () => void (await compose.run("hello", CHOICE)))
    expect(openSpy).toHaveBeenCalledWith(expect.objectContaining({ dir: "dir-a2", id: "a2", prompt: "hello" }))
  })

  it("prompt : groupe sans session, rien", () => {
    const { result, setCompose } = setup()
    result.current.groupActs.prompt({ name: "G", project: `${HOME}/Projets/a`, ids: ["disparue"] })
    expect(setCompose).not.toHaveBeenCalled()
  })

  it("open : reprend dans des onglets les sessions du groupe qui ne tournent pas et dont le dossier existe", async () => {
    const openSpy = vi.spyOn(api, "open").mockResolvedValue(undefined as never)
    const { result } = setup({ live: ["a2"] })
    await act(async () => void (await result.current.groupActs.open({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2", "g1"] })))
    expect(openSpy).toHaveBeenCalledTimes(1)
    expect(openSpy).toHaveBeenCalledWith({ dir: "dir-a1", id: "a1", args: "--base" })
    expect(toast.success).toHaveBeenCalled()
  })

  it("open : une ouverture ratée fait finir le bilan en erreur", async () => {
    vi.spyOn(api, "open").mockResolvedValueOnce(undefined as never).mockRejectedValueOnce(new Error("x"))
    const { result } = setup()
    await act(async () => void (await result.current.groupActs.open({ name: "G", project: `${HOME}/Projets/a`, ids: ["a1", "a2"] })))
    expect(toast.error).toHaveBeenCalledWith(expect.stringMatching(/1/), expect.anything())
  })
})

describe("projets", () => {
  const project: Project = { name: "Refonte", dirs: [`${HOME}/Projets/a`] }

  it("add dans un projet existant : ajoute le dossier une seule fois", async () => {
    const { result } = setup({ data: { projects: [project] } as never })
    act(() => void result.current.actions.projectActs.add(`${HOME}/Projets/b`, "Refonte"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [{ name: "Refonte", dirs: [`${HOME}/Projets/a`, `${HOME}/Projets/b`] }] })
    vi.mocked(api.vgroups).mockClear()
    act(() => void result.current.actions.projectActs.add(`${HOME}/Projets/a`, "Refonte"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [project] })
  })

  it("add avec nom null : demande un nom, refuse un nom existant, crée sinon", async () => {
    const { result, setPrompt } = setup({ data: { projects: [project] } as never })
    act(() => void result.current.actions.projectActs.add(`${HOME}/Projets/b`, null))
    const prompt = lastCall(setPrompt) as PromptAsk
    await act(async () => void prompt.run("  Refonte  "))
    expect(toast.error).toHaveBeenCalled()
    expect(api.vgroups).not.toHaveBeenCalled()
    await act(async () => void prompt.run("  Neuf  "))
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [project, { name: "Neuf", dirs: [`${HOME}/Projets/b`] }] })
  })

  it("remove retire le dossier du projet", async () => {
    const { result } = setup({ data: { projects: [project] } as never })
    act(() => void result.current.actions.projectActs.remove(`${HOME}/Projets/a`, "Refonte"))
    await act(async () => {})
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [{ name: "Refonte", dirs: [] }] })
  })

  it("rename : enregistre, déplace la note et navigue vers le nouveau nom", async () => {
    const notesSpy = vi.spyOn(api, "notesSave").mockResolvedValue({} as never)
    const { result, setPrompt, go } = setup({ data: { projects: [project] } as never })
    act(() => void result.current.actions.projectActs.rename(project))
    await act(async () => void (lastCall(setPrompt) as PromptAsk).run("Nouveau"))
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [{ ...project, name: "Nouveau" }] })
    expect(notesSpy).toHaveBeenCalledWith({ key: "p/Refonte", to: "p/Nouveau" })
    expect(go).toHaveBeenCalledWith({ kind: "project", name: "Nouveau" })
  })

  it("rename : même nom ou nom pris, rien n'est enregistré", async () => {
    const other: Project = { name: "Autre", dirs: [] }
    const { result, setPrompt } = setup({ data: { projects: [project, other] } as never })
    act(() => void result.current.actions.projectActs.rename(project))
    const prompt = lastCall(setPrompt) as PromptAsk
    await act(async () => void prompt.run("Refonte"))
    await act(async () => void prompt.run("Autre"))
    expect(api.vgroups).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledTimes(1)
  })

  it("delete : confirmation, suppression, retour aux récentes", async () => {
    const { result, setAsk, go } = setup({ data: { projects: [project] } as never })
    act(() => void result.current.actions.projectActs.delete(project))
    await act(async () => void (await (lastCall(setAsk) as Ask).run()))
    expect(api.vgroups).toHaveBeenCalledWith({ groups: [], favs: [], projects: [] })
    expect(go).toHaveBeenCalledWith({ kind: "recent" })
  })

  it("promptAll : refuse s'il n'y a aucun dossier du projet ; sinon lance une session par dossier", async () => {
    const startSpy = vi.spyOn(api, "newSession").mockResolvedValue({ message: "ok" })
    const empty = setup()
    empty.result.current.actions.projectActs.promptAll({ name: "Vide", dirs: ["/nulle/part"] })
    expect(toast.error).toHaveBeenCalled()
    expect(empty.setCompose).not.toHaveBeenCalled()
    const { result, setCompose } = setup()
    result.current.actions.projectActs.promptAll({ name: "Deux", dirs: [`${HOME}/Projets/a`, `${HOME}/Projets/b`, `${HOME}/Projets/gone`] })
    const compose = lastCall(setCompose) as ComposeAsk
    expect(compose.warning).toContain("2")
    const succeeded = await compose.run("go", CHOICE)
    expect(succeeded).toBe(true)
    expect(startSpy).toHaveBeenCalledTimes(2)
  })
})

describe("sessions", () => {
  it("newSession : ouvre la saisie pour le dossier ; rien pour un dossier inconnu", () => {
    const { result, setCompose } = setup()
    result.current.actions.newSession("/nulle/part")
    expect(setCompose).not.toHaveBeenCalled()
    result.current.actions.newSession(`${HOME}/Projets/a`, "début")
    const compose = lastCall(setCompose) as ComposeAsk
    expect(compose.initial).toBe("début")
    expect(compose.project).toBe(`${HOME}/Projets/a`)
  })

  it("newSession : le lancement part dans la dernière session du dossier et rappelle onSent seulement en cas de succès", async () => {
    const startSpy = vi.spyOn(api, "newSession").mockResolvedValueOnce({ message: "ok" }).mockRejectedValueOnce(new ApiError(500, "boom", "x"))
    const onSent = vi.fn()
    const { result, setCompose } = setup()
    result.current.actions.newSession(`${HOME}/Projets/a`, undefined, onSent)
    const compose = lastCall(setCompose) as ComposeAsk
    expect(await compose.run("p1", CHOICE)).toBe(true)
    expect(startSpy).toHaveBeenCalledWith(expect.objectContaining({ dir: "dir-a2", id: "a2", prompt: "p1" }))
    expect(onSent).toHaveBeenCalledTimes(1)
    expect(await compose.run("p2", CHOICE)).toBe(false)
    expect(onSent).toHaveBeenCalledTimes(1)
  })

  it("startSession : false sans dossier correspondant, true et lancement sinon", async () => {
    const startSpy = vi.spyOn(api, "newSession").mockResolvedValue({ message: "ok" })
    const { result } = setup()
    expect(await result.current.actions.startSession("/nulle/part", "x", CHOICE)).toBe(false)
    expect(startSpy).not.toHaveBeenCalled()
    expect(await result.current.actions.startSession(`${HOME}/Projets/b`, "x", CHOICE)).toBe(true)
    expect(startSpy).toHaveBeenCalledWith(expect.objectContaining({ dir: "dir-b1", id: "b1", prompt: "x" }))
  })

  it("une action qui échoue affiche l'erreur et recharge quand même", async () => {
    vi.spyOn(api, "open").mockRejectedValue(new ApiError(500, "boom", "terminal indisponible"))
    const toastErrorSpy = vi.mocked(toast.error)
    const { result, reload } = setup()
    await act(async () => void (await result.current.actions.open({ dir: "dir-a1", id: "a1" })))
    expect(toastErrorSpy).toHaveBeenCalled()
    expect(reload).toHaveBeenCalled()
  })

  it("open : transmet les arguments de lancement et relit jusqu'à voir la session apparaître", async () => {
    vi.useFakeTimers()
    const openSpy = vi.spyOn(api, "open").mockResolvedValue({ message: "ok" } as never)
    const { result, reload } = setup()
    await act(async () => void (await result.current.actions.open({ dir: "dir-a1", id: "a1" })))
    expect(openSpy).toHaveBeenCalledWith({ dir: "dir-a1", id: "a1", args: "--base" })
    expect(reload).toHaveBeenCalledTimes(1)
    await act(async () => void (await vi.advanceTimersByTimeAsync(700 * 8 + 100)))
    expect(reload).toHaveBeenCalledTimes(9)
  })

  it("resumeWith : déjà ouverte, simple information ; sinon saisie puis reprise avec le texte", async () => {
    const openSpy = vi.spyOn(api, "open").mockResolvedValue({ message: "ok" } as never)
    const opened = setup({ live: ["a1"] })
    opened.result.current.actions.resumeWith(opened.everyId.get("a1")!)
    expect(toast.info).toHaveBeenCalled()
    expect(opened.setCompose).not.toHaveBeenCalled()
    const { result, setCompose, everyId } = setup()
    result.current.actions.resumeWith(everyId.get("a1")!)
    const compose = lastCall(setCompose) as ComposeAsk
    expect(compose.subtitle).toBe("Titre a1")
    await act(async () => void (await compose.run("continue", CHOICE)))
    expect(openSpy).toHaveBeenCalledWith(expect.objectContaining({ dir: "dir-a1", id: "a1", prompt: "continue" }))
  })

  it("resumeSend : reprise directe avec le texte", async () => {
    const openSpy = vi.spyOn(api, "open").mockResolvedValue({ message: "ok" } as never)
    const { result, everyId } = setup()
    expect(await result.current.actions.resumeSend(everyId.get("a1")!, "salut")).toBe(true)
    expect(openSpy).toHaveBeenCalledWith(expect.objectContaining({ prompt: "salut" }))
  })

  it("copy : copie l'identifiant ; échec signalé", async () => {
    const copySpy = vi.spyOn(utils, "copyText").mockResolvedValue(undefined as never)
    const { result } = setup()
    result.current.actions.copy(session("a1"))
    await act(async () => {})
    expect(copySpy).toHaveBeenCalledWith("a1")
    expect(toast.success).toHaveBeenCalled()
    copySpy.mockRejectedValue(new Error("x"))
    result.current.actions.copy(session("a1"))
    await act(async () => {})
    expect(toast.error).toHaveBeenCalled()
  })

  it("rename : propose le titre actuel seulement s'il a été donné, et un titre automatique", async () => {
    const renameSpy = vi.spyOn(api, "rename").mockResolvedValue({ message: "ok", title: "t" })
    const { result, setPrompt } = setup()
    result.current.actions.rename(session("a1", { named: false, title: "auto-généré" }))
    const unnamedPrompt = lastCall(setPrompt) as PromptAsk
    expect(unnamedPrompt.value).toBe("")
    result.current.actions.rename(session("a1", { named: true, title: "Mon titre" }))
    const namedPrompt = lastCall(setPrompt) as PromptAsk
    expect(namedPrompt.value).toBe("Mon titre")
    await act(async () => void (await namedPrompt.run("Nouveau")))
    expect(renameSpy).toHaveBeenLastCalledWith(expect.objectContaining({ id: "a1" }), { title: "Nouveau" })
    await act(async () => void namedPrompt.auto!.run())
    expect(renameSpy).toHaveBeenLastCalledWith(expect.objectContaining({ id: "a1" }), { auto: true })
  })

  it("move : sans destination ou vers le dossier courant, rien n'est déplacé", async () => {
    const reassignSpy = vi.spyOn(api, "reassign").mockResolvedValue({ message: "ok", n: 1 })
    const { result, setPicker, everyId } = setup()
    const rows = [everyId.get("a1")!]
    const cancelled = result.current.actions.move(rows)
    lastCall(setPicker)!.done(null)
    await act(async () => void (await cancelled))
    const sameDir = result.current.actions.move(rows)
    lastCall(setPicker)!.done(`${HOME}/p`)
    await act(async () => void (await sameDir))
    expect(reassignSpy).not.toHaveBeenCalled()
  })

  it("move : déplace les sessions qui ne sont pas déjà à destination, puis les décoche", async () => {
    const reassignSpy = vi.spyOn(api, "reassign").mockResolvedValue({ message: "ok", n: 1 })
    const { result, setPicker, everyId, state } = setup()
    state.picked = new Set(["a1", "b1"])
    const rows = [everyId.get("a1")!, everyId.get("b1")!]
    const moving = result.current.actions.move(rows)
    expect(lastCall(setPicker)!.start).toBe(`${HOME}/p`)
    lastCall(setPicker)!.done(`${HOME}/ailleurs`)
    await act(async () => void (await moving))
    expect(reassignSpy).toHaveBeenCalledWith([{ dir: "dir-a1", id: "a1" }, { dir: "dir-b1", id: "b1" }], `${HOME}/ailleurs`)
    expect([...state.picked]).toEqual([])
  })

  it("moveGroup : réassigne les sessions du dossier à la destination choisie", async () => {
    const reassignSpy = vi.spyOn(api, "reassign").mockResolvedValue({ message: "ok", n: 2 })
    const { result, setPicker, folderA } = setup()
    const moving = result.current.actions.moveGroup(folderA, [{ dir: "dir-a1", id: "a1" }])
    lastCall(setPicker)!.done(`${HOME}/ailleurs`)
    await act(async () => void (await moving))
    expect(reassignSpy).toHaveBeenCalledWith([{ dir: "dir-a1", id: "a1" }], `${HOME}/ailleurs`)
  })
})

describe("fermeture et suppression", () => {
  it("stop : confirmation puis fermeture de chaque session, retrait immédiat de la liste des ouvertes, relectures", async () => {
    vi.useFakeTimers()
    const closeSpy = vi.spyOn(api, "sessionClose").mockResolvedValue({ message: "ok" })
    const onDone = vi.fn()
    const { result, setAsk, state, reload } = setup({ live: ["a1", "a2"], data: { live: ["a1", "a2"], waiting: ["a1"] } as never })
    result.current.actions.stop(["a1", "a2"], onDone)
    const ask = lastCall(setAsk) as Ask
    expect(ask.rows).toEqual(["a1", "a2"])
    expect(closeSpy).not.toHaveBeenCalled()
    await act(async () => void ask.run())
    await act(async () => void (await vi.advanceTimersByTimeAsync(5000)))
    expect(closeSpy).toHaveBeenCalledTimes(2)
    expect(state.data.live).toEqual([])
    expect(state.data.waiting).toEqual([])
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(reload).toHaveBeenCalledTimes(4)
    expect(toast.success).toHaveBeenCalled()
  })

  it("stop : une fermeture qui échoue garde cette session dans la liste, signale l'erreur, et n'appelle pas onDone", async () => {
    vi.useFakeTimers()
    vi.spyOn(api, "sessionClose").mockResolvedValueOnce({ message: "ok" }).mockRejectedValueOnce(new ApiError(500, "boom", "processus rebelle"))
    const onDone = vi.fn()
    const { result, setAsk, state } = setup({ live: ["a1", "a2"], data: { live: ["a1", "a2"] } as never })
    result.current.actions.stop(["a1", "a2"], onDone)
    await act(async () => void (lastCall(setAsk) as Ask).run())
    await act(async () => void (await vi.advanceTimersByTimeAsync(5000)))
    expect(state.data.live).toEqual(["a2"])
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("processus rebelle"))
    expect(onDone).not.toHaveBeenCalled()
  })

  it("remove : prévient si une session à supprimer est ouverte", () => {
    const { result, setAsk } = setup({ live: ["a1"] })
    result.current.actions.remove([{ dir: "dir-a1", id: "a1" }], "« Titre a1 »")
    const withLive = lastCall(setAsk) as Ask
    expect(withLive.removes).toBe(true)
    result.current.actions.remove([{ dir: "dir-a2", id: "a2" }], "« Titre a2 »")
    const withoutLive = lastCall(setAsk) as Ask
    expect(withLive.body.length).toBeGreaterThan(withoutLive.body.length)
  })

  it("remove confirmé : décoche, appelle after, met à la corbeille, recharge, propose d'annuler", async () => {
    const trashSpy = vi.spyOn(api, "trashSessionsAdd").mockResolvedValue({ message: "2 sessions envoyées\ndétail" })
    const restoreSpy = vi.spyOn(api, "trashSessionsRestore").mockResolvedValue({ message: "restaurées" })
    const after = vi.fn()
    const { result, setAsk, state, reload } = setup()
    state.picked = new Set(["a1", "a2", "b1"])
    const items = [{ dir: "dir-a1", id: "a1" }, { dir: "dir-a2", id: "a2" }]
    result.current.actions.remove(items, "ces sessions", after)
    await act(async () => void (await (lastCall(setAsk) as Ask).run()))
    expect(after).toHaveBeenCalledTimes(1)
    expect(trashSpy).toHaveBeenCalledWith(items)
    expect([...state.picked]).toEqual(["b1"])
    expect(reload).toHaveBeenCalled()
    const [title, options] = vi.mocked(toast.success).mock.calls[0] as [string, { action: { onClick: () => Promise<void> } }]
    expect(title).toBe("2 sessions envoyées")
    await act(async () => void (await options.action.onClick()))
    expect(restoreSpy).toHaveBeenCalledWith(["a1", "a2"])
  })

  it("remove : si la corbeille échoue, erreur affichée, aucun toast d'annulation", async () => {
    vi.spyOn(api, "trashSessionsAdd").mockRejectedValue(new ApiError(500, "boom", "corbeille pleine"))
    const { result, setAsk, reload } = setup()
    result.current.actions.remove([{ dir: "dir-a1", id: "a1" }], "x")
    await act(async () => void (await (lastCall(setAsk) as Ask).run()))
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("corbeille pleine"))
    expect(toast.success).not.toHaveBeenCalled()
    expect(reload).toHaveBeenCalled()
  })

  it("purgeAuto : confirmation chiffrée, puis suppression des revues automatiques", async () => {
    const rmSpy = vi.spyOn(api, "rmSessions").mockResolvedValue({ message: "ok" } as never)
    const { result, setAsk } = setup()
    result.current.purgeAuto()
    const ask = lastCall(setAsk) as Ask
    expect(rmSpy).not.toHaveBeenCalled()
    await act(async () => void (await (ask.run as () => Promise<boolean>)()))
    expect(rmSpy).toHaveBeenCalledWith([{ dir: "d", id: "auto1" }])
  })
})

describe("dossiers enregistrés", () => {
  it("addDirPath : ajoute, recharge puis ouvre le dossier", async () => {
    const addSpy = vi.spyOn(api, "dirsAdd").mockResolvedValue({ message: "ok", n: 1 })
    const { result, reload, go } = setup()
    await act(async () => void (await result.current.actions.addDirPath(`${HOME}/Projets/neuf`)))
    expect(addSpy).toHaveBeenCalledWith(`${HOME}/Projets/neuf`)
    expect(reload).toHaveBeenCalled()
    expect(go).toHaveBeenCalledWith({ kind: "dir", key: "~/Projets/neuf" })
  })

  it("addDirPath : échec, erreur affichée sans navigation ni rechargement", async () => {
    vi.spyOn(api, "dirsAdd").mockRejectedValue(new ApiError(400, "bad", "dossier introuvable"))
    const { result, reload, go } = setup()
    await act(async () => void (await result.current.actions.addDirPath("/nulle/part")))
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("dossier introuvable"))
    expect(reload).not.toHaveBeenCalled()
    expect(go).not.toHaveBeenCalled()
  })

  it("addDir : le sélecteur propose d'inclure les sous-dossiers ; choisi, il les ajoute tous", async () => {
    const addSpy = vi.spyOn(api, "dirsAdd").mockResolvedValue({ message: "ok", n: 3 })
    const { result, setPicker, reload } = setup()
    result.current.actions.addDir()
    const pick = lastCall(setPicker)!
    expect(pick.children).toBe(true)
    expect(pick.start).toBe(HOME)
    pick.done(`${HOME}/Projets`, true)
    await act(async () => {})
    expect(addSpy).toHaveBeenCalledWith(`${HOME}/Projets`, true)
    expect(reload).toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalled()
  })

  it("addDir : sélecteur annulé, rien n'est ajouté", async () => {
    const addSpy = vi.spyOn(api, "dirsAdd").mockResolvedValue({ message: "ok", n: 0 })
    const { result, setPicker } = setup()
    result.current.actions.addDir()
    lastCall(setPicker)!.done(null)
    await act(async () => {})
    expect(addSpy).not.toHaveBeenCalled()
  })

  it("removeDir : retire puis recharge ; échec signalé sans rechargement", async () => {
    const removeSpy = vi.spyOn(api, "dirsRemove").mockResolvedValueOnce({ message: "ok" }).mockRejectedValueOnce(new ApiError(500, "boom", "impossible"))
    const { result, reload } = setup()
    await act(async () => void (await result.current.actions.removeDir(`${HOME}/Projets/a`)))
    expect(removeSpy).toHaveBeenCalledWith(`${HOME}/Projets/a`)
    expect(reload).toHaveBeenCalledTimes(1)
    await act(async () => void (await result.current.actions.removeDir(`${HOME}/Projets/a`)))
    expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("impossible"))
    expect(reload).toHaveBeenCalledTimes(1)
  })
})

describe("touch", () => {
  it("marque la session ouverte et récente tout de suite, puis relit au bout de 2 s", async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(1_900_000_000_000))
    const { result, state, reload } = setup({ data: { live: [], waiting: ["a1"] } as never })
    act(() => void result.current.actions.touch("a1"))
    expect(state.data.live).toEqual(["a1"])
    expect(state.data.waiting).toEqual([])
    const touched = state.data.groups[0].sessions.find((candidate) => candidate.id === "a1")!
    expect(touched.mtime).toBe(1_900_000_000)
    expect(touched.ask).toBe(1_900_000_000)
    expect(state.data.groups[0].sessions.find((candidate) => candidate.id === "a2")!.ask).toBeUndefined()
    expect(reload).not.toHaveBeenCalled()
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it("ne duplique pas une session déjà ouverte", () => {
    const { result, state } = setup({ data: { live: ["a1"] } as never })
    act(() => void result.current.actions.touch("a1"))
    expect(state.data.live).toEqual(["a1"])
  })
})

describe("liens", () => {
  it("info, partage et lecture sont transmis tels quels", () => {
    const { result, setInfo, setShare, read } = setup()
    expect(result.current.actions.info).toBe(setInfo)
    expect(result.current.actions.share).toBe(setShare)
    expect(result.current.actions.read).toBe(read)
  })

  it("exportMd : un échec d'export est signalé", async () => {
    const { result, everyId } = setup()
    vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new Error("réseau"))))
    await act(async () => void (await result.current.actions.exportMd(everyId.get("a1")!)))
    vi.unstubAllGlobals()
    expect(toast.error).toHaveBeenCalled()
  })

  it("racine des transcripts : celle des données, ou la valeur par défaut", () => {
    const { result } = setup()
    expect(result.current.actions.root).toBe(`${HOME}/.claude/projects`)
    const defaulted = renderHook(() => result.current.actions.root)
    expect(defaulted.result.current).toBe(`${HOME}/.claude/projects`)
  })
})
