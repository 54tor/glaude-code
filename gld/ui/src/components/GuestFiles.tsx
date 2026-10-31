import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, File, Folder, Fan } from "lucide-react"
import { cn } from "cn"
import { api, type FsEntry } from "@/lib/api"
import { ApiError } from "@/lib/apiError"

// Dossier du partage en lecture seule (/v1/fs/tree, /v1/fs/entries/content) : le serveur impose le dossier et ne montre que les fichiers cités par la session.
type Listing = { entries: FsEntry[]; truncated: boolean } | "loading" | "failed"

const BASE_INDENT_PX = 8
const INDENT_PER_DEPTH_PX = 14

const joinPath = (parentPath: string, entryName: string) => (parentPath ? parentPath + "/" : "") + entryName
const indentStyle = (depth: number) => ({ paddingLeft: BASE_INDENT_PX + depth * INDENT_PER_DEPTH_PX })

export function GuestFiles({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [listings, setListings] = useState<Record<string, Listing>>({})
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set())
  const [selectedPath, setSelectedPath] = useState("")
  const [view, setView] = useState<{ path: string; text?: string; error?: string } | null>(null)
  const fileRequestCounter = useRef(0)

  const loadListing = useCallback(
    (path: string) => {
      api.fsTree(dir, path).then(
        (listing) => setListings((previousListings) => ({ ...previousListings, [path]: listing })),
        () => setListings((previousListings) => ({ ...previousListings, [path]: "failed" }))
      )
    },
    [dir]
  )
  useEffect(() => loadListing(""), [loadListing])

  const toggleDirectory = (path: string) => {
    const nextExpandedPaths = new Set(expandedPaths)
    if (nextExpandedPaths.delete(path)) return setExpandedPaths(nextExpandedPaths)
    nextExpandedPaths.add(path)
    setExpandedPaths(nextExpandedPaths)
    const needsLoading = listings[path] === undefined || listings[path] === "failed"
    if (needsLoading) {
      setListings((previousListings) => ({ ...previousListings, [path]: "loading" }))
      loadListing(path)
    }
  }
  const selectFile = async (path: string) => {
    setSelectedPath(path)
    const requestNumber = ++fileRequestCounter.current
    setView({ path })
    const fileResult = await api.fsFile(dir, path).then(
      (file) => ({ text: file.content }),
      (fileError: unknown) => ({ error: fileError instanceof ApiError ? fileError.message : t("conversation.guest.fileFailed") })
    )
    const isLatestRequest = requestNumber === fileRequestCounter.current
    if (isLatestRequest) setView({ path, ...fileResult })
  }

  const renderRows = (path: string, depth: number) => {
    const listing = listings[path]
    if (listing === undefined || listing === "loading") return <li className="px-2 py-1 text-muted-foreground" style={indentStyle(depth)}><Fan className="size-3.5 animate-spin" aria-hidden /></li>
    if (listing === "failed") return <li className="px-2 py-1 text-xs text-destructive" style={indentStyle(depth)}>{t("conversation.guest.filesDirFailed")}</li>
    const isEmptySubdirectory = listing.entries.length === 0 && path !== ""
    return (
      <>
        {isEmptySubdirectory && <li className="px-2 py-1 text-xs text-muted-foreground" style={indentStyle(depth)}>{t("conversation.guest.filesEmpty")}</li>}
        {listing.entries.map((entry) => {
          const entryPath = joinPath(path, entry.name)
          const isDir = entry.type === "dir"
          const isExpanded = expandedPaths.has(entryPath)
          return (
            <li key={entryPath}>
              <button
                type="button"
                disabled={entry.type === "symlink"}
                aria-expanded={isDir ? isExpanded : undefined}
                onClick={() => (isDir ? toggleDirectory(entryPath) : void selectFile(entryPath))}
                style={indentStyle(depth)}
                className={cn("flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-sm hover:bg-muted disabled:opacity-50", selectedPath === entryPath && "bg-muted")}
              >
                {isDir ? <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", isExpanded && "rotate-90")} aria-hidden /> : <span className="w-3.5 shrink-0" />}
                {isDir ? <Folder className="size-4 shrink-0" aria-hidden /> : <File className="size-4 shrink-0" aria-hidden />}
                <span className="min-w-0 truncate">{entry.name}</span>
              </button>
              {isDir && isExpanded && <ul>{renderRows(entryPath, depth + 1)}</ul>}
            </li>
          )
        })}
        {listing.truncated && <li className="px-2 py-1 text-xs text-muted-foreground" style={indentStyle(depth)}>{t("conversation.guest.filesTruncated")}</li>}
      </>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <ul aria-label={t("conversation.guest.files")} className="max-h-[40%] shrink-0 overflow-auto border-b p-2 md:max-h-none md:w-72 md:border-r md:border-b-0">{renderRows("", 0)}</ul>
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
