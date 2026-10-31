// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type BackupPlan } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { Backup } from "./Backup"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState(null, "", "/")
})

const item = (id: string, kind: string, cat: string, extra: object = {}) => ({ id, kind, label: id.split(":")[1], status: "new" as const, merge: false, cat, from: "", to: "", ...extra })
const plan: BackupPlan = {
  id: "s1",
  manifest: { format: 1, gld: "x", date: "d", cats: ["skills", "mcp", "memory"] },
  items: [item("skill:a", "skill", "skills"), item("mcp:srv", "mcp", "mcp", { risk: "exec", from: "KEY" }), item("projmem:/home/bob/p", "projmem", "memory")],
  targets: [{ dir: "/home/bob/p", project: "/home/bob/p" }, { dir: "/home/bob/q", project: "/home/bob/q" }],
  skipped: [],
}

async function staged() {
  vi.spyOn(api, "backupStage").mockResolvedValue(plan)
  const { container } = render(<Backup home="/home/bob" />)
  const input = container.querySelector('input[type="file"]') as HTMLInputElement
  await act(async () => void fireEvent.change(input, { target: { files: [new File(["x"], "b.zip")] } }))
  await screen.findByRole("button", { name: /appliquer/i })
}

describe("Backup", () => {
  it("?cats= précoche seulement la catégorie nommée", () => {
    window.history.replaceState(null, "", "/?cats=plugins")
    render(<Backup home="" />)
    const boxes = screen.getAllByRole("checkbox")
    expect(boxes.filter((element) => element.getAttribute("aria-checked") === "true")).toHaveLength(1)
    expect(screen.getByRole("checkbox", { name: /plugins/i }).getAttribute("aria-checked")).toBe("true")
  })

  it("le téléchargement est désactivé sans catégorie", () => {
    render(<Backup home="" />)
    expect(screen.queryByRole("link", { name: /télécharger/i })).toBeNull()
    expect(screen.getByRole("button", { name: /télécharger/i }).hasAttribute("disabled")).toBe(true)
  })

  it("les éléments importés sont groupés par catégorie", async () => {
    await staged()
    expect(screen.getAllByRole("heading", { level: 3 })).toHaveLength(3)
  })

  it("Appliquer n'envoie que les éléments cochés et les cibles choisies", async () => {
    await staged()
    const apply = vi.spyOn(api, "backupApply").mockResolvedValue({ results: [{ id: "skill:a", status: "ok" }] })
    fireEvent.click(screen.getByRole("checkbox", { name: /copier.*a$/i }))
    fireEvent.click(screen.getByRole("button", { name: /appliquer/i }))
    await waitFor(() => expect(apply).toHaveBeenCalled())
    expect(apply).toHaveBeenCalledWith("s1", [{ id: "skill:a", mode: "replace" }], { "/home/bob/p": "/home/bob/p" })
  })

  it("un résultat en échec affiche son message", async () => {
    await staged()
    vi.spyOn(api, "backupApply").mockResolvedValue({ results: [{ id: "skill:a", status: "failed", message: "boom" }] })
    fireEvent.click(screen.getByRole("checkbox", { name: /copier.*a$/i }))
    fireEvent.click(screen.getByRole("button", { name: /appliquer/i }))
    expect(await screen.findByText(/boom/)).toBeTruthy()
  })

  it("Annuler abandonne l'étape", async () => {
    await staged()
    const drop = vi.spyOn(api, "backupDrop").mockResolvedValue()
    fireEvent.click(screen.getByRole("button", { name: /annuler/i }))
    await waitFor(() => expect(drop).toHaveBeenCalledWith("s1"))
  })

  it("une erreur d'application est annoncée", async () => {
    await staged()
    vi.spyOn(api, "backupApply").mockRejectedValue(new ApiError(500, "internal", "boom"))
    fireEvent.click(screen.getByRole("checkbox", { name: /copier.*a$/i }))
    fireEvent.click(screen.getByRole("button", { name: /appliquer/i }))
    expect((await screen.findByRole("alert")).textContent).toContain("boom")
  })

  it("une étape perdue (404) ramène au choix du fichier", async () => {
    await staged()
    vi.spyOn(api, "backupApply").mockRejectedValue(new ApiError(404, "not_found", "gone"))
    fireEvent.click(screen.getByRole("checkbox", { name: /copier.*a$/i }))
    fireEvent.click(screen.getByRole("button", { name: /appliquer/i }))
    expect((await screen.findByRole("alert")).textContent).toContain("gone")
    expect(screen.getByRole("button", { name: /choisir une archive/i })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /appliquer/i })).toBeNull()
  })
})
