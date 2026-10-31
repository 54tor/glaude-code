// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type PluginAvail } from "@/lib/api"
import { PluginsCatalog } from "./PluginsDialog"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const avail = (name: string): PluginAvail => ({ id: name + "@m", name, description: "", marketplace: "m", installs: 0, installed: false })

describe("PluginsCatalog", () => {
  it("une réponse lente d'une recherche précédente n'écrase pas la dernière", async () => {
    vi.useFakeTimers()
    vi.spyOn(api, "marketplaces").mockResolvedValue({ marketplaces: [] })
    const pending: Record<string, (record: Awaited<ReturnType<typeof api.plugins>>) => void> = {}
    vi.spyOn(api, "plugins").mockImplementation((query = "") => new Promise((res) => void (pending[query] = res)))
    render(<PluginsCatalog projects={[]} home="" scope="user" onChange={() => {}} />)
    await act(async () => void vi.advanceTimersByTime(0))
    const box = screen.getByRole("textbox", { name: /rechercher un plugin/i })
    fireEvent.change(box, { target: { value: "a" } })
    await act(async () => void vi.advanceTimersByTime(300))
    fireEvent.change(box, { target: { value: "ab" } })
    await act(async () => void vi.advanceTimersByTime(300))
    await act(async () => pending["ab"]({ installed: [], available: [avail("bon")] }))
    await act(async () => pending["a"]({ installed: [], available: [avail("perime")] }))
    expect(screen.queryByText("perime")).toBeNull()
    expect(screen.getByText("bon")).toBeTruthy()
  })

  it("les plugins déjà installés quittent la liste des disponibles", async () => {
    vi.spyOn(api, "marketplaces").mockResolvedValue({ marketplaces: [] })
    vi.spyOn(api, "plugins").mockResolvedValue({ installed: [], available: [avail("garde"), avail("pris")] })
    const inst = [{ id: "pris@m", version: "1", scope: "user", enabled: true }]
    await act(async () => void render(<PluginsCatalog projects={[]} home="" scope="user" installed={inst} onChange={() => {}} />))
    expect(await screen.findByText("garde")).toBeTruthy()
    expect(screen.queryByText("pris")).toBeNull()
  })
})
