// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type CfgOverview } from "@/lib/api"
import { Plugins } from "./Plugins"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Plugins", () => {
  it("affiche le panneau et un lien d'export vers la sauvegarde des plugins", async () => {
    vi.spyOn(api, "config").mockResolvedValue({ projects: [] } as unknown as CfgOverview)
    vi.spyOn(api, "marketplaces").mockResolvedValue({ marketplaces: [] })
    vi.spyOn(api, "plugins").mockResolvedValue({ installed: [], available: [] })
    await act(async () => void render(<Plugins home="/home/bob" />))
    expect(screen.getByRole("textbox")).toBeTruthy()
    const link = screen.getByRole("link", { name: /sauvegarde|backup|export/i })
    expect(link.getAttribute("href")).toBe("/sauvegarde?cats=plugins")
  })
})
