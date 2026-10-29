// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { packOf } from "@/lib/memoryPack"
import { MemoryImport } from "./MemoryImport"
import type { List } from "./memoryLib"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const file = (name: string, key: string) => ({ key, name, kb: 0, mtime: 0, path: "/x", tokens: 0 })
const list: List = {
  global: [],
  projects: [{ dir: "-p-site", project: "/home/bob/site", missing: false, files: [file("a.md", "m/-p-site/a.md")] }],
  targets: [{ dir: "-p-site", project: "/home/bob/site" }],
}

describe("MemoryImport", () => {
  it("n'écrase pas un conflit laissé sur « ignorer », garde les deux sur demande", async () => {
    const save = vi.spyOn(api, "memorySave").mockResolvedValue({ message: "" })
    const entries = [
      { project: "app", name: "a.md", text: "A" },
      { project: "app", name: "b.md", text: "B" },
    ]
    render(<MemoryImport open list={list} homeDir="/home/bob" onClose={() => {}} onDone={() => {}} />)
    fireEvent.change(screen.getByLabelText(/fichier de mémoires|exported memories/i), { target: { files: [new File([packOf(entries)], "p.json")] } })
    const dest = await screen.findByLabelText(/Projet qui reçoit|Project receiving/)
    fireEvent.change(dest, { target: { value: "-p-site" } })

    // a.md existe déjà : par défaut ignoré, seul b.md part
    fireEvent.click(screen.getByRole("button", { name: /Import(er)? 1 /i }))
    await act(async () => {})
    expect(save.mock.calls).toEqual([["m/-p-site/b.md", "B", true]])
  })

  it("garde les deux : le fichier importé prend un nouveau nom", async () => {
    const save = vi.spyOn(api, "memorySave").mockResolvedValue({ message: "" })
    render(<MemoryImport open list={list} homeDir="/home/bob" onClose={() => {}} onDone={() => {}} />)
    fireEvent.change(screen.getByLabelText(/fichier de mémoires|exported memories/i), { target: { files: [new File([packOf([{ project: "app", name: "a.md", text: "A" }])], "p.json")] } })
    fireEvent.change(await screen.findByLabelText(/Projet qui reçoit|Project receiving/), { target: { value: "-p-site" } })
    fireEvent.change(screen.getByLabelText(/Si a.md existe|If a.md already/), { target: { value: "both" } })
    fireEvent.click(screen.getByRole("button", { name: /Import(er)? 1 /i }))
    await act(async () => {})
    expect(save.mock.calls).toEqual([["m/-p-site/a-2.md", "A", true]])
  })
})
