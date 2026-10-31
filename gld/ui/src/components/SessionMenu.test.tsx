// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Session, type VGroup } from "@/lib/api"
import { resetShares } from "@/hooks/useShares"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ActionsCtx, type Actions, type Row } from "./actions"
import { DD, GroupItems, SessionDropdown, SessionItems } from "./SessionMenu"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const session = (id: string, overrides: Partial<Session> = {}): Session => ({ id, dir: `dir-${id}`, title: `Titre ${id}`, named: true, cwd: "/home/bob/p", mtime: 1, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 }, ...overrides })
const row = (id: string, project = "/home/bob/p", missing = false): Row => ({ s: session(id), g: { project, missing, sessions: [] } })

type Overrides = Partial<Record<keyof Actions, unknown>>
const buildActions = (overrides: Overrides = {}) => {
  const calls = {
    read: vi.fn(),
    open: vi.fn(),
    resumeWith: vi.fn(),
    newSession: vi.fn(),
    info: vi.fn(),
    copy: vi.fn(),
    exportMd: vi.fn(),
    toggleFav: vi.fn(),
    rename: vi.fn(),
    share: vi.fn(),
    ungroup: vi.fn(),
    group: vi.fn(),
    pick: vi.fn(),
    move: vi.fn(),
    stop: vi.fn(),
    remove: vi.fn(),
  }
  const groupActs = { rows: vi.fn(() => [row("a"), row("b")]), open: vi.fn(), refs: vi.fn(), prompt: vi.fn(), export: vi.fn(), select: vi.fn(), fav: vi.fn(), rename: vi.fn(), remove: vi.fn(), show: vi.fn() }
  const actions = { ...calls, groupActs, vgroups: [] as VGroup[], favs: new Set<string>(), live: new Set<string>(), picked: new Set<string>(), selected: [] as Row[], inGroup: null, ...overrides } as unknown as Actions
  return { actions, calls, groupActs }
}

const menu = (actions: Actions, content: React.ReactElement) =>
  render(
    <ActionsCtx.Provider value={actions}>
      <DropdownMenu open>
        <DropdownMenuTrigger>ouvrir</DropdownMenuTrigger>
        <DropdownMenuContent>{content}</DropdownMenuContent>
      </DropdownMenu>
    </ActionsCtx.Provider>
  )
const choose = async (label: string | RegExp) => {
  fireEvent.click(await screen.findByText(label))
  await act(async () => {})
}

beforeEach(() => {
  resetShares()
  vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("SessionItems — une session", () => {
  it("chaque entrée appelle l'action de la bonne session", async () => {
    const { actions, calls } = buildActions()
    const target = row("s1")
    menu(actions, <SessionItems M={DD} row={target} />)
    await choose("Reprendre avec un message…")
    expect(calls.resumeWith).toHaveBeenCalledWith(target)
    await choose("Nouvelle session dans ce projet…")
    expect(calls.newSession).toHaveBeenCalledWith("/home/bob/p")
    await choose("Informations…")
    expect(calls.info).toHaveBeenCalledWith(target)
    await choose("Copier l'identifiant")
    expect(calls.copy).toHaveBeenCalledWith(target.s)
    await choose("Exporter en Markdown")
    expect(calls.exportMd).toHaveBeenCalledWith(target)
    await choose("Renommer…")
    expect(calls.rename).toHaveBeenCalledWith(target.s)
    await choose("Déplacer vers un autre dossier…")
    expect(calls.move).toHaveBeenCalledWith([target])
    await choose("Ajouter aux favoris")
    expect(calls.toggleFav).toHaveBeenCalledWith(["s1"])
    await choose("Partager…")
    expect(calls.share).toHaveBeenCalledWith(target)
  })

  it("dossier disparu : ni reprise ni nouvelle session", async () => {
    const { actions } = buildActions()
    menu(actions, <SessionItems M={DD} row={row("s1", "/home/bob/p", true)} />)
    await screen.findByText("Informations…")
    expect(screen.queryByText("Reprendre avec un message…")).toBeNull()
    expect(screen.queryByText("Nouvelle session dans ce projet…")).toBeNull()
  })

  it("favori : propose de le retirer", async () => {
    const { actions, calls } = buildActions({ favs: new Set(["s1"]) })
    menu(actions, <SessionItems M={DD} row={row("s1")} />)
    expect(screen.queryByText("Ajouter aux favoris")).toBeNull()
    await choose("Retirer des favoris")
    expect(calls.toggleFav).toHaveBeenCalledWith(["s1"])
  })

  it("« Fermer la session » seulement si elle est ouverte", async () => {
    const closed = buildActions()
    const first = menu(closed.actions, <SessionItems M={DD} row={row("s1")} />)
    await screen.findByText("Informations…")
    expect(screen.queryByText("Fermer la session")).toBeNull()
    first.unmount()
    const open = buildActions({ live: new Set(["s1"]) })
    menu(open.actions, <SessionItems M={DD} row={row("s1")} />)
    await choose("Fermer la session")
    expect(open.calls.stop).toHaveBeenCalledWith(["s1"])
  })

  it("suppression : cible dir+id, titre cité et rappel après confirmation", async () => {
    const { actions, calls } = buildActions()
    const after = vi.fn()
    menu(actions, <SessionItems M={DD} row={row("s1")} after={after} />)
    await choose("Supprimer")
    expect(calls.remove).toHaveBeenCalledWith([{ dir: "dir-s1", id: "s1" }], "« Titre s1 »", after)
  })

  it("entrées « full » (clic droit) : lire, reprendre dans un terminal, sélectionner", async () => {
    const { actions, calls } = buildActions()
    const target = row("s1")
    menu(actions, <SessionItems M={DD} row={target} full />)
    await choose("Lire la conversation")
    expect(calls.read).toHaveBeenCalledWith(target.s)
    await choose("Reprendre dans un terminal")
    expect(calls.open).toHaveBeenCalledWith({ dir: "dir-s1", id: "s1" })
    await choose("Sélectionner")
    expect(calls.pick).toHaveBeenCalledWith("s1")
  })

  it("sans « full » : pas de lecture ni de sélection", async () => {
    const { actions } = buildActions()
    menu(actions, <SessionItems M={DD} row={row("s1")} />)
    await screen.findByText("Informations…")
    expect(screen.queryByText("Lire la conversation")).toBeNull()
    expect(screen.queryByText("Sélectionner")).toBeNull()
  })

  it("session cochée : l'entrée propose de la désélectionner", async () => {
    const { actions } = buildActions({ picked: new Set(["s1"]), selected: [row("s1")] })
    menu(actions, <SessionItems M={DD} row={row("s1")} full />)
    expect(await screen.findByText("Désélectionner")).toBeTruthy()
  })

  it("dans un groupe virtuel : « Retirer du groupe » ne retire que cette session", async () => {
    const { actions, calls } = buildActions({ inGroup: { name: "G", project: "/home/bob/p", ids: ["s1"] } })
    menu(actions, <SessionItems M={DD} row={row("s1")} />)
    await choose("Retirer du groupe")
    expect(calls.ungroup).toHaveBeenCalledWith(["s1"])
  })

  it("partage existant : l'entrée l'indique", async () => {
    vi.mocked(api.shares).mockResolvedValue({ shares: [{ id: "s1", dir: "/d", title: "t", handle: "h", open: true, invites: [{ id: "i1" } as never] }] })
    const { actions } = buildActions()
    menu(actions, <SessionItems M={DD} row={row("s1")} />)
    expect(await screen.findByText("Partage…")).toBeTruthy()
    expect(screen.queryByText("Partager…")).toBeNull()
  })

  describe("groupes", () => {
    const groups: VGroup[] = [
      { name: "Refonte", project: "/home/bob/p", ids: ["s1"] },
      { name: "Autre", project: "/home/bob/p", ids: [] },
      { name: "Ailleurs", project: "/home/bob/q", ids: [] },
    ]

    it("le sous-menu ne liste que les groupes du dossier et marque ceux qui contiennent la session", async () => {
      const { actions } = buildActions({ vgroups: groups })
      menu(actions, <SessionItems M={DD} row={row("s1")} />)
      fireEvent.click(await screen.findByText("Groupe"))
      await screen.findByText("Refonte")
      expect(screen.getByText("Autre")).toBeTruthy()
      expect(screen.queryByText("Ailleurs")).toBeNull()
    })

    it("un groupe contenant déjà la session la retire ; un autre l'y ajoute", async () => {
      const { actions, calls } = buildActions({ vgroups: groups })
      menu(actions, <SessionItems M={DD} row={row("s1")} />)
      fireEvent.click(await screen.findByText("Groupe"))
      await choose("Refonte")
      expect(calls.ungroup).toHaveBeenCalledWith(["s1"], groups[0])
      expect(calls.group).not.toHaveBeenCalled()
    })

    it("ajout à un groupe existant", async () => {
      const { actions, calls } = buildActions({ vgroups: groups })
      menu(actions, <SessionItems M={DD} row={row("s1")} />)
      fireEvent.click(await screen.findByText("Groupe"))
      await choose("Autre")
      expect(calls.group).toHaveBeenCalledWith(["s1"], "/home/bob/p", "Autre")
    })

    it("nouveau groupe : nom null pour le demander", async () => {
      const { actions, calls } = buildActions({ vgroups: groups })
      menu(actions, <SessionItems M={DD} row={row("s1")} />)
      fireEvent.click(await screen.findByText("Groupe"))
      await choose("Nouveau groupe…")
      expect(calls.group).toHaveBeenCalledWith(["s1"], "/home/bob/p", null)
    })
  })
})

describe("SessionItems — sélection multiple", () => {
  const selection = [row("s1"), row("s2")]
  const selectionActions = (overrides: Overrides = {}) => buildActions({ picked: new Set(["s1", "s2"]), selected: selection, ...overrides })

  it("s'applique à toute la sélection, avec son effectif en titre", async () => {
    const { actions, calls } = selectionActions({ live: new Set(["s2"]) })
    menu(actions, <SessionItems M={DD} row={selection[0]} />)
    expect(await screen.findByText("2 sessions sélectionnées")).toBeTruthy()
    expect(screen.queryByText("Renommer…")).toBeNull()
    await choose("Ajouter aux favoris")
    expect(calls.toggleFav).toHaveBeenCalledWith(["s1", "s2"])
    await choose("Déplacer vers un autre dossier…")
    expect(calls.move).toHaveBeenCalledWith(selection)
    await choose("Fermer les sessions ouvertes")
    expect(calls.stop).toHaveBeenCalledWith(["s2"])
  })

  it("suppression groupée", async () => {
    const { actions, calls } = selectionActions()
    menu(actions, <SessionItems M={DD} row={selection[0]} />)
    await choose("Supprimer 2 sessions")
    expect(calls.remove).toHaveBeenCalledWith([{ dir: "dir-s1", id: "s1" }, { dir: "dir-s2", id: "s2" }], "les 2 sessions sélectionnées")
  })

  it("aucune session ouverte : pas de « Fermer les sessions ouvertes »", async () => {
    const { actions } = selectionActions()
    menu(actions, <SessionItems M={DD} row={selection[0]} />)
    await screen.findByText("2 sessions sélectionnées")
    expect(screen.queryByText("Fermer les sessions ouvertes")).toBeNull()
  })

  it("sessions de plusieurs dossiers : le groupe est désactivé", async () => {
    const mixed = [row("s1", "/home/bob/p"), row("s2", "/home/bob/q")]
    const { actions } = buildActions({ picked: new Set(["s1", "s2"]), selected: mixed })
    menu(actions, <SessionItems M={DD} row={mixed[0]} />)
    const mixedItem = await screen.findByText("Groupe : sessions de plusieurs dossiers")
    expect(mixedItem.closest("[role=menuitem]")?.getAttribute("aria-disabled")).toBe("true")
  })

  it("une seule session cochée : menu de session simple", async () => {
    const { actions } = buildActions({ picked: new Set(["s1"]), selected: [row("s1")] })
    menu(actions, <SessionItems M={DD} row={row("s1")} />)
    expect(await screen.findByText("Renommer…")).toBeTruthy()
  })

  it("dans un groupe : retire toute la sélection", async () => {
    const { actions, calls } = selectionActions({ inGroup: { name: "G", project: "/home/bob/p", ids: ["s1", "s2"] } })
    menu(actions, <SessionItems M={DD} row={selection[0]} />)
    await choose("Retirer du groupe")
    expect(calls.ungroup).toHaveBeenCalledWith(["s1", "s2"])
  })
})

describe("GroupItems", () => {
  const virtualGroup: VGroup = { name: "Refonte", project: "/home/bob/p", ids: ["a", "b"] }

  it("entrées d'un groupe : chacune appelle l'action du groupe", async () => {
    const { actions, groupActs } = buildActions()
    menu(actions, <GroupItems M={DD} vg={virtualGroup} />)
    await choose("Afficher")
    expect(groupActs.show).toHaveBeenCalledWith(virtualGroup)
    await choose("Reprendre les 2 sessions")
    expect(groupActs.open).toHaveBeenCalledWith(virtualGroup)
    await choose("Nouvelle session avec ces références…")
    expect(groupActs.prompt).toHaveBeenCalledWith(virtualGroup)
    await choose("Copier les références")
    expect(groupActs.refs).toHaveBeenCalledWith(virtualGroup)
    await choose("Exporter en Markdown")
    expect(groupActs.export).toHaveBeenCalledWith(virtualGroup)
    await choose("Renommer…")
    expect(groupActs.rename).toHaveBeenCalledWith(virtualGroup)
    await choose("Sélectionner ses sessions")
    expect(groupActs.select).toHaveBeenCalledWith(virtualGroup)
    await choose("Supprimer le groupe")
    expect(groupActs.remove).toHaveBeenCalledWith(virtualGroup)
  })

  it("sur sa propre page : pas d'entrée « Afficher »", async () => {
    const { actions } = buildActions()
    menu(actions, <GroupItems M={DD} vg={virtualGroup} page />)
    await screen.findByText("Refonte")
    expect(screen.queryByText("Afficher")).toBeNull()
  })

  it("groupe sans session vivante : les actions de masse sont désactivées, renommer et supprimer restent", async () => {
    const { actions, groupActs } = buildActions()
    groupActs.rows.mockReturnValue([])
    menu(actions, <GroupItems M={DD} vg={virtualGroup} />)
    for (const label of ["Reprendre les 0 session", "Copier les références", "Exporter en Markdown", "Sélectionner ses sessions"]) {
      const item = (await screen.findByText(label)).closest("[role=menuitem]")
      expect(item?.getAttribute("aria-disabled")).toBe("true")
    }
    expect(screen.getByText("Renommer…").closest("[role=menuitem]")?.getAttribute("aria-disabled")).toBeNull()
  })

  it("groupe favori : propose de le retirer des favoris", async () => {
    const { actions, groupActs } = buildActions()
    menu(actions, <GroupItems M={DD} vg={{ ...virtualGroup, fav: true }} />)
    await choose("Retirer des favoris")
    expect(groupActs.fav).toHaveBeenCalled()
  })
})

describe("SessionDropdown", () => {
  it("le menu s'ouvre sur le déclencheur, avec les entrées « extra » en tête", async () => {
    const { actions } = buildActions()
    render(
      <ActionsCtx.Provider value={actions}>
        <SessionDropdown row={row("s1")} extra={<div>entrée-lecteur</div>}>
          <button type="button">plus</button>
        </SessionDropdown>
      </ActionsCtx.Provider>
    )
    expect(screen.queryByText("Informations…")).toBeNull()
    fireEvent.pointerDown(screen.getByRole("button", { name: "plus" }), { button: 0, ctrlKey: false })
    expect(await screen.findByText("entrée-lecteur")).toBeTruthy()
    expect(screen.getByText("Informations…")).toBeTruthy()
  })
})
