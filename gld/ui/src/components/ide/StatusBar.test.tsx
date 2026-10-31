// @vitest-environment jsdom
import { act, cleanup, render, renderHook, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { branchOf, StatusBar, useEditorStatus } from "./StatusBar"
import type { EditorHandle, EditorStatus } from "./commands"
import type { GitState } from "@/lib/api"

const st = (editorStatus: Partial<EditorStatus> = {}): EditorStatus => ({ line: 3, col: 7, selected: 0, eol: "LF", language: "typescript", indent: "spaces:2", readOnly: false, ...editorStatus })
const ed = (editorStatus: EditorStatus | null) => editorStatus && ({ subscribe: (callback: (localEditorStatus: EditorStatus) => void) => (callback(editorStatus), () => {}) } as unknown as EditorHandle)
const bar = (record: Partial<Parameters<typeof StatusBar>[0]> = {}) =>
  render(<StatusBar editor={ed(st())} watch={{ connected: true, state: "open", retryIn: 0 }} index="ready" branch="main" dirty={0} {...record} />)

beforeEach(() => setLang("fr"))
afterEach(cleanup)

describe("StatusBar", () => {
  it("position, fins de ligne, langage, indentation, encodage, branche", () => {
    bar()
    for (const text of ["Ligne 3, colonne 7", "LF", "typescript", "Espaces : 2", "UTF-8", "main"]) expect(screen.getByText(text)).toBeTruthy()
    expect(screen.queryByText(/sélectionné/)).toBeNull()
    expect(screen.queryByText("lecture seule")).toBeNull()
  })

  it("sélection, CRLF, lecture seule, tabulations", () => {
    bar({ editor: ed(st({ selected: 12, eol: "CRLF", readOnly: true, indent: "tabs:4" })) })
    for (const text of ["12 caractères sélectionnés", "CRLF", "lecture seule", "Tabulations"]) expect(screen.getByText(text)).toBeTruthy()
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
    const record = { editor: ed(st()), branch: "main", dirty: 0 }
    rerender(<StatusBar {...record} watch={{ connected: false, state: "waiting", retryIn: 15 }} index="partial" />)
    expect(live.textContent).toContain("reconnexion dans 15 s")
    expect(live.textContent).toContain("Index partiel")
    rerender(<StatusBar {...record} watch={{ connected: false, state: "browserRetry", retryIn: 0 }} index="ready" />)
    expect(live.textContent).toContain("Surveillance interrompue")
    expect(live.textContent).toContain("Index prêt")
  })

  it("onglets modifiés", () => {
    const { rerender } = bar({ dirty: 1 })
    expect(screen.getByText("1 onglet modifié")).toBeTruthy()
    rerender(<StatusBar editor={ed(st())} watch={{ connected: true, state: "open", retryIn: 0 }} index="ready" dirty={3} />)
    expect(screen.getByText("3 onglets modifiés")).toBeTruthy()
  })
})

describe("useEditorStatus", () => {
  const fake = () => {
    let callback: ((editorStatus: EditorStatus) => void) | undefined
    const off = vi.fn()
    const editorHandle = { subscribe: vi.fn((localCallback: (editorStatus: EditorStatus) => void) => ((callback = localCallback), localCallback(st()), off)) } as unknown as EditorHandle
    return { h: editorHandle, off, push: (editorStatus: EditorStatus) => act(() => callback?.(editorStatus)) }
  }
  it("suit l'éditeur actif, se désabonne de l'ancien, rien sans éditeur", () => {
    const record = fake()
    const localRecord = fake()
    const { result, rerender } = renderHook(({ e: editorHandle }) => useEditorStatus(editorHandle), { initialProps: { e: record.h as EditorHandle | null } })
    expect(result.current?.line).toBe(3)
    record.push(st({ line: 9 }))
    expect(result.current?.line).toBe(9)
    rerender({ e: localRecord.h })
    expect(record.off).toHaveBeenCalled()
    expect(result.current?.line).toBe(3)
    rerender({ e: null })
    expect(localRecord.off).toHaveBeenCalled()
    expect(result.current).toBeNull()
  })
})

describe("branchOf", () => {
  const callback = (branch: string) => ({ branch }) as GitState
  it("dépôt le plus proche contenant le dossier, rien hors dépôt", () => {
    const record = { "/r": callback("main"), "/r/sub": callback("dev"), "/rx": callback("x") }
    expect(branchOf("/r", record)).toBe("main")
    expect(branchOf("/r/a/b", record)).toBe("main")
    expect(branchOf("/r/sub/c", record)).toBe("dev")
    expect(branchOf("/other", record)).toBeUndefined()
    expect(branchOf("/rxy", record)).toBeUndefined()
  })
})
