// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type FsRes } from "@/lib/api"
import type { EditorStatus } from "./commands"
import { IdeView } from "./IdeView"

// Compteur de rendus des onglets : ils se redessinent avec l'espace, pas avec la barre d'état.
const counts = vi.hoisted(() => ({ tabs: 0, listener: undefined as undefined | ((s: EditorStatus) => void) }))
vi.mock("./Tabs", async (orig) => {
  const real = await orig<typeof import("./Tabs")>()
  return { Tabs: (p: Parameters<typeof real.Tabs>[0]) => (counts.tabs++, real.Tabs(p)) }
})
vi.mock("./CodeEditor", async () => {
  const { useEffect } = await import("react")
  const handle = { undo() {}, redo() {}, find() {}, replace() {}, gotoLine() {}, palette() {}, focus() {}, zoom() {}, subscribe: (l: (s: EditorStatus) => void) => ((counts.listener = l), () => (counts.listener = undefined)) }
  const Editor = ({ editorRef }: { editorRef?: (h: unknown) => void }) => {
    useEffect(() => {
      if (!editorRef) return
      editorRef(handle)
      return () => editorRef(null)
    }, [editorRef])
    return <div />
  }
  return { default: Editor, setTsLib: () => {}, clearTsLibs: () => {} }
})

const res = <T,>(status: number, body: object): FsRes<T> => ({ status, ok: status < 300, body: body as FsRes<T>["body"] })
const st = (line: number): EditorStatus => ({ line, col: 1, selected: 0, eol: "LF", language: "plaintext", indent: "spaces:2", readOnly: false })

beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  vi.spyOn(api, "fsIndex").mockResolvedValue(res(200, { state: "ready", files: [] }))
  vi.spyOn(api, "fsTree").mockResolvedValue({ entries: [{ name: "a.txt", type: "file", size: 1, mtime: 1 }], truncated: false })
  vi.spyOn(api, "fsFile").mockResolvedValue(res(200, { content: "x", etag: "e1", size: 1, mtime: 1 }))
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it("un déplacement du curseur redessine la barre d'état, pas l'espace", async () => {
  render(<IdeView dir="/p" />)
  fireEvent.click(await screen.findByText("a.txt", { selector: "[role=treeitem] span" }))
  await screen.findByRole("tab", { name: /a\.txt/ })
  const bar = screen.getByRole("group", { name: "Barre d'état" })
  await waitFor(() => expect(counts.listener).toBeDefined())
  const before = counts.tabs
  act(() => counts.listener?.(st(7)))
  expect(within(bar).getByText("Ligne 7, colonne 1")).toBeTruthy()
  act(() => counts.listener?.(st(8)))
  expect(within(bar).getByText("Ligne 8, colonne 1")).toBeTruthy()
  expect(counts.tabs).toBe(before)
})
