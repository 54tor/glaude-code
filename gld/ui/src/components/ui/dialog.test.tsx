// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./dialog"

afterEach(cleanup)

it("le tiroir mobile s'arrête au-dessus de la barre d'état", () => {
  render(
    <Dialog open>
      <DialogContent>
        <DialogTitle>t</DialogTitle>
        <DialogDescription>d</DialogDescription>
      </DialogContent>
    </Dialog>
  )
  const classes = screen.getByRole("dialog").className
  expect(classes).toContain("max-sm:bottom-[var(--bar-h)]")
  expect(classes).not.toContain("max-sm:bottom-0")
})
