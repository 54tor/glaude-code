// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type GitState, type Group, type Project, type Session, type VGroup } from "@/lib/api"
import { resetGit } from "@/hooks/useGit"
import { resetOpen } from "@/hooks/useOpen"
import { buildTree } from "@/lib/tree"
import { ContextMenu, ContextMenuTrigger } from "@/components/ui/context-menu"
import { ActionsCtx, type Actions } from "./actions"
import { GroupRow, ProjectItems, ProjectTree } from "./ProjectTree"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const HOME = "/home/bob"
const flush = () => act(async () => {})
const session = (id: string): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: HOME, mtime: 1, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 } })
const group = (project: string, sessionIds: string[], missing = false): Group => ({ project, missing, sessions: sessionIds.map(session) })
const entry = (project: string, sessionIds: string[], missing = false) => {
  const built = group(project, sessionIds, missing)
  return { g: built, ss: built.sessions }
}
const gitState = (overrides: Partial<GitState> = {}): GitState => ({ branch: "main", upstream: true, ahead: 0, behind: 0, changed: 0, untracked: 0, ...overrides })

type Overrides = Partial<Record<keyof Actions, unknown>>
const buildActions = (overrides: Overrides = {}) => {
  const projectActs = { add: vi.fn(), remove: vi.fn(), rename: vi.fn(), delete: vi.fn(), promptAll: vi.fn() }
  const groupActs = { rows: vi.fn(() => [1, 2, 3]), open: vi.fn(), refs: vi.fn(), prompt: vi.fn(), export: vi.fn(), select: vi.fn(), fav: vi.fn(), rename: vi.fn(), remove: vi.fn(), show: vi.fn() }
  const calls = { open: vi.fn(), newSession: vi.fn(), moveGroup: vi.fn(), stop: vi.fn(), remove: vi.fn(), removeDir: vi.fn() }
  const actions = { ...calls, projectActs, groupActs, home: HOME, live: new Set<string>(), projects: [] as Project[], vgroups: [] as VGroup[], registeredDirs: [] as string[], ...overrides } as unknown as Actions
  return { actions, calls, projectActs, groupActs }
}

type TreeOptions = { selected?: string; selectedGroup?: string; actions?: Overrides }
const mountTree = async (entries: ReturnType<typeof entry>[], options: TreeOptions = {}) => {
  const built = buildActions(options.actions)
  const onSel = vi.fn()
  const onGroup = vi.fn()
  render(
    <ActionsCtx.Provider value={built.actions}>
      <ProjectTree nodes={buildTree(entries, HOME)} sel={options.selected ?? ""} onSel={onSel} selGroup={options.selectedGroup ?? ""} onGroup={onGroup} />
    </ActionsCtx.Provider>
  )
  await flush()
  return { ...built, onSel, onGroup }
}

beforeEach(() => {
  localStorage.clear()
  resetOpen()
  resetGit()
  vi.spyOn(api, "gitStatus").mockResolvedValue({ status: {}, repos: {} })
  vi.spyOn(api, "notes").mockResolvedValue({ notes: {} })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("ProjectTree", () => {
  it("deux dossiers frères : chacun une ligne avec son nombre de sessions", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1", "2"]), entry(`${HOME}/Projets/b`, ["3"])])
    expect(screen.getByRole("button", { name: /^a/ }).title).toBe(`${HOME}/Projets/a`)
    expect(screen.getByTitle("2 session(s)")).toBeTruthy()
    expect(screen.getByTitle("1 session(s)")).toBeTruthy()
  })

  it("un clic sur le dossier le sélectionne par sa clé", async () => {
    const { onSel } = await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])])
    fireEvent.click(screen.getByRole("button", { name: /^a/ }))
    expect(onSel).toHaveBeenCalledWith(expect.stringContaining("a"))
  })

  it("racine commune sans session : seuls ses sous-dossiers sont montrés, indentés au premier niveau", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])])
    expect(screen.queryByRole("button", { name: /^Projets/ })).toBeNull()
    expect(screen.getAllByRole("listitem").length).toBeGreaterThanOrEqual(2)
  })

  it("dossier imbriqué : replié par défaut, déplié par le chevron, état retenu dans le navigateur", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/a/sub`, ["2"]), entry(`${HOME}/Projets/b`, ["3"])])
    expect(screen.queryByRole("button", { name: /^sub/ })).toBeNull()
    const [expand] = screen.getAllByRole("button", { name: "Déplier" })
    fireEvent.click(expand)
    expect(screen.getByRole("button", { name: /^sub/ })).toBeTruthy()
    expect(Object.values(JSON.parse(localStorage.getItem("gld.open") ?? "{}"))).toEqual([true])
    fireEvent.click(screen.getByRole("button", { name: "Replier" }))
    expect(screen.queryByRole("button", { name: /^sub/ })).toBeNull()
  })

  it("le dossier sélectionné ouvre le chemin qui y mène", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/a/sub`, ["2"]), entry(`${HOME}/Projets/b`, ["3"])], { selected: "~/Projets/a/sub" })
    expect(screen.getByRole("button", { name: /^sub/ })).toBeTruthy()
    expect(screen.getByRole("button", { name: /^sub/ }).getAttribute("aria-current")).toBe("true")
  })

  it("choisir un dossier replié qui a des enfants le déplie", async () => {
    const { onSel } = await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/a/sub`, ["2"]), entry(`${HOME}/Projets/b`, ["3"])])
    fireEvent.click(screen.getByRole("button", { name: /^a/ }))
    expect(onSel).toHaveBeenCalled()
    expect(screen.getByRole("button", { name: /^sub/ })).toBeTruthy()
  })

  it("dossier introuvable : avertissement accessible", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1"], true), entry(`${HOME}/Projets/b`, ["2"])])
    expect(screen.getAllByLabelText("dossier introuvable")).toHaveLength(1)
  })

  it("état git : somme des fichiers modifiés et non suivis du dossier", async () => {
    vi.mocked(api.gitStatus).mockResolvedValue({ status: { [`${HOME}/Projets/a`]: gitState({ changed: 2, untracked: 1 }) }, repos: {} })
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])])
    expect(screen.getByTitle("3 changement(s) en attente")).toBeTruthy()
  })

  it("session ouverte dans le dossier : compteur d'activité", async () => {
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])], { actions: { live: new Set(["1"]) } })
    expect(screen.getByTitle("1 session(s), dont une ouverte")).toBeTruthy()
  })

  it("groupes virtuels : listés sous le dossier, triés par nom, avec leur nombre de sessions ; un clic les affiche", async () => {
    const vgroups: VGroup[] = [{ name: "Zèbre", project: `${HOME}/Projets/a`, ids: ["1"] }, { name: "Alpha", project: `${HOME}/Projets/a`, ids: ["1"] }]
    const { onGroup } = await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])], { selected: "~/Projets/a", actions: { vgroups } })
    const groupButtons = screen.getAllByTitle(/Groupe « /).map((button) => button.getAttribute("title"))
    expect(groupButtons[0]).toContain("Alpha")
    expect(groupButtons[1]).toContain("Zèbre")
    fireEvent.click(screen.getByTitle(/Alpha/))
    expect(onGroup).toHaveBeenCalledWith(vgroups[1], expect.stringContaining("a"))
  })

  it("groupe affiché : marqué comme page courante", async () => {
    const vgroups: VGroup[] = [{ name: "Alpha", project: `${HOME}/Projets/a`, ids: ["1"] }]
    await mountTree([entry(`${HOME}/Projets/a`, ["1"]), entry(`${HOME}/Projets/b`, ["2"])], { selected: "~/Projets/a", selectedGroup: "~/Projets/a\0Alpha", actions: { vgroups } })
    expect(screen.getByTitle(/Alpha/).getAttribute("aria-current")).toBe("true")
  })

  it("dépôts git sous un dossier de sessions : ligne repliée qui se déplie", async () => {
    vi.mocked(api.gitStatus).mockResolvedValue({
      status: { [`${HOME}/php/8.1`]: gitState({ changed: 1 }), [`${HOME}/php/8.2`]: gitState() },
      repos: { [`${HOME}/php`]: [`${HOME}/php/8.1`, `${HOME}/php/8.2`] },
    })
    await mountTree([entry(`${HOME}/php`, ["1"]), entry(`${HOME}/other`, ["2"])], { selected: "~/php" })
    const reposRow = screen.getByRole("button", { name: /2 dépôts/ })
    expect(reposRow.getAttribute("aria-expanded")).toBe("false")
    expect(screen.queryByText("8.1")).toBeNull()
    fireEvent.click(reposRow)
    expect(screen.getByText("8.1")).toBeTruthy()
    expect(screen.getByText("8.2")).toBeTruthy()
  })
})

describe("ProjectItems (menu contextuel d'un dossier)", () => {
  const openMenu = async (target: Group, options: { sessions?: Session[]; actions?: Overrides } = {}) => {
    const built = buildActions(options.actions)
    render(
      <ActionsCtx.Provider value={built.actions}>
        <ProjectMenuHost group={target} sessions={options.sessions} />
      </ActionsCtx.Provider>
    )
    fireEvent.contextMenu(screen.getByText("dossier"))
    await flush()
    return built
  }
  const choose = async (label: string | RegExp) => {
    fireEvent.click(await screen.findByText(label))
    await flush()
  }
  const reopen = async () => {
    fireEvent.contextMenu(screen.getByText("dossier"))
    await flush()
  }

  it("reprendre les récentes ouvre le dossier de la première session", async () => {
    const { calls } = await openMenu(group("/home/bob/p", ["1", "2"]))
    await choose("Reprendre les récentes")
    expect(calls.open).toHaveBeenCalledWith({ dir: "dir-1" })
  })

  it("nouvelle session dans le dossier", async () => {
    const { calls } = await openMenu(group("/home/bob/p", ["1"]))
    await choose("Nouvelle session…")
    expect(calls.newSession).toHaveBeenCalledWith("/home/bob/p")
  })

  it("dossier disparu : ni reprise ni nouvelle session", async () => {
    await openMenu(group("/home/bob/p", ["1"], true))
    await screen.findByText("Projet")
    expect(screen.queryByText("Reprendre les récentes")).toBeNull()
    expect(screen.queryByText("Nouvelle session…")).toBeNull()
  })

  it("sans session : pas de réassignation, de fermeture ni de suppression", async () => {
    await openMenu(group("/home/bob/p", []))
    await screen.findByText("Projet")
    expect(screen.queryByText(/Réassigner/)).toBeNull()
    expect(screen.queryByText(/Supprimer les/)).toBeNull()
  })

  it("réassigner et supprimer visent les sessions du dossier", async () => {
    const target = group("/home/bob/p", ["1", "2"])
    const { calls } = await openMenu(target)
    await choose("Réassigner les 2 sessions…")
    expect(calls.moveGroup).toHaveBeenCalledWith(target, [{ dir: "dir-1", id: "1" }, { dir: "dir-2", id: "2" }])
    await reopen()
    await choose("Supprimer les 2 sessions")
    expect(calls.remove).toHaveBeenCalledWith([{ dir: "dir-1", id: "1" }, { dir: "dir-2", id: "2" }], "les 2 session(s) de /home/bob/p")
  })

  it("fermer les ouvertes : uniquement celles qui tournent", async () => {
    const { calls } = await openMenu(group("/home/bob/p", ["1", "2", "3"]), { actions: { live: new Set(["2", "3"]) } })
    await choose("Fermer les sessions ouvertes (2)")
    expect(calls.stop).toHaveBeenCalledWith(["2", "3"])
  })

  it("aucune session ouverte : pas d'entrée de fermeture", async () => {
    await openMenu(group("/home/bob/p", ["1"]))
    await screen.findByText("Projet")
    expect(screen.queryByText(/Fermer les sessions ouvertes/)).toBeNull()
  })

  it("sous-menu Projet : coche ceux qui contiennent le dossier, retire ou ajoute", async () => {
    const projects: Project[] = [{ name: "Refonte", dirs: ["/home/bob/p"] }, { name: "Autre", dirs: [] }]
    const { projectActs } = await openMenu(group("/home/bob/p", ["1"]), { actions: { projects } })
    fireEvent.click(await screen.findByText("Projet"))
    await choose("Refonte")
    expect(projectActs.remove).toHaveBeenCalledWith("/home/bob/p", "Refonte")
    await reopen()
    fireEvent.click(await screen.findByText("Projet"))
    await choose("Autre")
    expect(projectActs.add).toHaveBeenCalledWith("/home/bob/p", "Autre")
  })

  it("nouveau projet : nom null pour le demander", async () => {
    const { projectActs } = await openMenu(group("/home/bob/p", ["1"]))
    fireEvent.click(await screen.findByText("Projet"))
    await choose("Nouveau projet…")
    expect(projectActs.add).toHaveBeenCalledWith("/home/bob/p", null)
  })

  it("dossier ajouté à la main : peut être retiré de la liste", async () => {
    const { calls } = await openMenu(group("/home/bob/p", []), { actions: { registeredDirs: ["/home/bob/p"] } })
    await choose("Retirer de la liste des dossiers")
    expect(calls.removeDir).toHaveBeenCalledWith("/home/bob/p")
  })

  it("dossier issu des sessions : pas de retrait de liste", async () => {
    await openMenu(group("/home/bob/p", ["1"]))
    await screen.findByText("Projet")
    expect(screen.queryByText("Retirer de la liste des dossiers")).toBeNull()
  })

  it("la liste de sessions passée en paramètre remplace celles du groupe", async () => {
    const { calls } = await openMenu(group("/home/bob/p", ["1", "2", "3"]), { sessions: [session("9")] })
    await choose("Réassigner les 1 sessions…")
    expect(calls.moveGroup).toHaveBeenCalledWith(expect.anything(), [{ dir: "dir-9", id: "9" }])
  })
})

describe("GroupRow", () => {
  const virtualGroup: VGroup = { name: "Refonte", project: "/home/bob/p", ids: ["1"], fav: true }

  it("affiche le nom, l'étoile des favoris et le nombre de sessions du groupe", () => {
    const { actions } = buildActions()
    render(
      <ActionsCtx.Provider value={actions}>
        <GroupRow vg={virtualGroup} dirKey="p" project="/home/bob/p" depth={0} active={false} onGroup={() => {}} />
      </ActionsCtx.Provider>
    )
    const groupButton = screen.getByRole("button")
    expect(within(groupButton).getByText("Refonte")).toBeTruthy()
    expect(within(groupButton).getByLabelText("favori")).toBeTruthy()
    expect(groupButton.textContent).toContain("3")
  })

  it("clic droit : les entrées du groupe", async () => {
    const { actions, groupActs } = buildActions()
    render(
      <ActionsCtx.Provider value={actions}>
        <GroupRow vg={virtualGroup} dirKey="p" project="/home/bob/p" depth={0} active onGroup={() => {}} />
      </ActionsCtx.Provider>
    )
    fireEvent.contextMenu(screen.getByRole("button"))
    fireEvent.click(await screen.findByText("Supprimer le groupe"))
    expect(groupActs.remove).toHaveBeenCalledWith(virtualGroup)
  })
})

function ProjectMenuHost({ group: target, sessions }: { group: Group; sessions?: Session[] }) {
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div>dossier</div>
      </ContextMenuTrigger>
      <ProjectItems g={target} ss={sessions} />
    </ContextMenu>
  )
}
