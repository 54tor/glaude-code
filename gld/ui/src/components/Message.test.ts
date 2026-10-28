import { expect, test } from "vitest"
import { hideHome } from "./Message"

test("hideHome masque le dossier personnel", () => {
  expect(hideHome("voir\n@/home/bob/.claude/x.png")).toBe("voir\n@~/.claude/x.png")
  expect(hideHome("/Users/bob/a et C:\\Users\\Bob\\b")).toBe("~/a et ~\\b")
  expect(hideHome("/var/home/x /homework/y")).toBe("/var/home/x /homework/y")
})
