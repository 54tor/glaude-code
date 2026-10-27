// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { ShareDialog } from "./ShareDialog"

type Info = Awaited<ReturnType<typeof api.shareInfo>>
const homes = (current: string) => ({ current, main: "/m", homes: [] })
const ok = { ok: true, out: "" }
let write: ReturnType<typeof vi.fn>

beforeEach(() => {
  write = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true })
  vi.spyOn(api, "homes").mockResolvedValue(homes(""))
  vi.spyOn(api, "shareInfo").mockResolvedValue({ shared: false, connected: false })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const open = async (info?: Info) => {
  if (info) vi.spyOn(api, "shareInfo").mockResolvedValue(info)
  render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
  await act(async () => {})
}
const click = async (name: RegExp) => {
  fireEvent.click(screen.getByRole("button", { name }))
  await act(async () => {})
}

describe("ShareDialog", () => {
  it("génère le lien et le copie", async () => {
    vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "tok" })
    await open()
    await click(/Générer et copier/)
    expect(api.shareCreate).toHaveBeenCalledWith("s1", "/d")
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/main/tok`)
  })

  it("utilise le nom du home courant, échappé", async () => {
    vi.spyOn(api, "homes").mockResolvedValue(homes("a b"))
    vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "tok" })
    await open()
    await click(/Générer et copier/)
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/a%20b/tok`)
  })

  it("régénérer demande confirmation et prévient que l'ancien lien meurt", async () => {
    vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "t2" })
    await open({ shared: true, mode: "rw", connected: false })
    await click(/Régénérer le lien/)
    expect(api.shareCreate).not.toHaveBeenCalled()
    expect(screen.getByText(/ancien lien cessera/)).toBeTruthy()
    await click(/^Régénérer$/)
    expect(api.shareCreate).toHaveBeenCalledTimes(1)
  })

  it("bascule le mode en ro puis rw", async () => {
    await open({ shared: true, mode: "rw", connected: false })
    const m = vi.spyOn(api, "shareMode").mockImplementation(async (_id, mode) => {
      vi.spyOn(api, "shareInfo").mockResolvedValue({ shared: true, mode, connected: false })
      return ok
    })
    await click(/Passer en lecture seule/)
    expect(m).toHaveBeenLastCalledWith("s1", "ro")
    await click(/Redonner l'écriture/)
    expect(m).toHaveBeenLastCalledWith("s1", "rw")
  })

  it("mettre fin demande confirmation puis appelle shareEnd", async () => {
    vi.spyOn(api, "shareEnd").mockResolvedValue(ok)
    await open({ shared: true, mode: "rw", connected: false })
    await click(/Mettre fin au partage/)
    expect(api.shareEnd).not.toHaveBeenCalled()
    await click(/^Mettre fin$/)
    expect(api.shareEnd).toHaveBeenCalledWith("s1")
  })

  it("une erreur 500 à la fin dit que le partage est révoqué", async () => {
    vi.spyOn(api, "shareEnd").mockResolvedValue({ ok: false, out: "boom" })
    const err = vi.spyOn(toast, "error")
    await open({ shared: true, mode: "rw", connected: false })
    await click(/Mettre fin au partage/)
    await click(/^Mettre fin$/)
    expect(err).toHaveBeenCalledWith(expect.stringContaining("Partage révoqué"))
  })

  it("erreur de création : toast", async () => {
    vi.spyOn(api, "shareCreate").mockResolvedValue({ ok: false, out: "non" })
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Générer et copier/)
    expect(err).toHaveBeenCalled()
    expect(write).not.toHaveBeenCalled()
  })

  it("avertit hors HTTPS seulement", async () => {
    await open()
    expect(screen.queryByText(/non HTTPS/) !== null).toBe(location.protocol !== "https:")
    cleanup()
    vi.stubGlobal("location", { ...location, protocol: "https:", origin: "https://x" })
    await open()
    expect(screen.queryByText(/non HTTPS/)).toBeNull()
    vi.unstubAllGlobals()
  })

  it("l'indicateur d'invité suit shareInfo (sondage 5 s)", async () => {
    vi.useFakeTimers()
    const s = vi.spyOn(api, "shareInfo").mockResolvedValue({ shared: true, mode: "rw", connected: false })
    render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
    await act(async () => {})
    expect(screen.getByText("Aucun invité connecté")).toBeTruthy()
    s.mockResolvedValue({ shared: true, mode: "rw", connected: true })
    await act(async () => void vi.advanceTimersByTime(5000))
    expect(screen.getByText("Invité connecté")).toBeTruthy()
  })
})
