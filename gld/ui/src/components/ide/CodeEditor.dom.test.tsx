// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsRes } from "@/lib/api"
import { IdeView } from "./IdeView"

// Monaco simulé avec sa sémantique réelle : addCommand passe par le service de raccourcis partagé (le dernier inscrit gagne),
// onKeyDown est propre à chaque éditeur.
const m = vi.hoisted(() => ({ log: [] as string[], cmds: [] as (() => void)[], keys: new Map<string, ((k: object) => void)[]>() }))
vi.mock("./monacoSetup", () => ({}))
vi.mock("monaco-editor/languages/features/typescript/register", () => ({ typescriptDefaults: {} }))
vi.mock("monaco-editor/editor", () => {
  const model = (name: string, v = "") => ({ uri: name, getLanguageId: () => "plaintext", getValue: () => v, onDidChangeContent: () => ({ dispose() {} }), dispose: () => m.log.push(`${name}.dispose`) })
  return {
    Uri: { file: (p: string) => p },
    KeyMod: { CtrlCmd: 2048 },
    KeyCode: { KeyS: 49 },
    editor: {
      getModel: () => null,
      setTheme: () => {},
      createModel: (v: string, _l?: string, uri?: string) => model(uri ?? v, v),
      create: (el: HTMLElement, o: { model: { uri: string } }) => {
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
          updateOptions: () => {},
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
