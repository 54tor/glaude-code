// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type FsFile } from "@/lib/api"
import type { EditorStatus } from "./commands"
import { IdeView } from "./IdeView"

// Compteur de rendus des onglets : ils se redessinent avec l'espace, pas avec la barre d'état.
const counts = vi.hoisted(() => ({ tabs: 0, listener: undefined as undefined | ((editorStatus: EditorStatus) => void) }))
vi.mock("./Tabs", async (orig) => {
  const real = await orig<typeof import("./Tabs")>()
  return { Tabs: (record: Parameters<typeof real.Tabs>[0]) => (counts.tabs++, real.Tabs(record)) }
})
vi.mock("./CodeEditor", async () => {
  const { useEffect } = await import("react")
  const handle = { undo() {}, redo() {}, find() {}, replace() {}, gotoLine() {}, palette() {}, focus() {}, zoom() {}, subscribe: (callback: (editorStatus: EditorStatus) => void) => ((counts.listener = callback), () => (counts.listener = undefined)) }
  const Editor = ({ editorRef }: { editorRef?: (value: unknown) => void }) => {
    useEffect(() => {
      if (!editorRef) return
      editorRef(handle)
      return () => editorRef(null)
    }, [editorRef])
    return <div />
  }
  return { default: Editor, setTsLib: () => {}, clearTsLibs: () => {} }
})

const f = (fsFile: FsFile): FsFile => fsFile
const st = (line: number): EditorStatus => ({ line, col: 1, selected: 0, eol: "LF", language: "plaintext", indent: "spaces:2", readOnly: false })

beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  vi.spyOn(api, "fsIndex").mockResolvedValue({ state: "ready", files: [] })
  vi.spyOn(api, "fsTree").mockResolvedValue({ entries: [{ name: "a.txt", type: "file", size: 1, mtime: 1 }], truncated: false })
  vi.spyOn(api, "fsFile").mockResolvedValue(f({ content: "x", etag: "e1", size: 1, mtime: 1 }))
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

// Sur téléphone (classes max-md, jsdom n'applique pas les media queries : on lit donc les classes posées),
// l'espace montre un seul volet : l'arbre d'abord, l'éditeur dès qu'un fichier est ouvert, et le bouton
// « Fichiers » ramène à l'arbre. Sur bureau ces classes n'ont aucun effet et les deux volets restent côte à côte.
it("sur téléphone, ouvrir un fichier bascule de l'arbre vers l'éditeur et « Fichiers » y revient", async () => {
  render(<IdeView dir="/p" />)
  const elementArbre = (await screen.findByText("a.txt", { selector: "[role=treeitem] span" })).closest("aside")!
  const boutonRetourAuxFichiers = screen.getByRole("button", { name: "Fichiers" })
  const colonneEditeur = boutonRetourAuxFichiers.parentElement!
  expect(elementArbre.className).not.toContain("max-md:hidden")
  expect(colonneEditeur.className).toContain("max-md:hidden")

  fireEvent.click(screen.getByText("a.txt", { selector: "[role=treeitem] span" }))
  await screen.findByRole("tab", { name: /a\.txt/ })
  expect(elementArbre.className).toContain("max-md:hidden")
  expect(colonneEditeur.className).not.toContain("max-md:hidden")

  fireEvent.click(boutonRetourAuxFichiers)
  expect(elementArbre.className).not.toContain("max-md:hidden")
  expect(colonneEditeur.className).toContain("max-md:hidden")
})
