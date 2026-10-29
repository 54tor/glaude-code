import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, File, Folder, Fan } from "lucide-react"
import { cn } from "cn"
import { api, type FsEntry } from "@/lib/api"
import { ApiError } from "@/lib/apiError"

// Dossier du partage en lecture seule (/v1/fs/tree, /v1/fs/entries/content) : le serveur impose le dossier et refuse les fichiers secrets.
type Listing = { entries: FsEntry[]; truncated: boolean } | "loading" | "failed"
const join = (p: string, n: string) => (p ? p + "/" : "") + n

export function GuestFiles({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [lists, setLists] = useState<Record<string, Listing>>({})
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [sel, setSel] = useState("")
  const [view, setView] = useState<{ path: string; text?: string; error?: string } | null>(null)
  const seq = useRef(0)

  const load = useCallback(
    (path: string) => {
      api.fsTree(dir, path).then(
        (r) => setLists((l) => ({ ...l, [path]: r })),
        () => setLists((l) => ({ ...l, [path]: "failed" }))
      )
    },
    [dir]
  )
  useEffect(() => load(""), [load])

  const toggle = (path: string) => {
    const next = new Set(open)
    if (next.delete(path)) return setOpen(next)
    next.add(path)
    setOpen(next)
    if (lists[path] === undefined || lists[path] === "failed") {
      setLists((l) => ({ ...l, [path]: "loading" }))
      load(path)
    }
  }
  const pick = async (path: string) => {
    setSel(path)
    const n = ++seq.current
    setView({ path })
    const r = await api.fsFile(dir, path).then(
      (f) => ({ text: f.content }),
      (e: unknown) => ({ error: e instanceof ApiError ? e.message : t("conversation.guest.fileFailed") })
    )
    if (n === seq.current) setView({ path, ...r })
  }

  const rows = (path: string, depth: number) => {
    const l = lists[path]
    if (l === undefined || l === "loading") return <li className="px-2 py-1 text-muted-foreground" style={{ paddingLeft: 8 + depth * 14 }}><Fan className="size-3.5 animate-spin" aria-hidden /></li>
    if (l === "failed") return <li className="px-2 py-1 text-xs text-destructive" style={{ paddingLeft: 8 + depth * 14 }}>{t("conversation.guest.filesDirFailed")}</li>
    return (
      <>
        {l.entries.length === 0 && path !== "" && <li className="px-2 py-1 text-xs text-muted-foreground" style={{ paddingLeft: 8 + depth * 14 }}>{t("conversation.guest.filesEmpty")}</li>}
        {l.entries.map((e) => {
          const p = join(path, e.name)
          const isDir = e.type === "dir"
          return (
            <li key={p}>
              <button
                type="button"
                disabled={e.type === "symlink"}
                aria-expanded={isDir ? open.has(p) : undefined}
                onClick={() => (isDir ? toggle(p) : void pick(p))}
                style={{ paddingLeft: 8 + depth * 14 }}
                className={cn("flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-sm hover:bg-muted disabled:opacity-50", sel === p && "bg-muted")}
              >
                {isDir ? <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open.has(p) && "rotate-90")} aria-hidden /> : <span className="w-3.5 shrink-0" />}
                {isDir ? <Folder className="size-4 shrink-0" aria-hidden /> : <File className="size-4 shrink-0" aria-hidden />}
                <span className="min-w-0 truncate">{e.name}</span>
              </button>
              {isDir && open.has(p) && <ul>{rows(p, depth + 1)}</ul>}
            </li>
          )
        })}
        {l.truncated && <li className="px-2 py-1 text-xs text-muted-foreground" style={{ paddingLeft: 8 + depth * 14 }}>{t("conversation.guest.filesTruncated")}</li>}
      </>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <ul aria-label={t("conversation.guest.files")} className="max-h-[40%] shrink-0 overflow-auto border-b p-2 md:max-h-none md:w-72 md:border-r md:border-b-0">{rows("", 0)}</ul>
      <div className="min-h-0 min-w-0 flex-1 overflow-auto p-3">
        {!view && <p className="text-sm text-muted-foreground">{t("conversation.guest.filePick")}</p>}
        {view && <p className="mb-2 truncate text-xs text-muted-foreground">{view.path}</p>}
        {view?.error && <p className="text-sm text-destructive">{view.error}</p>}
        {view && view.text === undefined && !view.error && <Fan className="size-4 animate-spin" aria-hidden />}
        {view?.text !== undefined && <pre className="session-text text-xs whitespace-pre-wrap break-words">{view.text}</pre>}
      </div>
    </div>
  )
}
