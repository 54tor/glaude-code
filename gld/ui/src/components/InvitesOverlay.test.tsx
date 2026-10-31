// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"
import { api, type InviteInfo, type ShareInfo } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { resetShares } from "@/hooks/useShares"
import { InvitesOverlay } from "./InvitesOverlay"

const inv = (inviteInfo: Partial<InviteInfo> = {}): InviteInfo => ({ id: "i1", seq: 1, name: "", label: "Léa", mode: "rw", state: "connected", lastSeen: Date.now() / 1000, redeemed: true, queue: [], ...inviteInfo })
const sh = (invites: InviteInfo[], shareInfo: Partial<ShareInfo> = {}): ShareInfo => ({ id: "s1", dir: "/d", title: "Ma session", handle: "h", open: true, invites, ...shareInfo })
let write: ReturnType<typeof vi.fn>
let onOpen: ReturnType<typeof vi.fn<(id: string) => void>>

const mount = async (shares: ShareInfo[]) => {
  vi.spyOn(api, "shares").mockResolvedValue({ shares })
  render(<InvitesOverlay onClose={() => {}} onOpenSession={onOpen} />)
  await act(async () => {})
}
const click = async (name: RegExp | string, index = 0) => {
  fireEvent.click(screen.getAllByRole("button", { name })[index])
  await act(async () => {})
}

beforeEach(() => {
  resetShares()
  onOpen = vi.fn()
  write = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, "clipboard", { value: { writeText: write }, configurable: true })
  vi.spyOn(api, "homes").mockResolvedValue({ current: "", main: "/m", homes: [] })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("InvitesOverlay", () => {
  it("affiche titre de session et états connecté / absent / non utilisé", async () => {
    await mount([sh([inv(), inv({ id: "i2", label: "Bob", state: "away", lastSeen: Date.now() / 1000 - 7200 }), inv({ id: "i3", label: "Invité 3", state: "pending", lastSeen: 0, redeemed: false })])])
    expect(screen.getByText("Ma session")).toBeTruthy()
    expect(screen.getByText("connecté")).toBeTruthy()
    expect(screen.getByText(/absent \(vu/)).toBeTruthy()
    expect(screen.getByText("lien non utilisé")).toBeTruthy()
  })

  it("le bouton de session l'ouvre", async () => {
    await mount([sh([inv()])])
    await click(/Ouvrir la session/)
    expect(onOpen).toHaveBeenCalledWith("s1")
  })

  it("session fermée : la mention", async () => {
    await mount([sh([inv()], { open: false })])
    expect(screen.getByText(/session fermée/i)).toBeTruthy()
  })

  it("lecture en erreur : message et réessai", async () => {
    vi.spyOn(api, "shares").mockRejectedValueOnce(Object.assign(new Error("x"), { status: 500 }))
    render(<InvitesOverlay onClose={() => {}} />)
    await act(async () => {})
    expect(screen.getByText(/illisible/)).toBeTruthy()
    vi.spyOn(api, "shares").mockResolvedValue({ shares: [sh([inv()])] })
    await click(/Réessayer/)
    expect(screen.getByText("Léa")).toBeTruthy()
  })

  it("la bascule appelle inviteMode avec l'autre mode", async () => {
    const mock = vi.spyOn(api, "inviteMode").mockResolvedValue(undefined)
    await mount([sh([inv()])])
    fireEvent.click(screen.getByRole("switch", { name: /Écriture pour Léa/ }))
    await act(async () => {})
    expect(mock).toHaveBeenCalledWith("s1", "i1", "ro")
  })

  it("régénérer : confirmation, puis lien affiché et copié", async () => {
    const mock = vi.spyOn(api, "inviteRegen").mockResolvedValue({ token: "t2" })
    await mount([sh([inv()])])
    await click(/Régénérer le lien/)
    expect(mock).not.toHaveBeenCalled()
    await click(/^Régénérer$/)
    expect(mock).toHaveBeenCalledWith("s1", "i1")
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/main/t2`)
    expect(screen.getByDisplayValue(/invite\/main\/t2/)).toBeTruthy()
  })

  it("révoquer demande confirmation", async () => {
    const mock = vi.spyOn(api, "inviteRevoke").mockResolvedValue(undefined)
    await mount([sh([inv()])])
    await click(/Révoquer/)
    expect(mock).not.toHaveBeenCalled()
    await click(/^Révoquer$/)
    expect(mock).toHaveBeenCalledWith("s1", "i1")
  })

  it.each([["", undefined], ["Léa", "Léa"]])("inviter avec le nom %j", async (typed, sent) => {
    const mock = vi.spyOn(api, "inviteCreate").mockResolvedValue({ token: "tok", invite: { id: "i1", seq: 1, label: "Léa" } })
    await mount([sh([inv()])])
    if (typed) fireEvent.change(screen.getByLabelText(/Nom de l'invité/), { target: { value: typed } })
    await click(/Inviter quelqu'un/)
    expect(mock).toHaveBeenCalledWith("s1", "/d", "Ma session", sent)
    expect(write).toHaveBeenCalledWith(`${location.origin}/invite/main/tok`)
  })

  it("messages en attente : texte, état, retrait via queueRm", async () => {
    const rm = vi.spyOn(api, "queueRm").mockResolvedValue({ text: "" })
    await mount([sh([inv({ queue: [{ id: "q1", text: "bonjour", state: "queued", at: 1 }, { id: "q2", text: "en cours", state: "sending", at: 2 }] })])])
    expect(screen.getByText("bonjour")).toBeTruthy()
    const list = screen.getByRole("list", { name: "Léa" })
    expect(within(list).getAllByRole("button")).toHaveLength(1)
    await click(/Retirer de la file/)
    expect(rm).toHaveBeenCalledWith("s1", "q1")
  })

  it("erreur de création : toast", async () => {
    vi.spyOn(api, "inviteCreate").mockRejectedValue(new ApiError(500, "internal", "dix"))
    const err = vi.spyOn(toast, "error")
    await mount([sh([inv()])])
    await click(/Inviter quelqu'un/)
    expect(err).toHaveBeenCalled()
    expect(write).not.toHaveBeenCalled()
  })

  it("double clic sur Inviter : un seul appel", async () => {
    const mock = vi.spyOn(api, "inviteCreate").mockImplementation(() => new Promise((callback) => setTimeout(() => callback({ token: "t", invite: { id: "i1", seq: 1, label: "Léa" } }), 10)))
    await mount([sh([inv()])])
    const element = screen.getByRole("button", { name: /Inviter quelqu'un/ })
    fireEvent.click(element)
    fireEvent.click(element)
    await act(async () => void (await new Promise((callback) => setTimeout(callback, 30))))
    expect(mock).toHaveBeenCalledTimes(1)
  })

  it("le lien affiché porte le libellé de son invitation : le dernier régénéré seul", async () => {
    vi.spyOn(api, "inviteRegen").mockResolvedValueOnce({ token: "ta" }).mockResolvedValueOnce({ token: "tb" })
    await mount([sh([inv(), inv({ id: "i2", label: "Bob" })])])
    await click(/Régénérer le lien/, 0)
    await click(/^Régénérer$/)
    expect(screen.getByLabelText("Lien de Léa")).toBeTruthy()
    await click(/Régénérer le lien/, 1)
    await click(/^Régénérer$/)
    expect(screen.queryByLabelText("Lien de Léa")).toBeNull()
    expect((screen.getByLabelText("Lien de Bob") as HTMLInputElement).value).toContain("/tb")
  })

  it("révoquer l'invitation du lien affiché l'efface", async () => {
    vi.spyOn(api, "inviteRegen").mockResolvedValue({ token: "tb" })
    vi.spyOn(api, "inviteRevoke").mockResolvedValue(undefined)
    await mount([sh([inv()])])
    await click(/Régénérer le lien/)
    await click(/^Régénérer$/)
    expect(screen.getByLabelText("Lien de Léa")).toBeTruthy()
    await click(/Révoquer/)
    await click(/^Révoquer$/)
    expect(screen.queryByLabelText("Lien de Léa")).toBeNull()
  })

  it("à 10 invitations : la limite est dite, plus de champ d'invitation", async () => {
    await mount([sh(Array.from({ length: 10 }, (_, thirdIndex) => inv({ id: `i${thirdIndex}`, label: `N${thirdIndex}` })))])
    expect(screen.getByText(/Dix invitations au plus/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Inviter quelqu'un/ })).toBeNull()
  })
})
