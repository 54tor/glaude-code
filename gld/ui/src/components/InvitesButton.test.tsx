// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type InviteInfo, type ShareInfo } from "@/lib/api"
import { resetShares } from "@/hooks/useShares"
import { InvitesButton } from "./InvitesButton"

const inv = (o: Partial<InviteInfo> = {}): InviteInfo => ({ id: "i1", seq: 1, name: "", label: "Léa", mode: "rw", state: "connected", lastSeen: 0, redeemed: true, queue: [], ...o })
const sh = (id: string, invites: InviteInfo[]): ShareInfo => ({ id, dir: "/d", title: id, handle: "h", open: true, invites })
const q = (id: string) => ({ id, text: "x", state: "queued" as const, at: 1 })
const mount = async (shares: ShareInfo[]) => {
  vi.spyOn(api, "shares").mockResolvedValue({ shares })
  render(<InvitesButton />)
  await act(async () => {})
}

beforeEach(() => resetShares())
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("InvitesButton", () => {
  it("masqué sans partage", async () => {
    await mount([])
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("pastille = invités connectés + messages en attente de tous les partages", async () => {
    await mount([sh("a", [inv({ queue: [q("1"), q("2")] }), inv({ id: "i2", state: "away" })]), sh("b", [inv({ id: "i3", queue: [q("3")] })])])
    const b = screen.getByRole("button", { name: /2 connecté.*3 message/ })
    expect(b.textContent).toContain("5")
  })

  it("aucune pastille quand le compte est nul", async () => {
    await mount([sh("a", [inv({ state: "pending" })])])
    expect(screen.getByRole("button", { name: /0 connecté/ }).textContent).toBe("")
  })

  it("ouvre l'overlay", async () => {
    await mount([sh("a", [inv()])])
    fireEvent.click(screen.getByRole("button", { name: /Invitations/ }))
    await act(async () => {})
    expect(screen.getByRole("dialog")).toBeTruthy()
  })
})
