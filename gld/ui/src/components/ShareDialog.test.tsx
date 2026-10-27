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

  it.each([
    ["http:", true],
    ["https:", false],
  ])("avertissement non-HTTPS avec %s : %s", async (protocol, shown) => {
    vi.stubGlobal("location", { protocol, origin: `${protocol}//x` })
    await open()
    expect(screen.queryByText(/non HTTPS/) !== null).toBe(shown)
    vi.unstubAllGlobals()
  })

  it("avant la première réponse de shareInfo, aucune action n'est possible", async () => {
    let res!: (v: Info) => void
    vi.spyOn(api, "shareInfo").mockReturnValue(new Promise<Info>((r) => (res = r)))
    const c = vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "t" })
    render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
    await act(async () => {})
    expect(screen.queryByRole("button", { name: /Générer|Régénérer/ })).toBeNull()
    await act(async () => res({ shared: true, mode: "rw", connected: false }))
    await click(/Régénérer le lien/)
    expect(c).not.toHaveBeenCalled()
    expect(screen.getByText(/ancien lien cessera/)).toBeTruthy()
  })

  it("si le premier shareInfo échoue : erreur et réessai, pas de « non partagé » supposé", async () => {
    const s = vi.spyOn(api, "shareInfo").mockRejectedValueOnce(new Error("x"))
    render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
    await act(async () => {})
    expect(screen.queryByRole("button", { name: /Générer/ })).toBeNull()
    s.mockResolvedValue({ shared: false, connected: false })
    await click(/Réessayer/)
    expect(screen.getByRole("button", { name: /Générer et copier/ })).toBeTruthy()
  })

  it("un double clic sur Générer n'appelle shareCreate qu'une fois", async () => {
    vi.spyOn(api, "shareCreate").mockImplementation(() => new Promise((r) => setTimeout(() => r({ ...ok, token: "t" }), 10)))
    await open()
    const b = screen.getByRole("button", { name: /Générer et copier/ })
    fireEvent.click(b)
    fireEvent.click(b)
    await act(async () => void (await new Promise((r) => setTimeout(r, 30))))
    expect(api.shareCreate).toHaveBeenCalledTimes(1)
  })

  it("homes en échec : toast, pas de lien, rien de créé", async () => {
    vi.spyOn(api, "homes").mockRejectedValue(new Error("x"))
    const c = vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "t" })
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Générer et copier/)
    expect(err).toHaveBeenCalled()
    expect(c).not.toHaveBeenCalled()
    expect(screen.queryByRole("textbox")).toBeNull()
  })

  it("sans navigator.clipboard : lien affiché et toast de copie échouée", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
    vi.spyOn(api, "shareCreate").mockResolvedValue({ ...ok, token: "tok" })
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Générer et copier/)
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe(`${location.origin}/invite/main/tok`)
    expect(err).toHaveBeenCalledWith(expect.stringContaining("Copie impossible"))
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
