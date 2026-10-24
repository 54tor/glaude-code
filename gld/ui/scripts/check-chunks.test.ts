// @vitest-environment node
import { spawnSync } from "node:child_process"
import { expect, it } from "vitest"

it("le garde des chunks passe son auto-test (cycle, taille, entrée creuse, import dynamique, import collé)", () => {
  const r = spawnSync(process.execPath, ["scripts/check-chunks.mjs", "--self-test"], { encoding: "utf8" })
  expect(r.stderr + r.stdout).toContain("auto-test ok")
  expect(r.status).toBe(0)
})
