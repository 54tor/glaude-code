// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { OrphanFiles } from "./OrphanFiles"

const ORPHAN = { name: "a.png", bytes: 2048, at: 1, trashed: false }
const TRASHED = { name: "b.png", bytes: 1024, at: 1, trashed: true }

const mount = async () => {
  render(<OrphanFiles open onClose={vi.fn()} />)
  await act(async () => {})
}

describe("OrphanFiles", () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("liste les fichiers et met le fichier cliqué à la corbeille", async () => {
    vi.spyOn(api, "orphanFiles").mockResolvedValue({
      files: [ORPHAN, TRASHED],
      days: 30,
    })
    const trash = vi
      .spyOn(api, "orphanFilesTrash")
      .mockResolvedValue({ message: "ok" })
    await mount()
    expect(screen.getByText("a.png")).toBeTruthy()
    expect(screen.getByText("b.png")).toBeTruthy()
    await act(async () => {
      fireEvent.click(screen.getByText(/Mettre à la corbeille|Move to trash/))
    })
    expect(trash).toHaveBeenCalledWith(["a.png"])
  })

  it("restaure un fichier de la corbeille", async () => {
    vi.spyOn(api, "orphanFiles").mockResolvedValue({
      files: [TRASHED],
      days: 30,
    })
    const restore = vi
      .spyOn(api, "orphanFilesRestore")
      .mockResolvedValue({ message: "ok" })
    await mount()
    await act(async () => {
      fireEvent.click(screen.getByText(/Restaurer|Restore/))
    })
    expect(restore).toHaveBeenCalledWith(["b.png"])
  })

  it("n'affiche aucun bouton « Vider » sans fichier en corbeille", async () => {
    vi.spyOn(api, "orphanFiles").mockResolvedValue({
      files: [ORPHAN],
      days: 30,
    })
    await mount()
    expect(screen.queryByText(/Vider la corbeille|Empty the trash/)).toBeNull()
  })
})
