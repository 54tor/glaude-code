// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest"
import { setAuth } from "@/lib/auth"
import { clearUserData, signedOut } from "@/lib/auth"
import { readDrafts, writeDrafts } from "./drafts"
import type { Tab } from "./ideState"

const tab = { path: "/home/bob/a.txt", content: "secret", etag: "e1", readOnly: false, saved: "x" } as unknown as Tab

afterEach(() => {
  sessionStorage.clear()
  localStorage.clear()
  setAuth({ state: "loading", owner: undefined })
})

describe("ide drafts cleanup", () => {
  it("clearUserData removes ide drafts and ide keys", () => {
    sessionStorage.setItem("gld.ide./d.drafts", "{}")
    localStorage.setItem("gld.ide./d.tabs", "[]")
    localStorage.setItem("other", "1")
    clearUserData()
    expect(sessionStorage.getItem("gld.ide./d.drafts")).toBeNull()
    expect(localStorage.getItem("gld.ide./d.tabs")).toBeNull()
    expect(localStorage.getItem("other")).toBe("1")
  })
  it("does not rewrite drafts after sign-out", () => {
    setAuth({ state: "ok", owner: "alice" })
    writeDrafts("/d", [tab])
    expect(Object.keys(readDrafts("/d"))).toHaveLength(1)
    signedOut()
    writeDrafts("/d", [tab])
    expect(readDrafts("/d")).toEqual({})
  })
})
