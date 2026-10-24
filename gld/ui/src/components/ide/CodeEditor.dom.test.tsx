// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { createRef } from "react"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsRes } from "@/lib/api"
import { IdeView } from "./IdeView"
import type { EditorHandle, EditorStatus } from "./commands"

// Monaco simulé avec sa sémantique réelle : addCommand passe par le service de raccourcis partagé (le dernier inscrit gagne),
// onKeyDown est propre à chaque éditeur.
type Model = { uri: string; value: string; type(v: string): void }
// fire : évènements Monaco par nom ; live : écouteurs encore abonnés ; at : position, sélection, EOL, langage, indentation simulés.
const m = vi.hoisted(() => ({
  log: [] as string[],
  cmds: [] as (() => void)[],
  keys: new Map<string, ((k: object) => void)[]>(),
  models: new Map<string, Model>(),
  fire: new Map<string, (() => void)[]>(),
  live: 0,
  at: { line: 1, col: 1, sel: 0, eol: "\n", lang: "plaintext", spaces: true, size: 4 },
}))
vi.mock("./monacoSetup", () => ({}))
vi.mock("monaco-editor/languages/features/typescript/register", () => ({ typescriptDefaults: {} }))
vi.mock("monaco-editor/editor", () => {
  // type : frappe de l'utilisateur (le modèle change et prévient) ; setValue : écriture venue du composant, journalisée.
  const model = (name: string, v = "") => {
    const ls: (() => void)[] = []
    const o = {
      uri: name,
      value: v,
      getLanguageId: () => m.at.lang,
      getEOL: () => m.at.eol,
      getOptions: () => ({ insertSpaces: m.at.spaces, tabSize: m.at.size }),
      getValueLengthInRange: () => m.at.sel,
      getValue: () => o.value,
      type: (x: string) => ((o.value = x), ls.forEach((f) => f())),
      setValue: (x: string) => (m.log.push(`setValue:${x}`), (o.value = x), ls.forEach((f) => f())),
      onDidChangeContent: (f: () => void) => (ls.push(f), { dispose() {} }),
      dispose: () => m.log.push(`${name}.dispose`),
    }
    m.models.set(name, o)
    return o
  }
  const on = (name: string) => (f: () => void) => {
    m.fire.set(name, [...(m.fire.get(name) ?? []), f])
    m.live++
    return { dispose: () => (m.fire.set(name, (m.fire.get(name) ?? []).filter((x) => x !== f)), m.live--) }
  }
  return {
    Uri: { file: (p: string) => p },
    KeyMod: { CtrlCmd: 2048 },
    KeyCode: { KeyS: 49 },
    editor: {
      EditorOption: { readOnly: 91 },
      getModel: () => null,
      setTheme: () => {},
      createModel: (v: string, _l?: string, uri?: string) => model(uri ?? v, v),
      create: (el: HTMLElement, o: { model: { uri: string }; readOnly?: boolean }) => {
        m.log.push(`create:${o.model.uri}`)
        let ro = !!o.readOnly
        const ls: ((k: object) => void)[] = []
        m.keys.set(o.model.uri, ls)
        // Comme Monaco : écoute le keydown natif de son conteneur et expose preventDefault de l'évènement du navigateur.
        el.dataset.uri = o.model.uri
        el.tabIndex = 0
        el.addEventListener("keydown", (ev) => {
          const k = { ctrlKey: ev.ctrlKey, metaKey: ev.metaKey, altKey: ev.altKey, shiftKey: ev.shiftKey, keyCode: ev.key === "s" ? 49 : 0, preventDefault: () => ev.preventDefault(), stopPropagation() {} }
          for (const f of ls) f(k)
          if (!ev.defaultPrevented && ev.ctrlKey && ev.key === "s") m.cmds.at(-1)?.()
        })
        return {
          addCommand: (_k: number, f: () => void) => m.cmds.push(f),
          focus: () => el.focus(),
          onKeyDown: (f: (k: object) => void) => (ls.push(f), { dispose: () => m.log.push(`keys.dispose:${o.model.uri}`) }),
          updateOptions: (x: { readOnly?: boolean }) => (m.log.push(`options:${JSON.stringify(x)}`), x.readOnly !== undefined && (ro = x.readOnly)),
          getAction: (id: string) => ({ run: () => m.log.push(`action:${id}`) }),
          trigger: (_s: string, id: string) => m.log.push(`trigger:${id}`),
          getOption: () => ro,
          getModel: () => o.model,
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
  const k = { ctrlKey: true, metaKey: false, altKey: false, shiftKey: false, keyCode: 49, preventDefault: () => (prevented = true), stopPropagation() {} }
  for (const f of m.keys.get(path) ?? []) f(k)
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
    const r = render(<CodeEditor {...props} value="" />)
    const mdl = m.models.get("a.ts")!
    mdl.type("a")
    mdl.type("ab")
    r.rerender(<CodeEditor {...props} value="a" />)
    r.rerender(<CodeEditor {...props} value="ab" />)
    expect(mdl.value).toBe("ab")
    expect(m.log.filter((x) => x.startsWith("setValue"))).toEqual([])
    r.rerender(<CodeEditor {...props} value="disque" />)
    expect(mdl.value).toBe("disque")
    expect(m.log.filter((x) => x.startsWith("setValue"))).toEqual(["setValue:disque"])
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
    const h = ref.current!
    h.find()
    h.replace()
    h.gotoLine()
    h.palette()
    h.undo()
    h.redo()
    expect(m.log.filter((x) => /^(action|trigger):/.test(x))).toEqual([
      "action:actions.find",
      "action:editor.action.startFindReplaceAction",
      "action:editor.action.gotoLine",
      "action:editor.action.quickCommand",
      "trigger:undo",
      "trigger:redo",
    ])
    h.focus()
    expect(document.activeElement).toBe(document.querySelector('[data-uri="a.ts"]'))
    h.zoom(1)
    h.zoom(-1)
    h.zoom(0)
    expect(onFontSize.mock.calls).toEqual([[14], [12], [13]])
  })

  it("subscribe reçoit l'état courant puis chaque changement ; désabonné au démontage, poignée retirée et inerte", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const ref = createRef<EditorHandle>()
    const r = render(<CodeEditor {...props} readOnly editorRef={ref} />)
    const h = ref.current!
    const seen: EditorStatus[] = []
    h.subscribe((s) => seen.push(s))
    expect(seen.at(-1)).toEqual({ line: 1, col: 1, selected: 0, eol: "LF", language: "plaintext", indent: "spaces:4", readOnly: true })
    m.at = { line: 3, col: 5, sel: 4, eol: "\r\n", lang: "typescript", spaces: false, size: 2 }
    for (const f of m.fire.get("cursor") ?? []) f()
    expect(seen.at(-1)).toEqual({ line: 3, col: 5, selected: 4, eol: "CRLF", language: "typescript", indent: "tabs:2", readOnly: true })
    const off = vi.fn()
    const un = h.subscribe(off)
    un()
    off.mockClear()
    for (const f of m.fire.get("options") ?? []) f()
    expect(off).not.toHaveBeenCalled()
    expect(seen.length).toBe(3)
    h.subscribe(() => {})
    r.unmount()
    expect(m.live).toBe(0)
    expect(ref.current).toBeNull()
    const before = m.log.length
    expect(() => {
      h.find()
      h.undo()
      h.zoom(1)
      h.focus()
      h.subscribe(() => {})()
    }).not.toThrow()
    expect(m.log.length).toBe(before)
  })

  it("wordWrap, minimap, fontSize poussés dans les options sans recréer l'éditeur ni réécrire le modèle", async () => {
    const { default: CodeEditor } = await import("./CodeEditor")
    const r = render(<CodeEditor {...props} value="abc" />)
    m.log.length = 0
    r.rerender(<CodeEditor {...props} value="abc" wordWrap minimap fontSize={16} />)
    const opts = m.log.filter((x) => x.startsWith("options:")).map((x) => JSON.parse(x.slice(8)))
    expect(Object.assign({}, ...opts)).toMatchObject({ wordWrap: "on", minimap: { enabled: true }, fontSize: 16 })
    expect(m.log.filter((x) => x.startsWith("create:") || x.startsWith("setValue"))).toEqual([])
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
  const res = <T,>(status: number, body: object): FsRes<T> => ({ status, ok: status < 300, body: body as FsRes<T>["body"] })
  let save: MockInstance<typeof api.fsSave>
  beforeEach(() => {
    setLang("fr")
    localStorage.clear()
    vi.stubGlobal("EventSource", class { close() {} })
    vi.spyOn(api, "fsIndex").mockResolvedValue(res(200, { state: "ready", files: [] }))
    vi.spyOn(api, "fsTree").mockResolvedValue({ entries: ["a.txt", "mix.txt"].map((name) => ({ name, type: "file" as const, size: 1, mtime: 1 })), truncated: false })
    vi.spyOn(api, "fsFile").mockImplementation(async (_d, p) => res(200, { content: p === "mix.txt" ? "a\r\nb\n" : "x\n", etag: "e1", size: 2, mtime: 1 }))
    save = vi.spyOn(api, "fsSave").mockResolvedValue(res(200, { ok: true, etag: "e2" }))
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
    el.parentElement!.addEventListener("keydown", (e) => (seen = e.defaultPrevented))
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
    await new Promise((r) => setTimeout(r, 50))
    expect(save).not.toHaveBeenCalled()
  })
})
