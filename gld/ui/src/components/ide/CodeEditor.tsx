import { useEffect, useRef } from "react"
import * as monaco from "monaco-editor/editor"
import { typescriptDefaults } from "monaco-editor/languages/features/typescript/register"
import "./monacoSetup"
import { FONTS } from "@/lib/fonts"

type Theme = "light" | "dark"
const themeOf = (t: Theme) => (t === "dark" ? "vs-dark" : "vs")
const base = (theme: Theme) => ({ theme: themeOf(theme), fontFamily: FONTS.jetbrains.mono, fontSize: 13, automaticLayout: true, minimap: { enabled: false }, scrollBeyondLastLine: false })

// Sources du projet données au service TypeScript ; null retire l'entrée. Seul ce module touche Monaco.
// Clé identique à l'URI du modèle d'un onglet ouvert (encodée) : sinon TypeScript voit deux racines au même texte.
const libs = new Map<string, monaco.IDisposable>()
export function setTsLib(path: string, content: string | null) {
  libs.get(path)?.dispose()
  libs.delete(path)
  if (content !== null) libs.set(path, typescriptDefaults.addExtraLib(content, monaco.Uri.file(path).toString()))
}
export function clearTsLibs() {
  for (const d of libs.values()) d.dispose()
  libs.clear()
}

// Un éditeur et un modèle par onglet : le modèle naît avec l'onglet et disparaît avec lui. Le texte reçu sert tel quel (CRLF compris).
// focus : compteur de demandes de focus venues de l'espace ; 0 n'en demande aucune.
export default function CodeEditor({ path, value, readOnly, theme, focus, onChange, onSave }: { path: string; value: string; readOnly?: boolean; theme: Theme; focus?: number; onChange(v: string): void; onSave(v: string): void }) {
  const box = useRef<HTMLDivElement>(null)
  const model = useRef<monaco.editor.ITextModel | null>(null)
  const ed = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const cb = useRef({ onChange, onSave })
  // Valeurs émises que le parent n'a pas encore renvoyées : une frappe entre un rendu et son effet ferait sinon revenir le texte d'avant.
  const mine = useRef<string[]>([])
  useEffect(() => {
    cb.current = { onChange, onSave }
  })

  useEffect(() => {
    const uri = monaco.Uri.file(path)
    monaco.editor.getModel(uri)?.dispose()
    const m = monaco.editor.createModel(value, undefined, uri)
    const e = monaco.editor.create(box.current!, { ...base(theme), model: m, readOnly })
    // Pas addCommand : ce raccourci est global à Monaco, le dernier éditeur créé l'emporterait et Ctrl+S enregistrerait un autre onglet.
    const keys = e.onKeyDown((k) => {
      if (!(k.ctrlKey || k.metaKey) || k.altKey || k.shiftKey || k.keyCode !== monaco.KeyCode.KeyS) return
      k.preventDefault()
      cb.current.onSave(m.getValue())
    })
    const sub = m.onDidChangeContent(() => {
      const v = m.getValue()
      if (mine.current.push(v) > 64) mine.current.shift()
      cb.current.onChange(v)
    })
    model.current = m
    ed.current = e
    return () => {
      keys.dispose()
      sub.dispose()
      e.dispose()
      m.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path])

  // Rechargement depuis le disque : seule une valeur que nous n'avons pas émise vient de l'extérieur.
  useEffect(() => {
    const i = mine.current.indexOf(value)
    if (i >= 0) return void mine.current.splice(0, i + 1)
    mine.current = []
    if (model.current && model.current.getValue() !== value) model.current.setValue(value)
  }, [value])
  useEffect(() => {
    if (focus) ed.current?.focus()
  }, [focus])
  useEffect(() => {
    ed.current?.updateOptions({ readOnly })
  }, [readOnly])
  useEffect(() => {
    monaco.editor.setTheme(themeOf(theme))
  }, [theme])

  return <div ref={box} className="size-full" />
}

// Comparaison disque / onglet : à droite le modèle même de l'onglet, donc éditable et synchronisé avec lui.
export function DiffView({ path, original, modified, theme }: { path: string; original: string; modified: string; theme: Theme }) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const own = monaco.editor.getModel(monaco.Uri.file(path))
    const mod = own ?? monaco.editor.createModel(modified)
    const orig = monaco.editor.createModel(original, mod.getLanguageId())
    const d = monaco.editor.createDiffEditor(box.current!, { ...base(theme), originalEditable: false })
    // Modèle de vue gardé : d.dispose() ne l'arrête pas, et un calcul en vol sur un modèle disposé lève « no diff result available ».
    const vm = d.createViewModel({ original: orig, modified: mod })
    d.setModel(vm)
    return () => {
      d.dispose()
      vm.dispose()
      orig.dispose()
      if (!own) mod.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, original])
  return <div ref={box} className="size-full" />
}
