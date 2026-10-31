import { Suspense, useEffect, useLayoutEffect, useReducer, useRef, useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { cn } from "cn"
import { api, type FsFile } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { useCan } from "@/lib/auth"
import { useLocal } from "@/hooks/useLocal"
import { retryLazy } from "@/lib/retryLazy"
import { Confirm, type Ask } from "@/components/Confirm"
import { FileTree, type TreeActions } from "./FileTree"
import { TreeToolbar } from "./TreeToolbar"
import { EditorToolbar } from "./EditorToolbar"
import { Tabs } from "./Tabs"
import { ConflictBar } from "./ConflictBar"
import { ConflictDiff } from "./ConflictDiff"
import { QuickOpen } from "./QuickOpen"
import { GitChanges } from "../GitChanges"
import { useGitMarks } from "./useGitMarks"
import { isDirty, reduce } from "./ideState"
import { useTree } from "./useTree"
import { useIndex } from "./useIndex"
import { useWatch, type WatchEvent } from "./useWatch"
import { useTsFeed } from "./feedTs"
import { Handle } from "./Handle"
import { readDrafts, useDrafts, type Draft } from "./drafts"
import { clamp, MAX_TABS, mixedEol, nameOf, parentOf, parseTabs, useResolvedTheme } from "./ideHelpers"
import { commands, isMarkdown, type CmdCtx, type EditorHandle } from "./commands"
import { MarkdownPreview } from "./MarkdownPreview"
import { createScrollLink } from "./scrollLink"
import { useViewPrefs } from "./useViewPrefs"
import { MenuBar } from "./MenuBar"
import { WorkspaceStatus } from "./StatusBar"

// Lecture d'un fichier : le contenu, ou l'erreur levée par l'API (jamais les deux).
type Read = { file: FsFile; err?: undefined } | { file?: undefined; err: unknown }

const CodeEditor = retryLazy(() => import("./CodeEditor"))

const DEFAULT_TREE_WIDTH = 260
const MIN_TREE_WIDTH = 140
const MAX_TREE_WIDTH = 700
const clampTreeWidth = (width: number) => clamp(width, MIN_TREE_WIDTH, MAX_TREE_WIDTH)
const NOT_FOUND = 404
const CONFLICT = 409
const PAYLOAD_TOO_LARGE = 413
const UNSUPPORTED_MEDIA_TYPE = 415
const GIT_REFRESH_DELAY_MS = 1000
const INDEX_REFETCH_DELAY_MS = 1000
const WATCH_DEBOUNCE_MS = 150
const RECHECK_DELAY_MS = 300

const isPlainShortcut = (event: { ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean; defaultPrevented: boolean; key: string }, letter: string) => {
  const hasCtrlOrMeta = event.ctrlKey || event.metaKey
  const hasOtherModifier = event.altKey || event.shiftKey
  return hasCtrlOrMeta && !hasOtherModifier && !event.defaultPrevented && event.key.toLowerCase() === letter
}

// Sans droit de lecture, aucun hook de l'espace ne tourne : ni flux, ni sondage d'index, ni Ctrl+P pris.
export function IdeView({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  if (!can("fs_read")) return <p className="p-6 text-sm text-muted-foreground">{t("ide.denied")}</p>
  return <Workspace dir={dir} />
}

// Espace de travail d'un dossier : arborescence, zone centrale.
function Workspace({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  const [storedWidth, setStoredWidth] = useLocal(`gld.ide.${dir}.w`, String(DEFAULT_TREE_WIDTH))
  const [savedTabs, setSavedTabs] = useLocal(`gld.ide.${dir}.tabs`, "")
  const [restored, setRestored] = useState(false)
  const [drag, setDrag] = useState<{ width?: number }>({})
  const [ide, dispatch] = useReducer(reduce, { tabs: [], active: "" })
  const [diff, setDiff] = useState<{ path: string; original: string } | null>(null)
  const [confirm, setConfirm] = useState<Ask | null>(null)
  // État vu par les sauvegardes en attente : une sauvegarde mise en file repart du contenu courant, pas de celui de son clic.
  const live = useRef(ide)
  // Avant tout évènement : un Ctrl+S juste après l'ouverture doit déjà voir l'onglet.
  useLayoutEffect(() => {
    live.current = ide
  })
  const saving = useRef(new Map<string, boolean>()) // chemin → une nouvelle demande attend la fin de celle en cours
  const theme = useResolvedTheme()
  const prefs = useViewPrefs()
  // Poignée de l'éditeur de l'onglet actif, en état : les commandes qui en dépendent se réactivent dès son montage.
  const [editor, setEditor] = useState<EditorHandle | null>(null)
  const [scrollLink] = useState(createScrollLink)
  const tree = useTree(dir)
  const treeActions = useRef<TreeActions | null>(null)
  const [filter, setFilter] = useState("")
  const index = useIndex(dir)
  const git = useGitMarks(dir)
  const [quick, setQuick] = useState(false)
  const rootEl = useRef<HTMLDivElement>(null)
  const picked = useRef("")
  const [focusRequest, setFocusRequest] = useState({ path: "", count: 0 })
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const later = (timerKey: string, delayMs: number, callback: () => void) => {
    clearTimeout(timers.current.get(timerKey))
    const timer = setTimeout(() => {
      timers.current.delete(timerKey)
      callback()
    }, delayMs)
    timers.current.set(timerKey, timer)
  }
  const reloaded = useRef(new Set<string>())
  const latestReadByPath = useRef(new Map<string, number>()) // chemin → numéro de la dernière lecture lancée
  const width = drag.width ?? clampTreeWidth(Number(storedWidth) || DEFAULT_TREE_WIDTH)

  const tabOf = (path: string) => ide.tabs.find((tab) => tab.path === path)
  const liveTabOf = (path: string) => live.current.tabs.find((tab) => tab.path === path)
  const active = tabOf(ide.active)
  const fail = (error: unknown) => toast.error(error instanceof ApiError ? error.message : t("shell.act.failed"))
  const read = (path: string): Promise<Read> => api.fsFile(dir, path).then((file) => ({ file }), (err) => ({ err }))

  const open = async (path: string) => {
    if (tabOf(path)) return dispatch({ t: "focus", path })
    show(path, await read(path))
  }
  const dropped = (path: string) => toast(t("ide.drafts.dropped", { name: nameOf(path) }), { id: "ide-draft:" + path })
  // quiet : restauration au chargement, tout fichier devenu illisible (supprimé, refusé, panne) est sauté sans message.
  // draft : brouillon de la session, repris seulement si le disque n'a pas bougé depuis (même etag).
  const show = (path: string, result: Read, quiet?: boolean, draft?: Draft) => {
    const content = result.file?.content
    const status = result.err instanceof ApiError ? result.err.status : 0
    const mixed = content !== undefined && mixedEol(content)
    const readOnly = mixed || !can("fs_write")
    const keepDraft = !!draft && content !== undefined && !readOnly && draft.baseEtag === (result.file?.etag ?? "")
    if (draft && !keepDraft) dropped(path)
    if (status === PAYLOAD_TOO_LARGE || status === UNSUPPORTED_MEDIA_TYPE) {
      const notice = status === PAYLOAD_TOO_LARGE ? "tooBig" : "binary"
      return dispatch({ t: "open", tab: { path, content: "", saved: "", etag: "", readOnly: true, notice } })
    }
    if (content === undefined) return quiet ? undefined : fail(result.err)
    dispatch({
      t: "open",
      tab: {
        path,
        content: keepDraft ? draft.content : content,
        saved: content,
        etag: result.file?.etag ?? "",
        readOnly,
        ...(mixed && { notice: "mixedEol" as const }),
      },
    })
  }
  // Lectures en parallèle, onglets posés dans l'ordre mémorisé ; aucun focus pris, le premier rendu n'attend pas.
  useEffect(() => {
    const { paths, active } = parseTabs(savedTabs)
    const drafts = readDrafts(dir)
    let cancelled = false
    Promise.all(paths.map(read)).then((results) => {
      if (cancelled) return
      results.forEach((result, position) => show(paths[position], result, true, drafts[paths[position]]))
      if (active) dispatch({ t: "focus", path: active })
      setRestored(true)
    })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const tabsSnap = JSON.stringify({ paths: ide.tabs.slice(0, MAX_TABS).map((tab) => tab.path), active: ide.active })
  useEffect(() => {
    if (restored && tabsSnap !== savedTabs) setSavedTabs(tabsSnap)
  }, [restored, tabsSnap, savedTabs, setSavedTabs])
  useDrafts(dir, ide.tabs, restored)
  // force : « Recharger » de l'utilisateur, qui écarte exprès ses modifications.
  const closeDiffOf = (path: string) => setDiff((current) => (current?.path === path ? null : current))
  const reload = async (path: string, pre?: Read, force?: boolean) => {
    closeDiffOf(path)
    const result = pre ?? (await read(path))
    if (!result.file) {
      fail(result.err)
      return
    }
    const { content, etag } = result.file
    dispatch({ t: "reloaded", path, content, etag, force, ...(mixedEol(content) && { notice: "mixedEol" as const }) })
  }
  // force : écraser la version du disque ; create : recréer un fichier supprimé (etag vide).
  // Une sauvegarde à la fois par fichier : la suivante partirait avec l'ancien etag et lèverait un faux conflit contre la nôtre.
  // value : texte du modèle de l'éditeur au moment du Ctrl+S (la dernière frappe n'est peut-être pas encore dans l'état).
  const save = async (path: string, how?: "force" | "create", value?: string) => {
    const tab = liveTabOf(path)
    if (!tab || tab.readOnly) return
    if (saving.current.has(path)) return void saving.current.set(path, true)
    saving.current.set(path, false)
    // Une relecture partie avant la sauvegarde ne doit pas appliquer ensuite la version d'avant.
    latestReadByPath.current.set(path, (latestReadByPath.current.get(path) ?? 0) + 1)
    if (how) setDiff(null)
    let content = value ?? tab.content
    let etag = how === "create" ? "" : tab.etag
    let force = how === "force" || undefined
    let touched = false
    try {
      for (;;) {
        let saved: { etag: string } | undefined
        let diskEtag: string | undefined
        try {
          saved = await api.fsSave(dir, path, content, etag, force)
        } catch (error) {
          const isConflict = error instanceof ApiError && error.status === CONFLICT
          if (!isConflict) return fail(error)
          if (error.code === "deleted") return dispatch({ t: "disk", path, kind: "deleted" })
          diskEtag = error.etag
        }
        if (!saved) {
          // Décision sur l'onglet tel qu'il est maintenant : une frappe pendant l'aller-retour le rend sale.
          const diskRead = await read(path)
          const currentTab = liveTabOf(path)
          if (!currentTab) return
          const diskContent = diskRead.file?.content
          const readEtag = diskRead.file?.etag
          // Simple touch ou réécriture identique : on adopte le nouvel etag et on réessaie une fois.
          const sameAsSaved = diskContent === currentTab.saved
          const etagConsistent = readEtag === (diskEtag ?? readEtag)
          if (!touched && sameAsSaved && readEtag && etagConsistent) {
            touched = true
            etag = readEtag
            continue
          }
          if (!isDirty(currentTab) && diskContent !== undefined) return reload(path, diskRead)
          return dispatch({ t: "disk", path, kind: "changed", always: true })
        }
        dispatch({ t: "saved", path, etag: saved.etag, content })
        later("git", GIT_REFRESH_DELAY_MS, git.refresh)
        const latestTab = liveTabOf(path)
        const resaveRequested = saving.current.get(path)
        if (!resaveRequested || !latestTab || latestTab.content === content) return
        saving.current.set(path, false)
        content = latestTab.content
        etag = saved.etag
        force = undefined
      }
    } finally {
      saving.current.delete(path)
    }
  }
  // Relit le disque pour un onglet ouvert après un évènement : l'etag dit si quelque chose a changé (notre propre écho n'en change pas).
  // Pendant une sauvegarde on repousse : l'etag final dira si c'était notre écho ou un vrai changement extérieur.
  const check = async (path: string): Promise<void> => {
    const recheckLater = () => later("f:" + path, RECHECK_DELAY_MS, () => check(path))
    if (saving.current.has(path)) return recheckLater()
    const requestNumber = (latestReadByPath.current.get(path) ?? 0) + 1
    latestReadByPath.current.set(path, requestNumber)
    const result = await read(path)
    if (latestReadByPath.current.get(path) !== requestNumber) return
    if (saving.current.has(path)) return recheckLater()
    const tab = liveTabOf(path)
    if (!tab) return
    const status = result.err instanceof ApiError ? result.err.status : 0
    if (status === NOT_FOUND) return dispatch({ t: "disk", path, kind: "deleted" })
    const isNetworkOrUnknownError = !!result.err && (!(result.err instanceof ApiError) || result.err.net)
    const hasNoContent = tab.notice === "tooBig" || tab.notice === "binary"
    if (isNetworkOrUnknownError || hasNoContent) return
    // Relecture refusée (fichier devenu trop gros, panne) : l'onglet ne reflète plus le disque, on le dit au lieu de se taire.
    if (!result.file) return dispatch({ t: "disk", path, kind: "changed", always: true })
    if (result.file.etag === tab.etag) return
    if (result.file.content === tab.content) return dispatch({ t: "saved", path, etag: result.file.etag, content: tab.content })
    if (isDirty(tab)) return dispatch({ t: "disk", path, kind: "changed" })
    await reload(path, result)
    reloaded.current.add(path)
    later("toast", RECHECK_DELAY_MS, () => {
      const names = [...reloaded.current]
      reloaded.current.clear()
      const name = nameOf(names[0])
      const message = names.length === 1 ? t("ide.watch.reloaded", { name }) : t("ide.watch.reloadedMany", { count: names.length })
      toast(message, { id: "ide-reloaded" })
    })
  }
  const checkLater = (path: string) => later("f:" + path, WATCH_DEBOUNCE_MS, () => check(path))
  const onDisk = (event: WatchEvent) => {
    later("index", INDEX_REFETCH_DELAY_MS, index.refetch)
    later("git", GIT_REFRESH_DELAY_MS, git.refresh)
    if (event.type === "resync") {
      tree.refreshAll()
      for (const tab of live.current.tabs) checkLater(tab.path)
      return
    }
    const parent = parentOf(event.path)
    if (parent === "" || tree.open.has(parent)) later("d:" + parent, WATCH_DEBOUNCE_MS, () => tree.refresh(parent))
    for (const tab of live.current.tabs) {
      const isAffected = tab.path === event.path || tab.path.startsWith(event.path + "/")
      if (isAffected) checkLater(tab.path)
    }
  }
  const watch = useWatch(dir, ["", ...tree.open], onDisk)
  useEffect(() => {
    const pendingTimers = timers.current
    return () => pendingTimers.forEach(clearTimeout)
  }, [])

  useTsFeed(dir, index.files, index.state, live)

  const showDiff = async (path: string) => {
    const result = await read(path)
    if (result.file) setDiff({ path, original: result.file.content })
    else fail(result.err)
  }
  const close = (path: string) => {
    const tab = tabOf(path)
    const closeNow = () => {
      dispatch({ t: "close", path })
      closeDiffOf(path)
    }
    if (!tab || !isDirty(tab)) return closeNow()
    setConfirm({ title: t("ide.tabs.closeTitle", { name: nameOf(path) }), body: t("ide.tabs.closeBody"), confirm: t("ide.tabs.closeConfirm"), run: closeNow })
  }
  // Une seule confirmation pour tous les onglets modifiés : plusieurs boîtes s'écraseraient l'une l'autre.
  const closeAll = () => {
    const closeNow = () => {
      ide.tabs.forEach((tab) => dispatch({ t: "close", path: tab.path }))
      setDiff(null)
    }
    if (!ide.tabs.some(isDirty)) return closeNow()
    setConfirm({ title: t("ide.tabs.closeAllTitle"), body: t("ide.tabs.closeBody"), confirm: t("ide.tabs.closeConfirm"), run: closeNow })
  }
  const ctx: CmdCtx = {
    can,
    tab: active,
    tabs: ide.tabs,
    readOnly: !!active?.readOnly,
    editor,
    prefs,
    treeShown: prefs.showTree,
    newFile: () => treeActions.current?.create("newFile"),
    newFolder: () => treeActions.current?.create("newDir"),
    save: () => void (active && save(active.path)),
    saveAll: async () => {
      for (const tab of ide.tabs) {
        if (isDirty(tab) && !tab.readOnly) await save(tab.path)
      }
    },
    closeTab: () => active && close(active.path),
    closeAll,
    openQuick: () => setQuick(true),
  }
  const run = (id: string, context: CmdCtx = ctx) => {
    const command = commands.find((candidate) => candidate.id === id)
    if (command?.enabled(context)) command.run(context)
  }
  // Ctrl+S hors de Monaco (arbre, onglets) : jamais la boîte « Enregistrer la page » du navigateur.
  const onKeyDown = (event: KeyboardEvent) => {
    if (!isPlainShortcut(event, "s")) return
    event.preventDefault()
    run("file.save")
  }
  // Ctrl+P sur tout le document : l'espace est la page, et sans focus dedans le navigateur ouvrirait l'impression.
  // Ailleurs dans gld (espace démonté), l'impression reste disponible.
  useEffect(() => {
    const onDocumentKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!isPlainShortcut(event, "p")) return
      event.preventDefault()
      setQuick(true)
      document.querySelector<HTMLElement>('[aria-controls="ide-quick-list"]')?.focus()
    }
    document.addEventListener("keydown", onDocumentKeyDown)
    return () => document.removeEventListener("keydown", onDocumentKeyDown)
  }, [])
  // Fermeture de l'ouverture rapide : focus dans l'espace tout de suite, puis dans l'éditeur visé dès qu'il existe.
  const refocus = () => {
    const path = picked.current || live.current.active
    picked.current = ""
    rootEl.current?.focus()
    if (path) setFocusRequest((previous) => ({ path, count: previous.count + 1 }))
  }
  const endTreeResize = (size: number) => {
    setStoredWidth(String(clampTreeWidth(size)))
    setDrag({})
  }
  const pickQuick = (path: string) => {
    picked.current = path
    setQuick(false)
    open(path)
  }

  return (
    <div ref={rootEl} tabIndex={-1} className="flex min-h-0 flex-1 flex-col outline-none" onKeyDown={onKeyDown}>
      <MenuBar ctx={ctx} />
      <div className="flex min-h-0 flex-1">
        {prefs.showTree && (
          <>
            <aside className="flex min-h-0 shrink-0 flex-col" style={{ width }}>
              <TreeToolbar dir={dir} tree={tree} ctx={ctx} filter={filter} onFilter={setFilter} onRestored={tree.refreshAll} />
              {git.marks.size > 0 && (
                <div className="flex shrink-0 flex-col items-start px-1 pt-1">
                  <GitChanges dir={dir} count={git.marks.size} onOpen={open} prefix={git.prefix} />
                  {git.more > 0 && <p className="px-2 text-xs text-muted-foreground">{t("ide.git.more", { count: git.more })}</p>}
                </div>
              )}
              <FileTree dir={dir} tree={tree} marks={git.marks} dirs={git.dirs} canWrite={can("fs_write")} selected={ide.active} filter={filter} actionsRef={treeActions} onOpen={open} onRenamed={(from, to) => dispatch({ t: "renamed", from, to })} />
            </aside>
            <Handle label={t("ide.resize.tree")} start={() => width} onMove={(size) => setDrag({ width: clampTreeWidth(size) })} onEnd={endTreeResize} />
          </>
        )}
        <div className="flex min-w-0 flex-1 flex-col">
          <EditorToolbar ctx={ctx} />
          {ide.tabs.length > 0 && <Tabs tabs={ide.tabs} active={ide.active} onFocus={(path) => dispatch({ t: "focus", path })} onClose={close} />}
          {active?.conflict && (
            <ConflictBar
              kind={active.conflict}
              readOnly={active.readOnly}
              onReload={() => reload(active.path, undefined, true)}
              onOverwrite={() => save(active.path, "force")}
              onDiff={() => showDiff(active.path)}
              onRecreate={() => save(active.path, "create")}
              onClose={() => close(active.path)}
            />
          )}
          <div className="relative min-h-0 flex-1">
            {ide.tabs.length === 0 && <div className="grid h-full place-items-center p-6 text-sm text-muted-foreground">{t("ide.center.none")}</div>}
            <Suspense fallback={<p className="p-6 text-sm text-muted-foreground">{t("ide.editor.loading")}</p>}>
              {ide.tabs.map((tab) => (
                <div key={tab.path} className={cn("absolute inset-0", tab.path !== ide.active && "hidden")}>
                  {tab.notice === "tooBig" || tab.notice === "binary" ? (
                    <p className="p-6 text-sm text-muted-foreground">{t(tab.notice === "tooBig" ? "ide.notice.tooBig" : "ide.notice.binary")}</p>
                  ) : (
                    <div className="flex size-full flex-col">
                      {tab.notice === "mixedEol" && <p className="shrink-0 border-b px-3 py-1.5 text-sm text-muted-foreground">{t("ide.notice.mixedEol")}</p>}
                      <div className="flex min-h-0 flex-1">
                        <div className="min-w-0 flex-1">
                        <CodeEditor
                          path={tab.path}
                          value={tab.content}
                          readOnly={tab.readOnly}
                          theme={theme}
                          wordWrap={prefs.wordWrap}
                          minimap={prefs.minimap}
                          fontSize={prefs.fontSize}
                          onFontSize={(size) => prefs.set("fontSize", size)}
                          editorRef={tab.path === ide.active ? setEditor : undefined}
                          scrollLink={tab.path === ide.active && prefs.showPreview && isMarkdown(tab.path) ? scrollLink : undefined}
                          focus={focusRequest.path === tab.path ? focusRequest.count : 0}
                          onChange={(value) => !tab.readOnly && dispatch({ t: "edit", path: tab.path, content: value })}
                          onSave={(value) => save(tab.path, undefined, value)}
                        />
                        </div>
                        {prefs.showPreview && isMarkdown(tab.path) && (
                          <MarkdownPreview content={tab.content} link={tab.path === ide.active ? scrollLink : undefined} />
                        )}
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </Suspense>
            {diff && diff.path === ide.active && active && <ConflictDiff path={diff.path} original={diff.original} modified={active.content} theme={theme} onClose={() => setDiff(null)} />}
          </div>
        </div>
      </div>
      {prefs.showStatus && <WorkspaceStatus dir={dir} ctx={ctx} watch={watch} index={index.state} />}
      <Confirm ask={confirm} onClose={() => setConfirm(null)} />
      <QuickOpen open={quick} files={index.files.map((file) => file.path)} state={index.state} onClose={() => setQuick(false)} onCloseFocus={refocus} onPick={pickQuick} />
    </div>
  )
}
