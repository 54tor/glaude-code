// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { WorkspaceButton } from "./WorkspaceButton"

beforeEach(() => setLang("fr"))
afterEach(() => {
  cleanup()
  setAuth({ state: "loading", rights: undefined, home: undefined })
})

describe("WorkspaceButton", () => {
  it("visible en mode ouvert et avec fs_read, avec le lien vers l'espace", () => {
    setAuth({ state: "open" })
    render(<WorkspaceButton dir="/home/moi/p" />)
    expect(screen.getByRole("link", { name: /Espace de travail/ }).getAttribute("href")).toBe("/espace/%2Fhome%2Fmoi%2Fp")
    cleanup()
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } })
    render(<WorkspaceButton dir="/p" />)
    expect(screen.getByRole("link")).toBeTruthy()
  })
  it("masqué sans fs_read", () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["mem_read"] } })
    render(<WorkspaceButton dir="/p" />)
    expect(screen.queryByRole("link")).toBeNull()
  })
})
