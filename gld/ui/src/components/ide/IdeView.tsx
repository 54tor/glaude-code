import { lazy, Suspense, useEffect, useReducer, useRef, useState, useSyncExternalStore, type KeyboardEvent, type PointerEvent } from "react"
import { useTranslation } from "react-i18next"
import { PanelBottom } from "lucide-react"
import { toast } from "sonner"
import { cn } from "cn"
import { api, type FsRes } from "@/lib/api"
import { useCan } from "@/lib/auth"
import { useLocal } from "@/hooks/useLocal"
import { Button } from "@/components/ui/button"
import { Confirm, type Ask } from "@/components/Confirm"
import { FileTree } from "./FileTree"
import { Tabs } from "./Tabs"
import { ConflictBar } from "./ConflictBar"
import { ConflictDiff } from "./ConflictDiff"
import { QuickOpen } from "./QuickOpen"
import { GitChanges } from "../GitChanges"
import { TerminalPane } from "./TerminalPane"
import { useGitMarks } from "./useGitMarks"
import { isDirty, reduce } from "./ideState"
import { useTree } from "./useTree"
import { useIndex } from "./useIndex"
import { useWatch, type WatchEvent } from "./useWatch"
import { pickFeed, runFeed } from "./feedTs"

const CodeEditor = lazy(() => import("./CodeEditor"))

// Monaco ramène des fins de ligne mixtes (ou un CR isolé) à la majoritaire : enregistrer réécrirait tout le fichier.
const mixedEol = (c: string) => /\r(?!\n)/.test(c) || (c.includes("\r\n") && /(^|[^\r])\n/.test(c))

// Le fournisseur de thème de gld pose la classe sur <html> : on la suit, « système » compris.
const watchTheme = (f: () => void) => {
  const o = new MutationObserver(f)
  o.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
  return () => o.disconnect()
}
const useResolvedTheme = () => useSyncExternalStore(watchTheme, () => (document.documentElement.classList.contains("dark") ? "dark" : "light"))

const parentOf = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf("/")))

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

// Poignée de redimensionnement aux événements pointer : onMove pendant le glissement, onEnd au relâchement.
function Handle({ label, vertical, start, onMove, onEnd }: { label: string; vertical?: boolean; start: () => number; onMove: (v: number) => void; onEnd: (v: number) => void }) {
  const from = useRef<{ at: number; v: number } | null>(null)
  const pos = (e: PointerEvent) => (vertical ? e.clientY : e.clientX)
  const next = (e: PointerEvent) => from.current!.v + (vertical ? from.current!.at - pos(e) : pos(e) - from.current!.at)
  return (
    <div
      role="separator"
      aria-orientation={vertical ? "horizontal" : "vertical"}
      aria-label={label}
      className={vertical ? "h-1 shrink-0 cursor-row-resize bg-border hover:bg-ring/50" : "w-1 shrink-0 cursor-col-resize bg-border hover:bg-ring/50"}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        from.current = { at: pos(e), v: start() }
      }}
      onPointerMove={(e) => from.current && onMove(next(e))}
      onPointerUp={(e) => {
        if (from.current) onEnd(next(e))
        from.current = null
      }}
    />
  )
}

// Espace de travail d'un dossier : arborescence, zone centrale, panneau du bas repliable.
export function IdeView({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  const [wStr, setW] = useLocal(`gld.ide.${dir}.w`, "260")
  const [hStr, setH] = useLocal(`gld.ide.${dir}.h`, "160")
  const [bottom, setBottom] = useLocal(`gld.ide.${dir}.bottom`, "1")
  const [drag, setDrag] = useState<{ w?: number; h?: number }>({})
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
  const tree = useTree(dir)
  const index = useIndex(dir)
  const git = useGitMarks(dir)
  const [quick, setQuick] = useState(false)
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const later = (k: string, ms: number, f: () => void) => {
    clearTimeout(timers.current.get(k))
    timers.current.set(k, setTimeout(() => (timers.current.delete(k), f()), ms))
  }
  const reloaded = useRef(new Set<string>())
  const seq = useRef(new Map<string, number>()) // chemin → numéro de la dernière lecture lancée
  const fed = useRef(new Map<string, number>()) // chemin → mtime de l'index au moment de l'alimentation
  const w = drag.w ?? clamp(Number(wStr) || 260, 140, 700)
  const h = drag.h ?? clamp(Number(hStr) || 160, 60, 600)

  const tabOf = (p: string) => ide.tabs.find((x) => x.path === p)
  const active = tabOf(ide.active)
  const fail = (r: FsRes) => toast.error(r.body.error || t("shell.act.failed"))

  const open = async (path: string) => {
    if (tabOf(path)) return dispatch({ t: "focus", path })
    const r = await api.fsFile(dir, path)
    if (r.status === 413 || r.status === 415)
      return dispatch({ t: "open", tab: { path, content: "", saved: "", etag: "", readOnly: true, notice: r.status === 413 ? "tooBig" : "binary" } })
    if (!r.ok || r.body.content === undefined) return fail(r)
    const c = r.body.content
    const mixed = mixedEol(c)
    dispatch({ t: "open", tab: { path, content: c, saved: c, etag: r.body.etag ?? "", readOnly: mixed || !can("fs_write"), ...(mixed && { notice: "mixedEol" as const }) } })
  }
  const reload = async (path: string, pre?: Awaited<ReturnType<typeof api.fsFile>>) => {
    setDiff((d) => (d?.path === path ? null : d))
    const r = pre ?? (await api.fsFile(dir, path))
    if (r.ok && r.body.content !== undefined) dispatch({ t: "reloaded", path, content: r.body.content, etag: r.body.etag ?? "", ...(mixedEol(r.body.content) && { notice: "mixedEol" as const }) })
    else fail(r)
  }
  // force : écraser la version du disque ; create : recréer un fichier supprimé (etag vide).
  // Une sauvegarde à la fois par fichier : la suivante partirait avec l'ancien etag et lèverait un faux conflit contre la nôtre.
  const save = async (path: string, how?: "force" | "create") => {
    const tab = live.current.tabs.find((x) => x.path === path)
    if (!tab || tab.readOnly) return
    if (saving.current.has(path)) return void saving.current.set(path, true)
    saving.current.set(path, false)
    if (how) setDiff(null)
    let content = tab.content
    let etag = how === "create" ? "" : tab.etag
    let force = how === "force" || undefined
    try {
      for (;;) {
        const r = await api.fsSave(dir, path, content, etag, force)
        if (!r.ok) {
          if (r.status !== 409) return fail(r)
          if (!r.body.deleted && !isDirty(tab)) return reload(path)
          return dispatch({ t: "disk", path, kind: r.body.deleted ? "deleted" : "changed" })
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
    if (!r.ok || r.body.content === undefined || tab.notice === "tooBig" || tab.notice === "binary" || r.body.etag === tab.etag) return
    if (r.body.content === tab.content) return dispatch({ t: "saved", path, etag: r.body.etag ?? "", content: tab.content })
    if (isDirty(tab)) return dispatch({ t: "disk", path, kind: "changed" })
    await reload(path, r)
    reloaded.current.add(path)
    later("toast", 300, () => {
      const names = [...reloaded.current]
      reloaded.current.clear()
      const name = names[0].slice(names[0].lastIndexOf("/") + 1)
      toast(names.length === 1 ? t("ide.watch.reloaded", { name }) : t("ide.watch.reloadedMany", { count: names.length }), { id: "ide-reloaded" })
    })
  }
  const onDisk = (e: WatchEvent) => {
    later("index", 1000, index.refetch)
    later("git", 1000, git.refresh)
    if (e.type === "resync") {
      tree.refresh("")
      for (const d of tree.open) tree.refresh(d)
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
    const mtime = new Map(index.files.map((f) => [f.path, f.mtime]))
    const picked = pickFeed(index.files, live.current.active)
    const keep = new Set(picked)
    const gone = [...fed.current.keys()].filter((p) => !keep.has(p))
    const todo = picked.filter((p) => fed.current.get(p) !== mtime.get(p))
    if (!gone.length && !todo.length) return
    const ac = new AbortController()
    import("./CodeEditor").then(({ setTsLib }) => {
      if (ac.signal.aborted) return
      for (const p of gone) {
        setTsLib(p, null)
        fed.current.delete(p)
      }
      return runFeed(dir, todo, (p, c) => (setTsLib(p, c), fed.current.set(p, mtime.get(p) ?? 0)), ac.signal)
    })
    return () => ac.abort()
  }, [dir, index.files, index.state])
  useEffect(() => {
    const f = fed.current
    return () => {
      if (f.size) import("./CodeEditor").then((m) => m.clearTsLibs())
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
    setConfirm({ title: t("ide.tabs.closeTitle", { name: path.slice(path.lastIndexOf("/") + 1) }), body: t("ide.tabs.closeBody"), confirm: t("ide.tabs.closeConfirm"), run: go })
  }
  // Ctrl+S hors de Monaco (arbre, onglets) : jamais la boîte « Enregistrer la page » du navigateur.
  // Ctrl+P n'est pris qu'ici : ailleurs dans gld, l'impression du navigateur reste disponible.
  const key = (e: KeyboardEvent) => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey || e.defaultPrevented) return
    const k = e.key.toLowerCase()
    if (k === "p") return e.preventDefault(), setQuick(true)
    if (k !== "s") return
    e.preventDefault()
    if (ide.active) save(ide.active)
  }

  if (!can("fs_read")) return <p className="p-6 text-sm text-muted-foreground">{t("ide.denied")}</p>
  return (
    <div className="flex min-h-0 flex-1" onKeyDown={key}>
      <aside className="flex min-h-0 shrink-0 flex-col" style={{ width: w }}>
        {git.marks.size > 0 && (
          <div className="flex shrink-0 flex-col items-start px-1 pt-1">
            <GitChanges dir={dir} count={git.marks.size} onOpen={open} prefix={git.prefix} />
            {git.more > 0 && <p className="px-2 text-xs text-muted-foreground">{t("ide.git.more", { count: git.more })}</p>}
          </div>
        )}
        <FileTree dir={dir} tree={tree} marks={git.marks} dirs={git.dirs} canWrite={can("fs_write")} selected={ide.active} onOpen={open} onRenamed={(from, to) => dispatch({ t: "renamed", from, to })} />
      </aside>
      <Handle label={t("ide.resize.tree")} start={() => w} onMove={(v) => setDrag({ w: clamp(v, 140, 700) })} onEnd={(v) => (setW(String(clamp(v, 140, 700))), setDrag({}))} />
      <div className="flex min-w-0 flex-1 flex-col">
        {ide.tabs.length > 0 && <Tabs tabs={ide.tabs} active={ide.active} onFocus={(path) => dispatch({ t: "focus", path })} onClose={close} />}
        {active?.conflict && (
          <ConflictBar
            kind={active.conflict}
            onReload={() => reload(active.path)}
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
                      <CodeEditor path={tab.path} value={tab.content} readOnly={tab.readOnly} theme={theme} onChange={(v) => !tab.readOnly && dispatch({ t: "edit", path: tab.path, content: v })} onSave={() => save(tab.path)} />
                    </div>
                  </div>
                )}
              </div>
            ))}
          </Suspense>
          {diff && diff.path === ide.active && active && <ConflictDiff path={diff.path} original={diff.original} modified={active.content} theme={theme} onClose={() => setDiff(null)} />}
        </div>
        {can("exec") && (
          <div className="flex justify-end border-t px-2 py-0.5">
            <Button variant="ghost" size="icon-sm" onClick={() => setBottom(bottom ? "" : "1")} aria-pressed={!!bottom} aria-label={t(bottom ? "ide.bottom.hide" : "ide.bottom.show")}>
              <PanelBottom />
            </Button>
          </div>
        )}
        {bottom && can("exec") && (
          <>
            <Handle label={t("ide.resize.bottom")} vertical start={() => h} onMove={(v) => setDrag({ h: clamp(v, 60, 600) })} onEnd={(v) => (setH(String(clamp(v, 60, 600))), setDrag({}))} />
            <div className="flex shrink-0 flex-col overflow-auto" style={{ height: h }}>
              <TerminalPane dir={dir} />
            </div>
          </>
        )}
      </div>
      <Confirm ask={confirm} onClose={() => setConfirm(null)} />
      <QuickOpen open={quick} files={index.files.map((f) => f.path)} state={index.state} onClose={() => setQuick(false)} onPick={(p) => (setQuick(false), open(p))} />
    </div>
  )
}
