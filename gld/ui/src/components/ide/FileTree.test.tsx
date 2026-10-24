// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type FsEntry } from "@/lib/api"
import { FileTree, type TreeActions } from "./FileTree"
import { useTree } from "./useTree"

const e = (name: string, type: FsEntry["type"]): FsEntry => ({ name, type, size: 1, mtime: 1 })
const root = [e("src", "dir"), e("link", "symlink"), e("a.txt", "file")]
const tree = (over: Record<string, { entries: FsEntry[]; truncated: boolean }> = {}) =>
  vi.spyOn(api, "fsTree").mockImplementation(async (_d, p) => over[p] ?? { entries: p === "" ? root : [e("b.go", "file")], truncated: false })

beforeEach(() => {
  setLang("fr")
  localStorage.clear()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

type P = Omit<Parameters<typeof FileTree>[0], "tree">
function Host(p: P) {
  return <FileTree {...p} tree={useTree(p.dir)} />
}
const mount = (o: Partial<P> = {}) => render(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} {...o} />)

describe("FileTree", () => {
  it("ne charge que la racine au montage, puis un seul appel au dépliage", async () => {
    const spy = tree()
    mount()
    await screen.findByText("src")
    expect(spy.mock.calls).toEqual([["/p", ""]])
    fireEvent.click(screen.getByLabelText("Déplier src"))
    await screen.findByText("b.go")
    expect(spy.mock.calls).toEqual([["/p", ""], ["/p", "src"]])
  })

  it("un lien symbolique n'a ni flèche ni ouverture", async () => {
    tree()
    const open = vi.fn()
    mount({ onOpen: open })
    const row = (await screen.findByText("link")).closest("[role=treeitem]")!
    expect(row.hasAttribute("aria-expanded")).toBe(false)
    expect(screen.queryByLabelText("Déplier link")).toBeNull()
    fireEvent.click(row)
    fireEvent.keyDown(row, { key: "Enter" })
    expect(open).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText("a.txt"))
    expect(open).toHaveBeenCalledWith("a.txt")
  })

  it("avertit quand la liste est tronquée", async () => {
    tree({ "": { entries: root, truncated: true } })
    mount()
    await screen.findByText("src")
    expect(screen.getByText(/tronquée/)).toBeTruthy()
  })

  it("menu contextuel masqué sans fs_write, présent avec", async () => {
    tree()
    const { unmount } = mount()
    fireEvent.contextMenu(await screen.findByText("a.txt"))
    expect(screen.queryByText("Renommer")).toBeNull()
    unmount()
    mount({ canWrite: true })
    fireEvent.contextMenu(await screen.findByText("a.txt"))
    expect(await screen.findByText("Renommer")).toBeTruthy()
    expect(screen.getByText("Supprimer")).toBeTruthy()
  })

  it("création à la racine via la poignée exposée : flux de création existant", async () => {
    const spy = tree()
    const fsNew = vi.spyOn(api, "fsNew").mockResolvedValue({ status: 200, ok: true, body: { ok: true } })
    const actions: { current: TreeActions | null } = { current: null }
    mount({ canWrite: true, actionsRef: actions })
    await screen.findByText("src")
    expect(screen.queryByRole("button", { name: "Nouveau fichier" })).toBeNull()
    act(() => actions.current!.create("newFile"))
    fireEvent.change(await screen.findByPlaceholderText("Nom"), { target: { value: "n.txt" } })
    fireEvent.click(screen.getByRole("button", { name: "Valider" }))
    await waitFor(() => expect(fsNew).toHaveBeenCalledWith("/p", "n.txt", "file"))
    await waitFor(() => expect(spy.mock.calls.filter((c) => c[1] === "").length).toBe(2))
    expect(localStorage.getItem("gld.ide./p.open") ?? "[]").toBe("[]")
    act(() => actions.current!.create("newDir"))
    expect(await screen.findByText("Nouveau dossier")).toBeTruthy()
  })

  it("filtre : sous-chaîne insensible à la casse sur les entrées chargées, ancêtres gardés, aucun appel réseau", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    const spy = tree()
    const { rerender } = mount({ filter: "B.G" })
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2))
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="B.G" />)
    expect(await screen.findByText("b.go")).toBeTruthy()
    expect(screen.getByText("src")).toBeTruthy()
    expect(screen.queryByText("a.txt")).toBeNull()
    expect(screen.queryByText("link")).toBeNull()
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="zzz" />)
    expect(screen.getByText("Aucune entrée chargée ne correspond.")).toBeTruthy()
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="" />)
    expect(screen.getByText("a.txt")).toBeTruthy()
    expect(screen.getByText("link")).toBeTruthy()
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it("filtre : un dossier chargé puis replié est aussi parcouru", async () => {
    const spy = tree()
    const { rerender } = mount()
    fireEvent.click(await screen.findByLabelText("Déplier src"))
    await screen.findByText("b.go")
    fireEvent.click(screen.getByLabelText("Replier src"))
    await waitFor(() => expect(screen.queryByText("b.go")).toBeNull())
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="b.go" />)
    expect(screen.getByText("b.go")).toBeTruthy()
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it("navigue au clavier", async () => {
    tree()
    const open = vi.fn()
    mount({ onOpen: open })
    await screen.findByText("src")
    const t = screen.getByRole("tree")
    fireEvent.keyDown(t, { key: "ArrowDown" })
    fireEvent.keyDown(t, { key: "ArrowRight" })
    await screen.findByText("b.go")
    fireEvent.keyDown(t, { key: "ArrowDown" })
    fireEvent.keyDown(t, { key: "Enter" })
    expect(open).toHaveBeenCalledWith("src/b.go")
    fireEvent.keyDown(t, { key: "ArrowLeft" })
    fireEvent.keyDown(t, { key: "ArrowLeft" })
    await waitFor(() => expect(screen.queryByText("b.go")).toBeNull())
  })

  it("restaure les dossiers dépliés depuis le stockage local", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    const spy = tree()
    mount()
    await screen.findByText("b.go")
    expect(spy).toHaveBeenCalledTimes(2)
  })
  it("filtre actif : clic, Entrée et flèches ne basculent ni ne mémorisent les dossiers dépliés, sans appel réseau", async () => {
    const spy = tree()
    const { rerender } = mount()
    fireEvent.click(await screen.findByLabelText("Déplier src"))
    await screen.findByText("b.go")
    fireEvent.click(screen.getByLabelText("Replier src"))
    await waitFor(() => expect(screen.queryByText("b.go")).toBeNull())
    const before = localStorage.getItem("gld.ide./p.open")
    const calls = spy.mock.calls.length
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="b" />)
    const row = screen.getByText("src").closest("[role=treeitem]") as HTMLElement
    expect(row.getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(row)
    fireEvent.click(screen.getByLabelText("Replier src"))
    act(() => row.focus())
    const t = screen.getByRole("tree")
    for (const key of ["Enter", "ArrowLeft", "ArrowRight", "Enter"]) fireEvent.keyDown(t, { key })
    expect(screen.getByText("b.go")).toBeTruthy()
    expect(row.getAttribute("aria-expanded")).toBe("true")
    expect(spy.mock.calls.length).toBe(calls)
    expect(localStorage.getItem("gld.ide./p.open")).toBe(before)
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="" />)
    expect(screen.queryByText("b.go")).toBeNull()
    expect(screen.getByText("src").closest("[role=treeitem]")!.getAttribute("aria-expanded")).toBe("false")
  })

  it("filtre actif : flèche droite descend vers l'enfant affiché, flèche gauche remonte au parent", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    tree()
    const { rerender } = mount()
    await screen.findByText("b.go")
    rerender(<Host dir="/p" canWrite={false} selected="" onOpen={() => {}} filter="b" />)
    const t = screen.getByRole("tree")
    act(() => (screen.getByText("src").closest("[role=treeitem]") as HTMLElement).focus())
    fireEvent.keyDown(t, { key: "ArrowRight" })
    await waitFor(() => expect(document.activeElement?.getAttribute("data-path")).toBe("src/b.go"))
    fireEvent.keyDown(t, { key: "ArrowLeft" })
    await waitFor(() => expect(document.activeElement?.getAttribute("data-path")).toBe("src"))
    fireEvent.keyDown(t, { key: "ArrowLeft" })
    expect(screen.getByText("b.go")).toBeTruthy()
    expect(localStorage.getItem("gld.ide./p.open")).toBe(JSON.stringify(["src"]))
  })

  it("la poignée de création est vidée au démontage", async () => {
    tree()
    const actions: { current: TreeActions | null } = { current: null }
    const { unmount } = mount({ canWrite: true, actionsRef: actions })
    await screen.findByText("src")
    expect(actions.current).not.toBeNull()
    unmount()
    expect(actions.current).toBeNull()
  })
})
