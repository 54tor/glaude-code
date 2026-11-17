// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, expect, test, vi } from "vitest"
import "@/i18n"
import { Message } from "./Message"

afterEach(cleanup)

test("un chemin gld-web-images devient une image relue par son seul nom", () => {
  const record = { r: "user" as const, t: "voir\n@/home/bob/.claude/gld-web-images/abc-1.png\nmerci" }
  const { container } = render(<Message m={record} conv={{ dir: "d", id: "i" }} />)
  const img = container.querySelector("img")
  expect(img?.getAttribute("src")).toBe("/v1/sessions/i/conversation/uploads/abc-1.png?dir=d")
  expect(container.textContent).not.toContain("gld-web-images")
  expect(container.textContent).toContain("merci")
})

test("l'invité garde le texte : la route lui est refusée", () => {
  const record = { r: "user" as const, t: "@/home/bob/.claude/gld-web-images/abc-1.png" }
  const { container } = render(<Message m={record} conv={{ dir: "d", id: "i", guest: true }} />)
  expect(container.querySelector("img")).toBeNull()
})

test("un fichier joint non image devient une pastille, sans chemin", () => {
  const record = { r: "user" as const, t: "@~/.claude/gld-web-images/20261117-231631-eff8f81e.md\n@~/.claude/gld-web-images/20261117-231631-52782f61.md" }
  const { container } = render(<Message m={record} conv={{ dir: "d", id: "i" }} />)
  expect(container.querySelector("img")).toBeNull()
  expect(container.textContent).not.toContain("gld-web-images")
  expect(container.textContent?.match(/MD/g)).toHaveLength(2)
})

test("un .md joint se déplie et affiche son contenu relu par son seul nom", async () => {
  vi.spyOn(await import("@/lib/auth"), "authFetch").mockResolvedValue(new Response("# Titre joint"))
  const record = { r: "user" as const, t: "@~/.claude/gld-web-images/20261117-231631-eff8f81e.md" }
  const { container } = render(<Message m={record} conv={{ dir: "d", id: "i" }} />)
  fireEvent.click(container.querySelector("button")!)
  await waitFor(() => expect(container.textContent).toContain("Titre joint"))
})
