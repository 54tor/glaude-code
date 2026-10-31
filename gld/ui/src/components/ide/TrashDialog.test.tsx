// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { TrashDialog } from "./TrashDialog"

const items = [
  { id: "2", path: "src/b.ts", mtime: 1_700_000_100 },
  { id: "1", path: "a.txt", mtime: 1_700_000_000 },
]
const list = () => vi.spyOn(api, "fsTrash").mockResolvedValue({ items })
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
    vi.spyOn(api, "fsTrash").mockResolvedValue({ items: [] })
    mount()
    expect(await screen.findByText("La corbeille est vide.")).toBeTruthy()
  })

  it("état d'erreur", async () => {
    vi.spyOn(api, "fsTrash").mockRejectedValue(new ApiError(500, "internal", "boom"))
    mount()
    expect(await screen.findByRole("alert")).toBeTruthy()
  })

  it("restaurer : appel exact, liste relue, arbre actualisé", async () => {
    const spy = list()
    const restore = vi.spyOn(api, "fsTrashRestore").mockResolvedValue(undefined)
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
    vi.spyOn(api, "fsTrashRestore").mockRejectedValue(new ApiError(409, "exists", "exists"))
    const onRestored = vi.fn()
    mount(true, onRestored)
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    expect(await screen.findByText(/existe déjà à cet emplacement/)).toBeTruthy()
    expect(onRestored).not.toHaveBeenCalled()
  })

  it("purger demande confirmation", async () => {
    list()
    const purge = vi.spyOn(api, "fsTrashPurge").mockResolvedValue({ done: [], failed: [] })
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
    const element = screen.getByRole("button", { name: "Restaurer a.txt" }) as HTMLButtonElement
    expect(element.disabled).toBe(true)
    expect((screen.getByRole("button", { name: "Supprimer définitivement a.txt" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText("Vous n'avez pas le droit de modifier les fichiers de ce dossier")).toBeTruthy()
    fireEvent.click(element)
    expect(restore).not.toHaveBeenCalled()
  })
  it("une requête à la fois : pendant une restauration, restaurer et purger sont désactivés", async () => {
    list()
    let done!: (value: void) => void
    const restore = vi.spyOn(api, "fsTrashRestore").mockReturnValue(new Promise<void>((callback) => (done = callback)))
    const purge = vi.spyOn(api, "fsTrashPurge")
    mount()
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    fireEvent.click(screen.getByRole("button", { name: "Restaurer src/b.ts" }))
    const element = screen.getByRole("button", { name: "Supprimer définitivement src/b.ts" }) as HTMLButtonElement
    expect(element.disabled).toBe(true)
    fireEvent.click(element)
    expect(restore).toHaveBeenCalledTimes(1)
    expect(purge).not.toHaveBeenCalled()
    await act(async () => done())
    await waitFor(() => expect(element.disabled).toBe(false))
  })
  it("une requête à la fois : pendant une purge, restaurer et purger sont désactivés", async () => {
    list()
    let done!: (record: { done: string[]; failed: [] }) => void
    const purge = vi.spyOn(api, "fsTrashPurge").mockReturnValue(new Promise<{ done: string[]; failed: [] }>((callback) => (done = callback)))
    const restore = vi.spyOn(api, "fsTrashRestore")
    mount()
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Supprimer définitivement a.txt" }))
    fireEvent.click(await screen.findByRole("button", { name: "Supprimer définitivement" }))
    await waitFor(() => expect(purge).toHaveBeenCalledTimes(1))
    const element = screen.getByRole("button", { name: "Restaurer src/b.ts" }) as HTMLButtonElement
    await waitFor(() => expect(element.disabled).toBe(true))
    fireEvent.click(element)
    expect(restore).not.toHaveBeenCalled()
    await act(async () => done({ done: [], failed: [] }))
    await waitFor(() => expect(element.disabled).toBe(false))
  })
  it("fenêtre fermée pendant une restauration : l'arbre est quand même actualisé, une seule fois", async () => {
    list()
    let done!: (value: void) => void
    vi.spyOn(api, "fsTrashRestore").mockReturnValue(new Promise<void>((callback) => (done = callback)))
    const err = vi.spyOn(console, "error")
    const onRestored = vi.fn()
    const { rerender } = mount(true, onRestored)
    await screen.findByText("a.txt")
    fireEvent.click(screen.getByRole("button", { name: "Restaurer a.txt" }))
    rerender(<TrashDialog dir="/p" open={false} onClose={() => {}} canWrite onRestored={onRestored} />)
    await waitFor(() => expect(screen.queryByText("a.txt")).toBeNull())
    await act(async () => done())
    expect(onRestored).toHaveBeenCalledTimes(1)
    expect(err).not.toHaveBeenCalled()
  })
})
