// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react"
import { afterEach, expect, test } from "vitest"
import "@/i18n"
import { Message } from "./Message"

afterEach(cleanup)

test("un chemin gld-web-images devient une image relue par son seul nom", () => {
  const m = { r: "user" as const, t: "voir\n@/home/bob/.claude/gld-web-images/abc-1.png\nmerci" }
  const { container } = render(<Message m={m} conv={{ dir: "d", id: "i" }} />)
  const img = container.querySelector("img")
  expect(img?.getAttribute("src")).toBe("/conv-upload?dir=d&id=i&name=abc-1.png")
  expect(container.textContent).not.toContain("gld-web-images")
  expect(container.textContent).toContain("merci")
})

test("l'invité garde le texte : la route lui est refusée", () => {
  const m = { r: "user" as const, t: "@/home/bob/.claude/gld-web-images/abc-1.png" }
  const { container } = render(<Message m={m} conv={{ dir: "d", id: "i", guest: true }} />)
  expect(container.querySelector("img")).toBeNull()
})
