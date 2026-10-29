// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"
import { api, type InviteInfo, type ShareInfo } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { resetShares } from "@/hooks/useShares"
import { ShareDialog } from "./ShareDialog"

const homes = (current: string) => ({ current, main: "/m", homes: [] })
let write: ReturnType<typeof vi.fn>
const inv = (o: Partial<InviteInfo> = {}): InviteInfo => ({ id: "i1", seq: 1, name: "", label: "Invité 1", mode: "rw", state: "pending", lastSeen: 0, redeemed: false, queue: [], ...o })
const sh = (invites: InviteInfo[]): ShareInfo => ({ id: "s1", dir: "/d", title: "T", handle: "h", open: true, invites })

beforeEach(() => {
  resetShares()
  write = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true })
  vi.spyOn(api, "homes").mockResolvedValue(homes(""))
  vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const open = async (shares?: ShareInfo[], title?: string) => {
  if (shares) vi.spyOn(api, "shares").mockResolvedValue({ shares })
  render(<ShareDialog id="s1" dir="/d" title={title} onClose={() => {}} />)
  await act(async () => {})
}
const click = async (name: RegExp) => {
  fireEvent.click(screen.getByRole("button", { name }))
  await act(async () => {})
}

describe("ShareDialog", () => {
  it("inviter : titre de la session, nom, lien copié", async () => {
    const c = vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "tok", invite: { id: "i1", seq: 1, label: "Léa" } })
    await open(undefined, "Ma session")
    fireEvent.change(screen.getByLabelText(/Nom de l'invité/), { target: { value: " Léa " } })
    await click(/Inviter quelqu'un/)
    expect(c).toHaveBeenCalledWith("s1", "/d", "Ma session", "Léa")
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/main/tok`)
    expect((screen.getByDisplayValue(/invite\/main\/tok/) as HTMLInputElement).value).toBe(`${location.origin}/invite/main/tok`)
  })

  it.each([["x".repeat(130), "x".repeat(120)], ["(sans titre)", undefined], ["", undefined]])("titre %#", async (given, sent) => {
    const c = vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "tok", invite: { id: "i1", seq: 1, label: "Léa" } })
    await open(undefined, given)
    await click(/Inviter quelqu'un/)
    expect(c).toHaveBeenCalledWith("s1", "/d", sent, undefined)
  })

  it("utilise le nom du home courant, échappé", async () => {
    vi.spyOn(api, "homes").mockResolvedValue(homes("a b"))
    vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "tok", invite: { id: "i1", seq: 1, label: "Léa" } })
    await open()
    await click(/Inviter quelqu'un/)
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/a%20b/tok`)
  })

  it("mettre fin demande confirmation puis appelle shareEnd", async () => {
    vi.spyOn(api, "shareEnd").mockResolvedValue(undefined)
    await open([sh([inv()])])
    await click(/Mettre fin au partage/)
    expect(api.shareEnd).not.toHaveBeenCalled()
    await click(/^Mettre fin$/)
    expect(api.shareEnd).toHaveBeenCalledWith("s1")
  })

  it("une erreur à la fin dit que le partage est révoqué", async () => {
    vi.spyOn(api, "shareEnd").mockRejectedValue(new ApiError(500, "internal", "boom"))
    const err = vi.spyOn(toast, "error")
    await open([sh([inv()])])
    await click(/Mettre fin au partage/)
    await click(/^Mettre fin$/)
    expect(err).toHaveBeenCalledWith(expect.stringContaining("Partage révoqué"))
  })

  it("erreur de création : toast, pas de copie", async () => {
    vi.spyOn(api, "inviteCreate").mockRejectedValue(new ApiError(500, "internal", "non"))
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Inviter quelqu'un/)
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

  it("avant la première réponse, aucune action ; si elle échoue : erreur et réessai, pas de « aucun invité » supposé", async () => {
    const s = vi.spyOn(api, "shares").mockRejectedValueOnce(Object.assign(new Error("x"), { status: 500 }))
    render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
    await act(async () => {})
    expect(screen.queryByRole("button", { name: /Inviter/ })).toBeNull()
    s.mockResolvedValue({ shares: [] })
    await click(/Réessayer/)
    expect(screen.getByRole("button", { name: /Inviter quelqu'un/ })).toBeTruthy()
  })

  it("lecture en cours : pas d'action", async () => {
    vi.spyOn(api, "shares").mockReturnValue(new Promise(() => {}))
    render(<ShareDialog id="s1" dir="/d" onClose={() => {}} />)
    await act(async () => {})
    expect(screen.queryByRole("button", { name: /Inviter/ })).toBeNull()
    expect(screen.getByText(/Lecture de l'état/)).toBeTruthy()
  })

  it("un double clic sur Inviter n'appelle inviteCreate qu'une fois", async () => {
    const c = vi.spyOn(api, "inviteCreate").mockImplementation(() => new Promise((r) => setTimeout(() => r({ token: "t", invite: { id: "i1", seq: 1, label: "Léa" } }), 10)))
    await open()
    const b = screen.getByRole("button", { name: /Inviter quelqu'un/ })
    fireEvent.click(b)
    fireEvent.click(b)
    await act(async () => void (await new Promise((r) => setTimeout(r, 30))))
    expect(c).toHaveBeenCalledTimes(1)
  })

  it("homes en échec : toast, rien de créé", async () => {
    vi.spyOn(api, "homes").mockRejectedValue(new Error("x"))
    const c = vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "t", invite: { id: "i1", seq: 1, label: "Léa" } })
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Inviter quelqu'un/)
    expect(err).toHaveBeenCalled()
    expect(c).not.toHaveBeenCalled()
  })

  it("sans navigator.clipboard : lien affiché et toast de copie échouée", async () => {
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
    vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "tok", invite: { id: "i1", seq: 1, label: "Léa" } })
    const err = vi.spyOn(toast, "error")
    await open()
    await click(/Inviter quelqu'un/)
    expect((screen.getByDisplayValue(/\/invite\/main\/tok/) as HTMLInputElement).value).toBe(`${location.origin}/invite/main/tok`)
    expect(err).toHaveBeenCalledWith(expect.stringContaining("Copie impossible"))
  })

  it("ne montre que la session demandée", async () => {
    await open([sh([inv({ label: "Léa" })]), { ...sh([inv({ id: "z", label: "Zoé" })]), id: "s2" }])
    expect(screen.getByText("Léa")).toBeTruthy()
    expect(screen.queryByText("Zoé")).toBeNull()
  })
})
