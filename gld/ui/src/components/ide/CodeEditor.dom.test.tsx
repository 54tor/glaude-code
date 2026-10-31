// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { createRef } from "react"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsFile } from "@/lib/api"
import { IdeView } from "./IdeView"
import type { EditorHandle, EditorStatus } from "./commands"

// Monaco simulé avec sa sémantique réelle : addCommand passe par le service de raccourcis partagé (le dernier inscrit gagne),
// onKeyDown est propre à chaque éditeur.
type Model = { uri: string; value: string; type(v: string): void }
// fire : évènements Monaco par nom ; live : écouteurs encore abonnés ; at : position, sélection, EOL, langage, indentation simulés.
const m = vi.hoisted(() => ({
  log: [] as string[],
  cmds: [] as (() => void)[],
  keys: new Map<string, ((object: object) => void)[]>(),
  models: new Map<string, Model>(),
  fire: new Map<string, (() => void)[]>(),
  live: 0,
  at: { line: 1, col: 1, sel: 0, eol: "\n", lang: "plaintext", spaces: true, size: 4 },
}))
vi.mock("./monacoSetup", () => ({}))
vi.mock("monaco-editor/languages/features/typescript/register", () => ({ typescriptDefaults: {} }))
vi.mock("monaco-editor/editor", () => {
  // type : frappe de l'utilisateur (le modèle change et prévient) ; setValue : écriture venue du composant, journalisée.
  const model = (name: string, localValue = "") => {
    const ls: (() => void)[] = []
    const record = {
      uri: name,
      value: localValue,
      getLanguageId: () => m.at.lang,
      getEOL: () => m.at.eol,
      getOptions: () => ({ insertSpaces: m.at.spaces, tabSize: m.at.size }),
      getValueLengthInRange: () => m.at.sel,
      getValue: () => record.value,
      type: (input: string) => ((record.value = input), ls.forEach((callback) => callback())),
      setValue: (input: string) => (m.log.push(`setValue:${input}`), (record.value = input), ls.forEach((callback) => callback())),
      onDidChangeContent: (callback: () => void) => (ls.push(callback), { dispose() {} }),
      dispose: () => m.log.push(`${name}.dispose`),
    }
    m.models.set(name, record)
    return record
  }
  const on = (name: string) => (callback: () => void) => {
    m.fire.set(name, [...(m.fire.get(name) ?? []), callback])
    m.live++
    return { dispose: () => (m.fire.set(name, (m.fire.get(name) ?? []).filter((localCallback) => localCallback !== callback)), m.live--) }
  }
  return {
    Uri: { file: (path: string) => path },
    KeyMod: { CtrlCmd: 2048 },
    KeyCode: { KeyS: 49 },
    editor: {
      EditorOption: { readOnly: 91 },
      getModel: () => null,
      setTheme: () => {},
      createModel: (value: string, _l?: string, uri?: string) => model(uri ?? value, value),
      create: (el: HTMLElement, record: { model: { uri: string }; readOnly?: boolean }) => {
        m.log.push(`create:${record.model.uri}`)
        let ro = !!record.readOnly
        const ls: ((object: object) => void)[] = []
        m.keys.set(record.model.uri, ls)
        // Comme Monaco : écoute le keydown natif de son conteneur et expose preventDefault de l'évènement du navigateur.
        el.dataset.uri = record.model.uri
        el.tabIndex = 0
        el.addEventListener("keydown", (ev) => {
          const record = { ctrlKey: ev.ctrlKey, metaKey: ev.metaKey, altKey: ev.altKey, shiftKey: ev.shiftKey, keyCode: ev.key === "s" ? 49 : 0, preventDefault: () => ev.preventDefault(), stopPropagation() {} }
          for (const callback of ls) callback(record)
          if (!ev.defaultPrevented && ev.ctrlKey && ev.key === "s") m.cmds.at(-1)?.()
        })
        return {
          addCommand: (_k: number, callback: () => void) => m.cmds.push(callback),
          focus: () => el.focus(),
          onKeyDown: (callback: (object: object) => void) => (ls.push(callback), { dispose: () => m.log.push(`keys.dispose:${record.model.uri}`) }),
          updateOptions: (record: { readOnly?: boolean }) => (m.log.push(`options:${JSON.stringify(record)}`), record.readOnly !== undefined && (ro = record.readOnly)),
          getAction: (id: string) => ({ run: () => m.log.push(`action:${id}`) }),
          trigger: (_s: string, id: string) => m.log.push(`trigger:${id}`),
          getOption: () => ro,
          getModel: () => record.model,
          getPosition: () => ({ lineNumber: m.at.line, column: m.at.col }),
          getSelections: () => [{}],
          onDidChangeCursorPosition: on("cursor"),
          onDidChangeCursorSelection: on("selection"),
          onDidChangeModel: on("model"),
          onDidChangeModelContent: on("content"),
          onDidChangeModelOptions: on("options"),
          dispose: () => {},
        }
      },
      createDiffEditor: () => ({
        createViewModel: () => ({ dispose: () => m.log.push("vm.dispose") }),
        setModel: () => {},
        dispose: () => m.log.push("diff.dispose"),
      }),
    },
  }
})

// Ctrl+S dans un éditeur : ses propres écouteurs d'abord, puis le raccourci global si personne ne l'a pris.
const ctrlS = (path: string) => {
  let prevented = false
  const record = { ctrlKey: true, metaKey: false, altKey: false, shiftKey: false, keyCode: 49, preventDefault: () => (prevented = true), stopPropagation() {} }
  for (const callback of m.keys.get(path) ?? []) callback(record)
  if (!prevented) m.cmds.at(-1)?.()
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  setAuth({ state: "loading", rights: undefined, home: undefined })
  m.log.length = 0
  m.cmds.length = 0
  m.keys.clear()
  m.models.clear()
  m.fire.clear()
  m.live = 0
  m.at = { line: 1, col: 1, sel: 0, eol: "\n", lang: "plaintext", spaces: true, size: 4 }
})

describe("CodeEditor", () => {
  it("Ctrl+S enregistre l'onglet de l'éditeur focalisé, pas le dernier ouvert", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const save = vi.fn()
    render(
      <>
        <CodeEditor path="a.ts" value="" theme="light" onChange={() => {}} onSave={() => save("a.ts")} />
        <CodeEditor path="b.ts" value="" theme="light" onChange={() => {}} onSave={() => save("b.ts")} />
      </>
    )
    ctrlS("a.ts")
    expect(save.mock.calls).toEqual([["a.ts"]])
  })
})

describe("CodeEditor : synchro valeur / modèle", () => {
  it("une valeur périmée renvoyée par le parent pendant la frappe n'annule rien ; une valeur extérieure remplace le modèle", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const props = { path: "a.ts", theme: "light" as const, onChange: vi.fn(), onSave: () => {} }
    const element = render(<CodeEditor {...props} value="" />)
    const mdl = m.models.get("a.ts")!
    mdl.type("a")
    mdl.type("ab")
    element.rerender(<CodeEditor {...props} value="a" />)
    element.rerender(<CodeEditor {...props} value="ab" />)
    expect(mdl.value).toBe("ab")
    expect(m.log.filter((input) => input.startsWith("setValue"))).toEqual([])
    element.rerender(<CodeEditor {...props} value="disque" />)
    expect(mdl.value).toBe("disque")
    expect(m.log.filter((input) => input.startsWith("setValue"))).toEqual(["setValue:disque"])
  })

  it("Ctrl+S donne le texte du modèle, dernière frappe comprise", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const onSave = vi.fn()
    render(<CodeEditor path="a.ts" value="v1" theme="light" onChange={() => {}} onSave={onSave} />)
    m.models.get("a.ts")!.type("v12")
    ctrlS("a.ts")
    expect(onSave).toHaveBeenCalledWith("v12")
  })
})

describe("CodeEditor : poignée impérative", () => {
  const props = { path: "a.ts", value: "", theme: "light" as const, onChange: () => {}, onSave: () => {} }

  it("chaque méthode déclenche l'action Monaco correspondante ; zoom passe par la préférence", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const ref = createRef<EditorHandle>()
    const onFontSize = vi.fn()
    render(<CodeEditor {...props} fontSize={13} onFontSize={onFontSize} editorRef={ref} />)
    const editorHandle = ref.current!
    editorHandle.find()
    editorHandle.replace()
    editorHandle.gotoLine()
    editorHandle.palette()
    editorHandle.undo()
    editorHandle.redo()
    expect(m.log.filter((input) => /^(action|trigger):/.test(input))).toEqual([
      "action:actions.find",
      "action:editor.action.startFindReplaceAction",
      "action:editor.action.gotoLine",
      "action:editor.action.quickCommand",
      "trigger:undo",
      "trigger:redo",
    ])
    editorHandle.focus()
    expect(document.activeElement).toBe(document.querySelector('[data-uri="a.ts"]'))
    editorHandle.zoom(1)
    editorHandle.zoom(-1)
    editorHandle.zoom(0)
    expect(onFontSize.mock.calls).toEqual([[14], [12], [13]])
  })

  it("subscribe reçoit l'état courant puis chaque changement ; désabonné au démontage, poignée retirée et inerte", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const ref = createRef<EditorHandle>()
    const element = render(<CodeEditor {...props} readOnly editorRef={ref} />)
    const editorHandle = ref.current!
    const seen: EditorStatus[] = []
    editorHandle.subscribe((editorStatus) => seen.push(editorStatus))
    expect(seen.at(-1)).toEqual({ line: 1, col: 1, selected: 0, eol: "LF", language: "plaintext", indent: "spaces:4", readOnly: true })
    m.at = { line: 3, col: 5, sel: 4, eol: "\r\n", lang: "typescript", spaces: false, size: 2 }
    for (const callback of m.fire.get("cursor") ?? []) callback()
    expect(seen.at(-1)).toEqual({ line: 3, col: 5, selected: 4, eol: "CRLF", language: "typescript", indent: "tabs:2", readOnly: true })
    const off = vi.fn()
    const un = editorHandle.subscribe(off)
    un()
    off.mockClear()
    for (const callback of m.fire.get("options") ?? []) callback()
    expect(off).not.toHaveBeenCalled()
    expect(seen.length).toBe(3)
    editorHandle.subscribe(() => {})
    element.unmount()
    expect(m.live).toBe(0)
    expect(ref.current).toBeNull()
    const before = m.log.length
    expect(() => {
      editorHandle.find()
      editorHandle.undo()
      editorHandle.zoom(1)
      editorHandle.focus()
      editorHandle.subscribe(() => {})()
    }).not.toThrow()
    expect(m.log.length).toBe(before)
  })

  it("wordWrap, minimap, fontSize poussés dans les options sans recréer l'éditeur ni réécrire le modèle", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const element = render(<CodeEditor {...props} value="abc" />)
    m.log.length = 0
    element.rerender(<CodeEditor {...props} value="abc" wordWrap minimap fontSize={16} />)
    const opts = m.log.filter((input) => input.startsWith("options:")).map((input) => JSON.parse(input.slice(8)))
    expect(Object.assign({}, ...opts)).toMatchObject({ wordWrap: "on", minimap: { enabled: true }, fontSize: 16 })
    expect(m.log.filter((input) => input.startsWith("create:") || input.startsWith("setValue"))).toEqual([])
  })
})

describe("DiffView", () => {
  it("arrête le calcul du diff (modèle de vue) avant de libérer les modèles", async () => {
    const { DiffView } = await import("./CodeEditor")
    render(<DiffView path="a.ts" original="disk" modified="mine" theme="light" />)
    cleanup()
    expect(m.log).toEqual(["diff.dispose", "vm.dispose", "disk.dispose", "mine.dispose"])
  })
})

// Espace complet avec le vrai CodeEditor (Monaco simulé) : le Ctrl+S de l'éditeur ne doit pas être repris par la racine de l'espace.
describe("IdeView + CodeEditor : Ctrl+S", () => {
  const callback = (record: { content: string; etag: string; size: number; mtime: number }): FsFile => record
  let save: MockInstance<typeof api.fsSave>
  beforeEach(() => {
    setLang("fr")
    localStorage.clear()
    vi.stubGlobal("EventSource", class { close() {} })
    vi.spyOn(api, "fsIndex").mockResolvedValue({ state: "ready", files: [] })
    vi.spyOn(api, "fsTree").mockResolvedValue({ entries: ["a.txt", "mix.txt"].map((name) => ({ name, type: "file" as const, size: 1, mtime: 1 })), truncated: false })
    vi.spyOn(api, "fsFile").mockImplementation(async (_d, path) => callback({ content: path === "mix.txt" ? "a\r\nb\n" : "x\n", etag: "e1", size: 2, mtime: 1 }))
    save = vi.spyOn(api, "fsSave").mockResolvedValue({ etag: "e2" })
  })
  const openEditor = async (name: string) => {
    fireEvent.click(await screen.findByText(name, { selector: "[role=treeitem] span" }))
    return waitFor(() => {
      const el = document.querySelector<HTMLElement>(`[data-uri="${name}"]`)
      expect(el).toBeTruthy()
      return el!
    })
  }

  it("un Ctrl+S = un seul enregistrement, déjà empêché avant la racine ; écouteur libéré au démontage", async () => {
    const { unmount } = render(<IdeView dir="/p" />)
    const el = await openEditor("a.txt")
    let seen: boolean | undefined
    el.parentElement!.addEventListener("keydown", (event) => (seen = event.defaultPrevented))
    fireEvent.keyDown(el, { key: "s", ctrlKey: true })
    expect(seen).toBe(true)
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
    expect(save.mock.calls[0][1]).toBe("a.txt")
    unmount()
    expect(m.log).toContain("keys.dispose:a.txt")
  })

  it("onglet en lecture seule (fins de ligne mixtes, ou utilisateur fs_read seul) : Ctrl+S n'enregistre jamais", async () => {
    render(<IdeView dir="/p" />)
    fireEvent.keyDown(await openEditor("mix.txt"), { key: "s", ctrlKey: true })
    cleanup()
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } })
    render(<IdeView dir="/p" />)
    fireEvent.keyDown(await openEditor("a.txt"), { key: "s", ctrlKey: true })
    await new Promise((callback) => setTimeout(callback, 50))
    expect(save).not.toHaveBeenCalled()
  })
})
