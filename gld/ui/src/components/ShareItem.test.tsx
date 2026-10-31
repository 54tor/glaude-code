// @vitest-environment jsdom
import type { ReactNode } from "react"
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type ShareInfo } from "@/lib/api"
import { resetShares } from "@/hooks/useShares"
import { ActionsCtx, type Row } from "./actions"
import { ShareItem, type Kit } from "./SessionMenu"

const M = { Item: ({ children }: { children: ReactNode }) => <button>{children}</button> } as unknown as Kit
const row = { s: { id: "s1" } } as Row
const share = (id: string, count: number): ShareInfo => ({ id, dir: "/d", title: "", handle: "h", open: true, invites: Array.from({ length: count }, (_, index) => ({ id: `i${index}`, seq: index, name: "", label: "x", mode: "rw", state: "pending", lastSeen: 0, redeemed: false, queue: [] })) })

const mount = async (shares: ShareInfo[]) => {
  const spy = vi.spyOn(api, "shares").mockResolvedValue({ shares })
  render(
    <ActionsCtx.Provider value={{} as never}>
      <ShareItem M={M} row={row} />
    </ActionsCtx.Provider>
  )
  await act(async () => {})
  return spy
}

beforeEach(() => resetShares())
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("ShareItem", () => {
  it("session avec invitations : Partage…", async () => {
    await mount([share("s1", 1)])
    expect(screen.getByRole("button").textContent).toMatch(/Partage…|Sharing…/)
  })
  it.each([[[]], [[share("s1", 0)]], [[share("s2", 2)]]])("sinon : Partager…", async (shares) => {
    await mount(shares)
    expect(screen.getByRole("button").textContent).toMatch(/Partager…|Share…/)
  })
})
