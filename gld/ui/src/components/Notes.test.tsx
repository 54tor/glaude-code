// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { Notes } from "./Notes"

vi.mock("./actions", () => ({ useActions: () => ({ live: new Set() }) }))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Notes", () => {
  it("ne réécrit pas la note en quittant sans l'avoir modifiée", async () => {
    vi.spyOn(api, "notes").mockResolvedValue({ notes: { "p/x": JSON.stringify(["garder"]) } } as Awaited<ReturnType<typeof api.notes>>)
    const post = vi.spyOn(api, "post").mockResolvedValue({ ok: true, out: "" } as Awaited<ReturnType<typeof api.post>>)
    const { unmount } = render(<Notes noteKey="p/x" rows={[]} />)
    await screen.findByDisplayValue("garder")
    unmount()
    expect(post).not.toHaveBeenCalled()
  })
})
