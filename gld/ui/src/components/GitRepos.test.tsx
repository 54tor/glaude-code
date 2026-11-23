// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import type { GitState } from "@/lib/api"
import { TooltipProvider } from "@/components/ui/tooltip"
import { GitRepos } from "./GitRepos"

afterEach(cleanup)

describe("GitRepos", () => {
  it("la liste montre le nom de chaque dépôt, rangée par rangée", () => {
    const dirs = ["/home/bob/alpha", "/home/bob/beta"]
    render(
      <TooltipProvider>
        <GitRepos dirs={dirs} status={{} as Record<string, GitState>} />
      </TooltipProvider>
    )
    fireEvent.click(screen.getByRole("button", { name: /dépôt/i }))
    const items = screen.getAllByRole("listitem")
    expect(items.map((item) => item.textContent)).toEqual(
      expect.arrayContaining([
        expect.stringContaining("alpha"),
        expect.stringContaining("beta"),
      ])
    )
    expect(items[0].className).toContain("flex-wrap")
  })
})
