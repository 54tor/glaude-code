// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

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
      create: (_el: unknown, o: { model: { uri: string } }) => {
        const ls: ((k: object) => void)[] = []
        m.keys.set(o.model.uri, ls)
        return {
          addCommand: (_k: number, f: () => void) => m.cmds.push(f),
          onKeyDown: (f: (k: object) => void) => (ls.push(f), { dispose() {} }),
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
