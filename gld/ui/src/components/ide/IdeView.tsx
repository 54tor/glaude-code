import { lazy, Suspense, useEffect, useReducer, useRef, useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { cn } from "cn"
import { api, type FsRes } from "@/lib/api"
import { useCan } from "@/lib/auth"
import { useLocal } from "@/hooks/useLocal"
import { Confirm, type Ask } from "@/components/Confirm"
import { FileTree, type TreeActions } from "./FileTree"
import { TreeToolbar } from "./TreeToolbar"
import { EditorToolbar } from "./EditorToolbar"
import { Tabs } from "./Tabs"
import { ConflictBar } from "./ConflictBar"
import { ConflictDiff } from "./ConflictDiff"
import { QuickOpen } from "./QuickOpen"
import { GitChanges } from "../GitChanges"
import { BottomPane, type BottomView } from "./BottomPane"
import { useGitMarks } from "./useGitMarks"
import { isDirty, reduce } from "./ideState"
import { useTree } from "./useTree"
import { useIndex } from "./useIndex"
import { useWatch, type WatchEvent } from "./useWatch"
import { pickFeed, runFeed } from "./feedTs"
import { Handle } from "./Handle"
import { readDrafts, useDrafts, type Draft } from "./drafts"
import { clamp, MAX_TABS, mixedEol, nameOf, parentOf, parseTabs, useResolvedTheme } from "./ideHelpers"
import { commands, type CmdCtx, type EditorHandle } from "./commands"
import { useViewPrefs } from "./useViewPrefs"

const CodeEditor = lazy(() => import("./CodeEditor"))

// Sans droit de lecture, aucun hook de l'espace ne tourne : ni flux, ni sondage d'index, ni Ctrl+P pris.
export function IdeView({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  if (!can("fs_read")) return <p className="p-6 text-sm text-muted-foreground">{t("ide.denied")}</p>
  return <Workspace dir={dir} />
}

// Espace de travail d'un dossier : arborescence, zone centrale, zone terminal du bas (barre toujours visible, jamais de shell : elle s'attache à une session Claude vivante).
function Workspace({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  const [wStr, setW] = useLocal(`gld.ide.${dir}.w`, "260")
  const [savedTabs, setSavedTabs] = useLocal(`gld.ide.${dir}.tabs`, "")
  const [restored, setRestored] = useState(false)
  const [drag, setDrag] = useState<{ w?: number }>({})
  const [ide, dispatch] = useReducer(reduce, { tabs: [], active: "" })
  const [diff, setDiff] = useState<{ path: string; original: string } | null>(null)
  const [confirm, setConfirm] = useState<Ask | null>(null)
  // État vu par les sauvegardes en attente : une sauvegarde mise en file repart du contenu courant, pas de celui de son clic.
  const live = useRef(ide)
  useEffect(() => {
    live.current = ide
  })
  const saving = useRef(new Map<string, boolean>()) // chemin → une nouvelle demande attend la fin de celle en cours
  const theme = useResolvedTheme()
  const prefs = useViewPrefs()
  const [term, setTerm] = useState<BottomView>({ open: false, available: false })
  const [termFocus, setTermFocus] = useState(0)
  // Poignée de l'éditeur de l'onglet actif, en état : les commandes qui en dépendent se réactivent dès son montage.
  const [editor, setEditor] = useState<EditorHandle | null>(null)
  const tree = useTree(dir)
  const treeActions = useRef<TreeActions | null>(null)
  const [filter, setFilter] = useState("")
  const index = useIndex(dir)
  const git = useGitMarks(dir)
  const [quick, setQuick] = useState(false)
  const rootEl = useRef<HTMLDivElement>(null)
  const picked = useRef("")
  const [focusReq, setFocusReq] = useState({ path: "", n: 0 })
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const later = (k: string, ms: number, f: () => void) => {
    clearTimeout(timers.current.get(k))
    timers.current.set(k, setTimeout(() => (timers.current.delete(k), f()), ms))
  }
  const reloaded = useRef(new Set<string>())
  const seq = useRef(new Map<string, number>()) // chemin → numéro de la dernière lecture lancée
  const fed = useRef(new Map<string, string>()) // chemin → « mtime:taille » de l'index au moment de l'alimentation
  const w = drag.w ?? clamp(Number(wStr) || 260, 140, 700)

  const tabOf = (p: string) => ide.tabs.find((x) => x.path === p)
  const active = tabOf(ide.active)
  const fail = (r: FsRes) => toast.error(r.body.error || t("shell.act.failed"))

  const open = async (path: string) => {
    if (tabOf(path)) return dispatch({ t: "focus", path })
    show(path, await api.fsFile(dir, path))
  }
  const dropped = (path: string) => toast(t("ide.drafts.dropped", { name: nameOf(path) }), { id: "ide-draft:" + path })
  // quiet : restauration au chargement, tout fichier devenu illisible (supprimé, refusé, panne) est sauté sans message.
  // draft : brouillon de la session, repris seulement si le disque n'a pas bougé depuis (même etag).
  const show = (path: string, r: Awaited<ReturnType<typeof api.fsFile>>, quiet?: boolean, draft?: Draft) => {
    const c = r.ok ? r.body.content : undefined
    const mixed = c !== undefined && mixedEol(c)
    const readOnly = mixed || !can("fs_write")
    const keep = !!draft && c !== undefined && !readOnly && draft.baseEtag === (r.body.etag ?? "")
    if (draft && !keep) dropped(path)
    if (r.status === 413 || r.status === 415)
      return dispatch({ t: "open", tab: { path, content: "", saved: "", etag: "", readOnly: true, notice: r.status === 413 ? "tooBig" : "binary" } })
    if (c === undefined) return quiet ? undefined : fail(r)
    dispatch({ t: "open", tab: { path, content: keep ? draft.content : c, saved: c, etag: r.body.etag ?? "", readOnly, ...(mixed && { notice: "mixedEol" as const }) } })
  }
  // Lectures en parallèle, onglets posés dans l'ordre mémorisé ; aucun focus pris, le premier rendu n'attend pas.
  useEffect(() => {
    const { paths, active } = parseTabs(savedTabs)
    const drafts = readDrafts(dir)
    let gone = false
    Promise.all(paths.map((p) => api.fsFile(dir, p).catch(() => null))).then((rs) => {
      if (gone) return
      rs.forEach((r, i) => (r ? show(paths[i], r, true, drafts[paths[i]]) : drafts[paths[i]] && dropped(paths[i])))
      if (active) dispatch({ t: "focus", path: active })
      setRestored(true)
    })
    return () => {
      gone = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const tabsSnap = JSON.stringify({ paths: ide.tabs.slice(0, MAX_TABS).map((x) => x.path), active: ide.active })
  useEffect(() => {
    if (restored && tabsSnap !== savedTabs) setSavedTabs(tabsSnap)
  }, [restored, tabsSnap, savedTabs, setSavedTabs])
  useDrafts(dir, ide.tabs, restored)
  // force : « Recharger » de l'utilisateur, qui écarte exprès ses modifications.
  const reload = async (path: string, pre?: Awaited<ReturnType<typeof api.fsFile>>, force?: boolean) => {
    setDiff((d) => (d?.path === path ? null : d))
    const r = pre ?? (await api.fsFile(dir, path))
    if (r.ok && r.body.content !== undefined) dispatch({ t: "reloaded", path, content: r.body.content, etag: r.body.etag ?? "", force, ...(mixedEol(r.body.content) && { notice: "mixedEol" as const }) })
    else fail(r)
  }
  // force : écraser la version du disque ; create : recréer un fichier supprimé (etag vide).
  // Une sauvegarde à la fois par fichier : la suivante partirait avec l'ancien etag et lèverait un faux conflit contre la nôtre.
  // value : texte du modèle de l'éditeur au moment du Ctrl+S (la dernière frappe n'est peut-être pas encore dans l'état).
  const save = async (path: string, how?: "force" | "create", value?: string) => {
    const tab = live.current.tabs.find((x) => x.path === path)
    if (!tab || tab.readOnly) return
    if (saving.current.has(path)) return void saving.current.set(path, true)
    saving.current.set(path, false)
    // Une relecture partie avant la sauvegarde ne doit pas appliquer ensuite la version d'avant.
    seq.current.set(path, (seq.current.get(path) ?? 0) + 1)
    if (how) setDiff(null)
    let content = value ?? tab.content
    let etag = how === "create" ? "" : tab.etag
    let force = how === "force" || undefined
    let touched = false
    try {
      for (;;) {
        const r = await api.fsSave(dir, path, content, etag, force)
        if (!r.ok) {
          if (r.status !== 409) return fail(r)
          if (r.body.deleted) return dispatch({ t: "disk", path, kind: "deleted" })
          // Décision sur l'onglet tel qu'il est maintenant : une frappe pendant l'aller-retour le rend sale.
          const d = await api.fsFile(dir, path)
          const now = live.current.tabs.find((x) => x.path === path)
          if (!now) return
          const disk = d.ok ? d.body.content : undefined
          // Simple touch ou réécriture identique : on adopte le nouvel etag et on réessaie une fois.
          if (!touched && disk === now.saved && d.body.etag) {
            touched = true
            etag = d.body.etag
            continue
          }
          if (!isDirty(now) && disk !== undefined) return reload(path, d)
          return dispatch({ t: "disk", path, kind: "changed", always: true })
        }
        dispatch({ t: "saved", path, etag: r.body.etag ?? "", content })
        later("git", 1000, git.refresh)
        const next = live.current.tabs.find((x) => x.path === path)
        if (!saving.current.get(path) || !next || next.content === content) return
        saving.current.set(path, false)
        content = next.content
        etag = r.body.etag ?? ""
        force = undefined
      }
    } finally {
      saving.current.delete(path)
    }
  }
  // Relit le disque pour un onglet ouvert après un évènement : l'etag dit si quelque chose a changé (notre propre écho n'en change pas).
  // Pendant une sauvegarde on repousse : l'etag final dira si c'était notre écho ou un vrai changement extérieur.
  const check = async (path: string): Promise<void> => {
    if (saving.current.has(path)) return later("f:" + path, 300, () => check(path))
    const n = (seq.current.get(path) ?? 0) + 1
    seq.current.set(path, n)
    const r = await api.fsFile(dir, path)
    if (seq.current.get(path) !== n) return
    if (saving.current.has(path)) return later("f:" + path, 300, () => check(path))
    const tab = live.current.tabs.find((x) => x.path === path)
    if (!tab) return
    if (r.status === 404) return dispatch({ t: "disk", path, kind: "deleted" })
    if (r.status === 0 || tab.notice === "tooBig" || tab.notice === "binary") return
    // Relecture refusée (fichier devenu trop gros, panne) : l'onglet ne reflète plus le disque, on le dit au lieu de se taire.
    if (!r.ok || r.body.content === undefined) return dispatch({ t: "disk", path, kind: "changed", always: true })
    if (r.body.etag === tab.etag) return
    if (r.body.content === tab.content) return dispatch({ t: "saved", path, etag: r.body.etag ?? "", content: tab.content })
    if (isDirty(tab)) return dispatch({ t: "disk", path, kind: "changed" })
    await reload(path, r)
    reloaded.current.add(path)
    later("toast", 300, () => {
      const names = [...reloaded.current]
      reloaded.current.clear()
      const name = nameOf(names[0])
      toast(names.length === 1 ? t("ide.watch.reloaded", { name }) : t("ide.watch.reloadedMany", { count: names.length }), { id: "ide-reloaded" })
    })
  }
  const onDisk = (e: WatchEvent) => {
    later("index", 1000, index.refetch)
    later("git", 1000, git.refresh)
    if (e.type === "resync") {
      tree.refreshAll()
      for (const x of live.current.tabs) later("f:" + x.path, 150, () => check(x.path))
      return
    }
    const parent = parentOf(e.path)
    if (parent === "" || tree.open.has(parent)) later("d:" + parent, 150, () => tree.refresh(parent))
    for (const x of live.current.tabs) if (x.path === e.path || x.path.startsWith(e.path + "/")) later("f:" + x.path, 150, () => check(x.path))
  }
  useWatch(dir, ["", ...tree.open], onDisk)
  useEffect(() => {
    const all = timers.current
    return () => all.forEach(clearTimeout)
  }, [])

  // Alimentation du service TypeScript : seulement les fichiers de l'index, et seulement ceux qui ont changé depuis.
  useEffect(() => {
    if (index.state === "building") return
    const stamp = new Map(index.files.map((f) => [f.path, `${f.mtime}:${f.size}`]))
    const picked = pickFeed(index.files, live.current.active)
    const keep = new Set(picked)
    const gone = [...fed.current.keys()].filter((p) => !keep.has(p))
    const todo = picked.filter((p) => fed.current.get(p) !== stamp.get(p))
    if (!gone.length && !todo.length) return
    const ac = new AbortController()
    import("./CodeEditor")
      .then(({ setTsLib }) => {
        if (ac.signal.aborted) return
        for (const p of gone) {
          setTsLib(p, null)
          fed.current.delete(p)
        }
        return runFeed(dir, todo, (p, c) => (setTsLib(p, c), fed.current.set(p, stamp.get(p) ?? "")), ac.signal)
      })
      .catch(() => {})
    return () => ac.abort()
  }, [dir, index.files, index.state])
  useEffect(() => {
    const f = fed.current
    return () => {
      if (f.size) import("./CodeEditor").then((m) => m.clearTsLibs(), () => {})
      f.clear()
    }
  }, [dir])

  const showDiff = async (path: string) => {
    const r = await api.fsFile(dir, path)
    if (r.ok && r.body.content !== undefined) setDiff({ path, original: r.body.content })
    else fail(r)
  }
  const close = (path: string) => {
    const tab = tabOf(path)
    const go = () => (dispatch({ t: "close", path }), setDiff((d) => (d?.path === path ? null : d)))
    if (!tab || !isDirty(tab)) return go()
    setConfirm({ title: t("ide.tabs.closeTitle", { name: nameOf(path) }), body: t("ide.tabs.closeBody"), confirm: t("ide.tabs.closeConfirm"), run: go })
  }
  // Une seule confirmation pour tous les onglets modifiés : plusieurs boîtes s'écraseraient l'une l'autre.
  const closeAll = () => {
    const go = () => (ide.tabs.forEach((x) => dispatch({ t: "close", path: x.path })), setDiff(null))
    if (!ide.tabs.some(isDirty)) return go()
    setConfirm({ title: t("ide.tabs.closeAllTitle"), body: t("ide.tabs.closeBody"), confirm: t("ide.tabs.closeConfirm"), run: go })
  }
  const ctx: CmdCtx = {
    can,
    tab: active,
    tabs: ide.tabs,
    readOnly: !!active?.readOnly,
    editor,
    // La bascule part de l'état réellement affiché, pas de la préférence brute (ouverture d'emblée sans choix).
    prefs: { ...prefs, showTerminal: term.open },
    terminalAvailable: term.available,
    treeShown: prefs.showTree,
    newFile: () => treeActions.current?.create("newFile"),
    newFolder: () => treeActions.current?.create("newDir"),
    focusSession: () => setTermFocus((n) => n + 1),
    save: () => void (active && save(active.path)),
    saveAll: async () => {
      for (const x of ide.tabs) if (isDirty(x) && !x.readOnly) await save(x.path)
    },
    closeTab: () => active && close(active.path),
    closeAll,
    openQuick: () => setQuick(true),
  }
  const run = (id: string, k: CmdCtx = ctx) => {
    const c = commands.find((x) => x.id === id)
    if (c?.enabled(k)) c.run(k)
  }
  // Ctrl+S hors de Monaco (arbre, onglets) : jamais la boîte « Enregistrer la page » du navigateur.
  const key = (e: KeyboardEvent) => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey || e.defaultPrevented || e.key.toLowerCase() !== "s") return
    e.preventDefault()
    run("file.save")
  }
  // Ctrl+P sur tout le document : l'espace est la page, et sans focus dedans le navigateur ouvrirait l'impression.
  // Ailleurs dans gld (espace démonté), l'impression reste disponible.
  useEffect(() => {
    const k = (e: globalThis.KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey || e.defaultPrevented || e.key.toLowerCase() !== "p") return
      e.preventDefault()
      setQuick(true)
      document.querySelector<HTMLElement>('[aria-controls="ide-quick-list"]')?.focus()
    }
    document.addEventListener("keydown", k)
    return () => document.removeEventListener("keydown", k)
  }, [])
  // Fermeture de l'ouverture rapide : focus dans l'espace tout de suite, puis dans l'éditeur visé dès qu'il existe.
  const refocus = () => {
    const path = picked.current || live.current.active
    picked.current = ""
    rootEl.current?.focus()
    if (path) setFocusReq((f) => ({ path, n: f.n + 1 }))
  }

  return (
    <div ref={rootEl} tabIndex={-1} className="flex min-h-0 flex-1 outline-none" onKeyDown={key}>
      {prefs.showTree && (
        <>
          <aside className="flex min-h-0 shrink-0 flex-col" style={{ width: w }}>
            <TreeToolbar dir={dir} tree={tree} ctx={ctx} filter={filter} onFilter={setFilter} onRestored={tree.refreshAll} />
            {git.marks.size > 0 && (
              <div className="flex shrink-0 flex-col items-start px-1 pt-1">
                <GitChanges dir={dir} count={git.marks.size} onOpen={open} prefix={git.prefix} />
                {git.more > 0 && <p className="px-2 text-xs text-muted-foreground">{t("ide.git.more", { count: git.more })}</p>}
              </div>
            )}
            <FileTree dir={dir} tree={tree} marks={git.marks} dirs={git.dirs} canWrite={can("fs_write")} selected={ide.active} filter={filter} actionsRef={treeActions} onOpen={open} onRenamed={(from, to) => dispatch({ t: "renamed", from, to })} />
          </aside>
          <Handle label={t("ide.resize.tree")} start={() => w} onMove={(v) => setDrag({ w: clamp(v, 140, 700) })} onEnd={(v) => (setW(String(clamp(v, 140, 700))), setDrag({}))} />
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
                    <div className="min-h-0 flex-1">
                      <CodeEditor
                        path={tab.path}
                        value={tab.content}
                        readOnly={tab.readOnly}
                        theme={theme}
                        wordWrap={prefs.wordWrap}
                        minimap={prefs.minimap}
                        fontSize={prefs.fontSize}
                        onFontSize={(n) => prefs.set("fontSize", n)}
                        editorRef={tab.path === ide.active ? setEditor : undefined}
                        focus={focusReq.path === tab.path ? focusReq.n : 0}
                        onChange={(v) => !tab.readOnly && dispatch({ t: "edit", path: tab.path, content: v })}
                        onSave={(v) => save(tab.path, undefined, v)}
                      />
                    </div>
                  </div>
                )}
              </div>
            ))}
          </Suspense>
          {diff && diff.path === ide.active && active && <ConflictDiff path={diff.path} original={diff.original} modified={active.content} theme={theme} onClose={() => setDiff(null)} />}
        </div>
        <BottomPane dir={dir} prefs={prefs} focusN={termFocus} onView={setTerm} onToggle={(open) => run("view.toggleTerminal", { ...ctx, prefs: { ...prefs, showTerminal: open } })} />
      </div>
      <Confirm ask={confirm} onClose={() => setConfirm(null)} />
      <QuickOpen open={quick} files={index.files.map((f) => f.path)} state={index.state} onClose={() => setQuick(false)} onCloseFocus={refocus} onPick={(p) => ((picked.current = p), setQuick(false), open(p))} />
    </div>
  )
}
