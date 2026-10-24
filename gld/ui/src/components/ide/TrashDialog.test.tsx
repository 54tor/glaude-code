// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type FsRes } from "@/lib/api"
import { TrashDialog } from "./TrashDialog"

const res = (status: number, body: object = {}): FsRes => ({ status, ok: status < 300, body })
const items = [
  { id: "2", path: "src/b.ts", mtime: 1_700_000_100 },
  { id: "1", path: "a.txt", mtime: 1_700_000_000 },
]
const list = () => vi.spyOn(api, "fsTrash").mockResolvedValue({ status: 200, ok: true, body: { items } })
const mount = (canWrite = true, onRestored = vi.fn()) => render(<TrashDialog dir="/p" open onClose={() => {}} canWrite={canWrite} onRestored={onRestored} />)

beforeEach(() => setLang("fr"))
afterEach(() => (cleanup(), vi.restoreAllMocks()))

describe("TrashDialog", () => {
  it("liste les éléments avec chemin et date", async () => {
    list()
    mount()
    expect(await screen.findByText("src/b.ts")).toBeTruthy()
    expect(screen.getByText("a.txt")).toBeTruthy()
    expect(screen.getAllByRole("listitem")).toHaveLength(2)
    expect(screen.getAllByRole("time")).toHaveLength(2)
  })

  it("état vide", async () => {
    vi.spyOn(api, "fsTrash").mockResolvedValue({ status: 200, ok: true, body: { items: [] } })
    mount()
    expect(await screen.findByText("La corbeille est vide.")).toBeTruthy()
  })

  it("état d'erreur", async () => {
    vi.spyOn(api, "fsTrash").mockResolvedValue(res(500, { error: "boom" }))
    mount()
    expect(await screen.findByRole("alert")).toBeTruthy()
  })

  it("restaurer : appel exact, liste relue, arbre actualisé", async () => {
    const spy = list()
    const restore = vi.spyOn(api, "fsTrashRestore").mockResolvedValue(res(200, { ok: true }))
    const onRestored = vi.fn()
    mount(true, onRestored)
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    await waitFor(() => expect(restore).toHaveBeenCalledWith("/p", "1"))
    await waitFor(() => expect(onRestored).toHaveBeenCalled())
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it("restaurer en 409 : message « existe déjà »", async () => {
    list()
    vi.spyOn(api, "fsTrashRestore").mockResolvedValue(res(409, { error: "exists" }))
    const onRestored = vi.fn()
    mount(true, onRestored)
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    expect(await screen.findByText(/existe déjà à cet emplacement/)).toBeTruthy()
    expect(onRestored).not.toHaveBeenCalled()
  })

  it("purger demande confirmation", async () => {
    list()
    const purge = vi.spyOn(api, "fsTrashPurge").mockResolvedValue(res(200, { ok: true }))
    mount()
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Supprimer définitivement a.txt" }))
    expect(purge).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole("button", { name: "Supprimer définitivement" }))
    await waitFor(() => expect(purge).toHaveBeenCalledWith("/p", "1"))
  })

  it("sans fs_write : restaurer et purger désactivés avec la raison", async () => {
    list()
    const restore = vi.spyOn(api, "fsTrashRestore")
    mount(false)
    await screen.findByText("a.txt")
    const b = screen.getByRole("button", { name: "Restaurer a.txt" }) as HTMLButtonElement
    expect(b.disabled).toBe(true)
    expect((screen.getByRole("button", { name: "Supprimer définitivement a.txt" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText("Vous n'avez pas le droit de modifier les fichiers de ce dossier")).toBeTruthy()
    fireEvent.click(b)
    expect(restore).not.toHaveBeenCalled()
  })
})
