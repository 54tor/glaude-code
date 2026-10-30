// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { Dialog, DialogContent, DialogTitle } from "./dialog"

afterEach(cleanup)

const open = (sheet?: boolean) =>
  render(
    <Dialog open>
      <DialogContent sheet={sheet} className="top-0 h-dvh">
        <DialogTitle>t</DialogTitle>
      </DialogContent>
    </Dialog>
  )

describe("DialogContent", () => {
  it("s'ouvre en bas d'écran sur téléphone par défaut", () => {
    open()
    expect(screen.getByRole("dialog").className).toContain("max-sm:top-auto")
  })
  // Sur iPhone, max-sm:top-auto et max-sm:max-h-[92dvh] l'emportaient sur top-0 h-dvh : bande vide en haut.
  it("laisse un dialogue plein écran garder sa hauteur sur téléphone", () => {
    open(false)
    const c = screen.getByRole("dialog").className
    expect(c).not.toMatch(/max-sm:(top-auto|max-h-|bottom-0)/)
    expect(c).toContain("top-0")
  })
})
