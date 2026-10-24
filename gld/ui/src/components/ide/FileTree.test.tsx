// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type FsEntry } from "@/lib/api"
import { FileTree } from "./FileTree"

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

const mount = (o: Partial<Parameters<typeof FileTree>[0]> = {}) => render(<FileTree dir="/p" canWrite={false} selected="" onOpen={() => {}} {...o} />)

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

  it("boutons de création à la racine : masqués sans fs_write, créent à la racine avec", async () => {
    const spy = tree()
    const { unmount } = mount()
    await screen.findByText("src")
    expect(screen.queryByRole("button", { name: "Nouveau fichier" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Nouveau dossier" })).toBeNull()
    unmount()
    const fsNew = vi.spyOn(api, "fsNew").mockResolvedValue({ status: 200, ok: true, body: { ok: true } })
    mount({ canWrite: true })
    await screen.findByText("src")
    expect(screen.getByRole("button", { name: "Nouveau dossier" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Nouveau fichier" }))
    fireEvent.change(await screen.findByPlaceholderText("Nom"), { target: { value: "n.txt" } })
    fireEvent.click(screen.getByRole("button", { name: "Valider" }))
    await waitFor(() => expect(fsNew).toHaveBeenCalledWith("/p", "n.txt", "file"))
    await waitFor(() => expect(spy.mock.calls.filter((c) => c[1] === "").length).toBe(3))
    expect(localStorage.getItem("gld.ide./p.open") ?? "[]").toBe("[]")
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
})
