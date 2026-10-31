// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { Notes } from "./Notes"

const newSession = vi.hoisted(() => vi.fn())
vi.mock("./actions", () => ({ useActions: () => ({ live: new Set(), newSession }) }))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Notes", () => {
  it("ne réécrit pas la note en quittant sans l'avoir modifiée", async () => {
    vi.spyOn(api, "notes").mockResolvedValue({ notes: { "p/x": JSON.stringify(["garder"]) } } as Awaited<ReturnType<typeof api.notes>>)
    const save = vi.spyOn(api, "notesSave").mockResolvedValue({ message: "" })
    const { unmount } = render(<Notes noteKey="p/x" scope={[]} rows={[]} />)
    await screen.findByDisplayValue("garder")
    unmount()
    expect(save).not.toHaveBeenCalled()
  })

  it("remet une entrée envoyée dans la note et la retire de l'historique", async () => {
    vi.spyOn(api, "notes").mockResolvedValue({ notes: { "p/x": JSON.stringify(["garder"]) } } as Awaited<ReturnType<typeof api.notes>>)
    vi.spyOn(api, "notesSent").mockResolvedValue({ sent: [{ id: "a", at: 0, text: "- perdue", to: "s1" }] })
    vi.spyOn(api, "notesSave").mockResolvedValue({ message: "" })
    const rm = vi.spyOn(api, "notesSentRemove").mockResolvedValue({ message: "" })
    render(<Notes noteKey="p/x" scope={[]} rows={[]} />)
    await screen.findByDisplayValue("garder")
    fireEvent.click(await screen.findByRole("button", { name: /Envoyées|Sent/ }))
    fireEvent.click(await screen.findByRole("button", { name: /Remettre dans la note|Put back in the note/ }))
    await screen.findByDisplayValue("perdue")
    await waitFor(() => expect(rm).toHaveBeenCalledWith("p/x", "a"))
  })

  it("l'envoi précède la liste d'un cadrage, l'historique garde la liste seule", async () => {
    vi.spyOn(api, "notes").mockResolvedValue({ notes: { "p/x": JSON.stringify(["un", "deux"]) } } as Awaited<ReturnType<typeof api.notes>>)
    vi.spyOn(api, "notesSave").mockResolvedValue({ message: "" })
    const add = vi.spyOn(api, "notesSentAdd").mockResolvedValue({ entry: { id: "a", at: 0, text: "", to: "" } })
    render(<Notes noteKey="p/x" folder="/d" scope={["/d"]} rows={[]} />)
    await screen.findByDisplayValue("un")
    fireEvent.click(screen.getByRole("button", { name: /Nouvelle session avec la note/ }))
    await waitFor(() => expect(newSession).toHaveBeenCalled())
    const [folder, message] = newSession.mock.calls[0]
    expect(folder).toBe("/d")
    expect(message).toMatch(/^Voici une liste de points à aborder dans l'ordre, prépare le travail/)
    expect(message).toMatch(/\n\n- un\n- deux$/)
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ text: "- un\n- deux" }))
  })
})
