// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { branchOf, StatusBar, useEditorStatus } from "./StatusBar"
import type { EditorHandle, EditorStatus } from "./commands"
import type { GitState } from "@/lib/api"

const st = (o: Partial<EditorStatus> = {}): EditorStatus => ({ line: 3, col: 7, selected: 0, eol: "LF", language: "typescript", indent: "spaces:2", readOnly: false, ...o })
const term = (o: { open?: boolean; reason?: string } = {}) => ({ open: false, reason: "", onToggle: vi.fn(), ...o })
const ed = (s: EditorStatus | null) => s && ({ subscribe: (l: (x: EditorStatus) => void) => (l(s), () => {}) } as unknown as EditorHandle)
const bar = (o: Partial<Parameters<typeof StatusBar>[0]> = {}) =>
  render(<StatusBar editor={ed(st())} watch={{ connected: true, state: "open", retryIn: 0 }} index="ready" branch="main" dirty={0} term={term()} {...o} />)

beforeEach(() => setLang("fr"))
afterEach(cleanup)

describe("StatusBar", () => {
  it("position, fins de ligne, langage, indentation, encodage, branche", () => {
    bar()
    for (const s of ["Ligne 3, colonne 7", "LF", "typescript", "Espaces : 2", "UTF-8", "main"]) expect(screen.getByText(s)).toBeTruthy()
    expect(screen.queryByText(/sélectionné/)).toBeNull()
    expect(screen.queryByText("lecture seule")).toBeNull()
  })

  it("sélection, CRLF, lecture seule, tabulations", () => {
    bar({ editor: ed(st({ selected: 12, eol: "CRLF", readOnly: true, indent: "tabs:4" })) })
    for (const s of ["12 caractères sélectionnés", "CRLF", "lecture seule", "Tabulations"]) expect(screen.getByText(s)).toBeTruthy()
  })

  it("aucun onglet actif : rien sur la position ni l'éditeur ; hors dépôt : pas de branche", () => {
    bar({ editor: null, branch: undefined })
    expect(screen.queryByText(/Ligne/)).toBeNull()
    expect(screen.queryByText("LF")).toBeNull()
    expect(screen.queryByText(/Branche/)).toBeNull()
    expect(screen.getByText("UTF-8")).toBeTruthy()
  })

  it("état du flux et de l'index dans une région annoncée, la position n'en fait pas partie", () => {
    const { rerender } = bar({ index: "building" })
    const live = screen.getByRole("status")
    expect(live.textContent).toContain("Surveillance active")
    expect(live.textContent).toContain("Indexation en cours")
    expect(live.textContent).not.toContain("Ligne")
    const p = { editor: ed(st()), branch: "main", dirty: 0, term: term() }
    rerender(<StatusBar {...p} watch={{ connected: false, state: "waiting", retryIn: 15 }} index="partial" />)
    expect(live.textContent).toContain("reconnexion dans 15 s")
    expect(live.textContent).toContain("Index partiel")
    rerender(<StatusBar {...p} watch={{ connected: false, state: "browserRetry", retryIn: 0 }} index="ready" />)
    expect(live.textContent).toContain("Surveillance interrompue")
    expect(live.textContent).toContain("Index prêt")
  })

  it("onglets modifiés", () => {
    const { rerender } = bar({ dirty: 1 })
    expect(screen.getByText("1 onglet modifié")).toBeTruthy()
    rerender(<StatusBar editor={ed(st())} watch={{ connected: true, state: "open", retryIn: 0 }} index="ready" dirty={3} term={term()} />)
    expect(screen.getByText("3 onglets modifiés")).toBeTruthy()
  })

  it("bouton du terminal : bascule, ou désactivé avec la raison", () => {
    const t1 = term({ open: true })
    const { rerender } = bar({ term: t1 })
    const b = screen.getByRole("button", { name: "Ouvrir ou replier le terminal" })
    expect(b.getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(b)
    expect(t1.onToggle).toHaveBeenCalled()
    const t2 = term({ reason: "Le terminal demande le droit « exec »" })
    rerender(<StatusBar editor={ed(st())} watch={{ connected: true, state: "open", retryIn: 0 }} index="ready" dirty={0} term={t2} />)
    const off = screen.getByRole("button", { name: "Ouvrir ou replier le terminal" })
    expect(off.getAttribute("aria-disabled")).toBe("true")
    expect(document.getElementById(off.getAttribute("aria-describedby")!)?.textContent).toMatch(/exec/)
    fireEvent.click(off)
    expect(t2.onToggle).not.toHaveBeenCalled()
  })
})

describe("useEditorStatus", () => {
  const fake = () => {
    let l: ((s: EditorStatus) => void) | undefined
    const off = vi.fn()
    const h = { subscribe: vi.fn((f: (s: EditorStatus) => void) => ((l = f), f(st()), off)) } as unknown as EditorHandle
    return { h, off, push: (s: EditorStatus) => act(() => l?.(s)) }
  }
  it("suit l'éditeur actif, se désabonne de l'ancien, rien sans éditeur", () => {
    const a = fake()
    const b = fake()
    const { result, rerender } = renderHook(({ e }) => useEditorStatus(e), { initialProps: { e: a.h as EditorHandle | null } })
    expect(result.current?.line).toBe(3)
    a.push(st({ line: 9 }))
    expect(result.current?.line).toBe(9)
    rerender({ e: b.h })
    expect(a.off).toHaveBeenCalled()
    expect(result.current?.line).toBe(3)
    rerender({ e: null })
    expect(b.off).toHaveBeenCalled()
    expect(result.current).toBeNull()
  })
})

describe("branchOf", () => {
  const g = (branch: string) => ({ branch }) as GitState
  it("dépôt le plus proche contenant le dossier, rien hors dépôt", () => {
    const s = { "/r": g("main"), "/r/sub": g("dev"), "/rx": g("x") }
    expect(branchOf("/r", s)).toBe("main")
    expect(branchOf("/r/a/b", s)).toBe("main")
    expect(branchOf("/r/sub/c", s)).toBe("dev")
    expect(branchOf("/other", s)).toBeUndefined()
    expect(branchOf("/rxy", s)).toBeUndefined()
  })
})
