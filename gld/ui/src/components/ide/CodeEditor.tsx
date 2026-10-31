import { useEffect, useRef, type Ref } from "react"
import * as monaco from "monaco-editor/editor"
import { typescriptDefaults } from "monaco-editor/languages/features/typescript/register"
import "./monacoSetup"
import { FONTS } from "@/lib/fonts"
import type { EditorHandle, EditorStatus } from "./commands"
import { SCROLL_TOLERANCE_PX, type ScrollLink } from "./scrollLink"
import { zoomed } from "./useViewPrefs"
import { touchOnly } from "@/lib/utils"

type Theme = "light" | "dark"
const DEFAULT_FONT_SIZE = 13
const MAX_PENDING_VALUES = 64
const themeOf = (theme: Theme) => (theme === "dark" ? "vs-dark" : "vs")
const view = (wordWrap?: boolean, minimap?: boolean, fontSize?: number) => ({
  wordWrap: wordWrap ? ("on" as const) : ("off" as const),
  minimap: { enabled: !!minimap },
  fontSize: fontSize ?? DEFAULT_FONT_SIZE,
})
const setRef = <T,>(ref: Ref<T> | undefined, value: T | null) => {
  if (typeof ref === "function") ref(value)
  else if (ref) ref.current = value
}
const base = (theme: Theme) => ({
  theme: themeOf(theme),
  fontFamily: FONTS.jetbrains.mono,
  fontSize: DEFAULT_FONT_SIZE,
  automaticLayout: true,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
})

// Sources du projet données au service TypeScript ; null retire l'entrée. Seul ce module touche Monaco.
// Clé identique à l'URI du modèle d'un onglet ouvert (encodée) : sinon TypeScript voit deux racines au même texte.
const libs = new Map<string, monaco.IDisposable>()
export function setTsLib(path: string, content: string | null) {
  libs.get(path)?.dispose()
  libs.delete(path)
  if (content !== null) libs.set(path, typescriptDefaults.addExtraLib(content, monaco.Uri.file(path).toString()))
}
export function clearTsLibs() {
  for (const lib of libs.values()) lib.dispose()
  libs.clear()
}

// Un éditeur et un modèle par onglet : le modèle naît avec l'onglet et disparaît avec lui. Le texte reçu sert tel quel (CRLF compris).
// focus : compteur de demandes de focus venues de l'espace ; 0 n'en demande aucune.
// scrollLink : défilement lié à l'aperçu Markdown ; posé seulement sur l'onglet actif.
// editorRef : poignée de l'éditeur, posée seulement sur l'onglet actif ; onFontSize : le zoom passe par la préférence, pas par l'éditeur seul.
type Props = {
  path: string
  value: string
  readOnly?: boolean
  theme: Theme
  focus?: number
  wordWrap?: boolean
  minimap?: boolean
  fontSize?: number
  editorRef?: Ref<EditorHandle | null>
  scrollLink?: ScrollLink
  onFontSize?(size: number): void
  onChange(value: string): void
  onSave(value: string): void
}
export default function CodeEditor({ path, value, readOnly, theme, focus, wordWrap, minimap, fontSize, editorRef, scrollLink, onFontSize, onChange, onSave }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const model = useRef<monaco.editor.ITextModel | null>(null)
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)
  const callbacks = useRef({ onChange, onSave, onFontSize, fontSize })
  const handle = useRef<{ editorHandle: EditorHandle; notify(): void } | null>(null)
  // Valeurs émises que le parent n'a pas encore renvoyées : une frappe entre un rendu et son effet ferait sinon revenir le texte d'avant.
  const pendingValues = useRef<string[]>([])
  useEffect(() => {
    callbacks.current = { onChange, onSave, onFontSize, fontSize }
  })

  useEffect(() => {
    const uri = monaco.Uri.file(path)
    monaco.editor.getModel(uri)?.dispose()
    const textModel = monaco.editor.createModel(value, undefined, uri)
    const codeEditor = monaco.editor.create(box.current!, {
      ...base(theme),
      ...view(wordWrap, minimap, fontSize),
      model: textModel,
      readOnly: readOnly || touchOnly(),
    })
    // Pas addCommand : ce raccourci est global à Monaco, le dernier éditeur créé l'emporterait et Ctrl+S enregistrerait un autre onglet.
    const keyListener = codeEditor.onKeyDown((keyEvent) => {
      const hasCtrlOrMeta = keyEvent.ctrlKey || keyEvent.metaKey
      const hasOtherModifier = keyEvent.altKey || keyEvent.shiftKey
      const isSaveShortcut = hasCtrlOrMeta && !hasOtherModifier && keyEvent.keyCode === monaco.KeyCode.KeyS
      if (!isSaveShortcut) return
      keyEvent.preventDefault()
      callbacks.current.onSave(textModel.getValue())
    })
    const contentListener = textModel.onDidChangeContent(() => {
      const currentValue = textModel.getValue()
      if (pendingValues.current.push(currentValue) > MAX_PENDING_VALUES) pendingValues.current.shift()
      callbacks.current.onChange(currentValue)
    })
    // alive : une poignée gardée après le démontage ne touche plus un éditeur libéré.
    let alive = true
    const listeners = new Set<(status: EditorStatus) => void>()
    const status = (): EditorStatus => {
      const options = textModel.getOptions()
      const position = codeEditor.getPosition()
      const selections = codeEditor.getSelections() ?? []
      return {
        line: position?.lineNumber ?? 1,
        col: position?.column ?? 1,
        selected: selections.reduce((total, range) => total + textModel.getValueLengthInRange(range), 0),
        eol: textModel.getEOL() === "\r\n" ? "CRLF" : "LF",
        language: textModel.getLanguageId(),
        indent: `${options.insertSpaces ? "spaces" : "tabs"}:${options.tabSize}`,
        readOnly: codeEditor.getOption(monaco.editor.EditorOption.readOnly),
      }
    }
    const notify = () => {
      if (!alive || !listeners.size) return
      const currentStatus = status()
      for (const listener of listeners) listener(currentStatus)
    }
    const editorListeners = [
      codeEditor.onDidChangeCursorPosition(notify),
      codeEditor.onDidChangeCursorSelection(notify),
      codeEditor.onDidChangeModel(notify),
      codeEditor.onDidChangeModelContent(notify),
      codeEditor.onDidChangeModelOptions(notify),
    ]
    const action = (id: string) => () => void (alive && codeEditor.getAction(id)?.run())
    const editorHandle: EditorHandle = {
      undo: () => void (alive && codeEditor.trigger("keyboard", "undo", null)),
      redo: () => void (alive && codeEditor.trigger("keyboard", "redo", null)),
      find: action("actions.find"),
      replace: action("editor.action.startFindReplaceAction"),
      gotoLine: action("editor.action.gotoLine"),
      palette: action("editor.action.quickCommand"),
      focus: () => void (alive && codeEditor.focus()),
      zoom: (delta) =>
        void (alive && callbacks.current.onFontSize?.(zoomed(callbacks.current.fontSize ?? DEFAULT_FONT_SIZE, delta))),
      subscribe(listener) {
        if (!alive) return () => {}
        listeners.add(listener)
        listener(status())
        return () => void listeners.delete(listener)
      },
    }
    handle.current = { editorHandle, notify }
    model.current = textModel
    editor.current = codeEditor
    return () => {
      alive = false
      listeners.clear()
      handle.current = null
      for (const disposable of editorListeners) disposable.dispose()
      keyListener.dispose()
      contentListener.dispose()
      codeEditor.dispose()
      textModel.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path])

  useEffect(() => {
    const editorHandle = handle.current?.editorHandle
    if (!editorRef || !editorHandle) return
    setRef(editorRef, editorHandle)
    return () => setRef(editorRef, null)
  }, [editorRef, path])
  useEffect(() => {
    const codeEditor = editor.current
    if (!scrollLink || !codeEditor) return
    const scrollableHeight = () => Math.max(1, codeEditor.getScrollHeight() - codeEditor.getLayoutInfo().height)
    const detach = scrollLink.attachEditor((ratio) => {
      const target = ratio * scrollableHeight()
      if (Math.abs(codeEditor.getScrollTop() - target) > SCROLL_TOLERANCE_PX) codeEditor.setScrollTop(target)
    })
    const listener = codeEditor.onDidScrollChange(() => scrollLink.fromEditor(codeEditor.getScrollTop() / scrollableHeight()))
    return () => {
      detach()
      listener.dispose()
    }
  }, [scrollLink, path])
  useEffect(() => {
    editor.current?.updateOptions(view(wordWrap, minimap, fontSize))
  }, [wordWrap, minimap, fontSize])

  // Rechargement depuis le disque : seule une valeur que nous n'avons pas émise vient de l'extérieur.
  useEffect(() => {
    const pendingIndex = pendingValues.current.indexOf(value)
    if (pendingIndex >= 0) return void pendingValues.current.splice(0, pendingIndex + 1)
    pendingValues.current = []
    if (model.current && model.current.getValue() !== value) model.current.setValue(value)
  }, [value])
  useEffect(() => {
    if (focus) editor.current?.focus()
  }, [focus])
  useEffect(() => {
    editor.current?.updateOptions({ readOnly: readOnly || touchOnly() })
    handle.current?.notify()
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
    const tabModel = monaco.editor.getModel(monaco.Uri.file(path))
    const modifiedModel = tabModel ?? monaco.editor.createModel(modified)
    const originalModel = monaco.editor.createModel(original, modifiedModel.getLanguageId())
    const diffEditor = monaco.editor.createDiffEditor(box.current!, { ...base(theme), originalEditable: false })
    // Modèle de vue gardé : diffEditor.dispose() ne l'arrête pas, et un calcul en vol sur un modèle disposé lève « no diff result available ».
    const viewModel = diffEditor.createViewModel({ original: originalModel, modified: modifiedModel })
    diffEditor.setModel(viewModel)
    return () => {
      diffEditor.dispose()
      viewModel.dispose()
      originalModel.dispose()
      if (!tabModel) modifiedModel.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, original])
  return <div ref={box} className="size-full" />
}
