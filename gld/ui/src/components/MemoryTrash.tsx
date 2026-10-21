import { useEffect, useState } from "react"
import { ArchiveRestore, FileText, Trash, Trash2 } from "lucide-react"
import { api, type Trashed } from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DiffView } from "./MemoryDiff"

// Corbeille dans la colonne de gauche : les versions supprimées ou remplacées.
export function TrashList({ trash, current, onPick, onPurgeAll }: { trash: Trashed[]; current: string; onPick: (k: string) => void; onPurgeAll: () => void }) {
  return (
    <section className="mt-4 border-t pt-2">
      <div className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground">
        <Trash className="size-3.5" /> <span className="flex-1">Corbeille</span>
        <Button variant="ghost" size="xs" className="h-5 px-1.5 text-xs" onClick={onPurgeAll}>
          Vider
        </Button>
      </div>
      <ul className="space-y-px">
        {trash.map((t) => (
          <li key={t.id}>
            <button
              onClick={() => onPick("corbeille/" + t.id)}
              title={t.path}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                t.id === current ? "bg-accent font-medium" : "text-muted-foreground hover:bg-muted hover:text-foreground"
              )}
            >
              <FileText className="size-3.5 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{t.path.split(/[\\/]/).pop()}</span>
              <span className="shrink-0 text-xs font-normal">
                {t.reason} {ago(t.at)}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

// Une version de la corbeille : son texte, ou ses différences avec le fichier actuel (s'il existe encore).
export function TrashViewer({
  trashed,
  text,
  homeDir,
  exists,
  onPurge,
  onRestore,
}: {
  trashed: Trashed | undefined
  text: string | null
  homeDir: string
  exists: boolean // le fichier d'origine existe encore : on peut comparer
  onPurge: () => void
  onRestore: () => void
}) {
  const [diff, setDiff] = useState(false)
  const [current, setCurrent] = useState<string | null>(null)
  const key = trashed?.key
  useEffect(() => {
    if (!diff || !key) return
    let live = true
    api.memoryFile(key).then((r) => live && setCurrent(r.text ?? ""))
    return () => {
      live = false
    }
  }, [diff, key])
  const shortPath = (p: string) => (homeDir && p.startsWith(homeDir) ? "~" + p.slice(homeDir.length) : p)
  return (
    <>
      <div className="flex items-center gap-2 border-b px-6 py-2">
        <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={trashed?.path}>
          {trashed ? `${shortPath(trashed.path)} · ${trashed.reason} ${ago(trashed.at)}` : "Introuvable dans la corbeille"}
        </code>
        {trashed && (
          <>
            {exists && (
              <Button variant={diff ? "secondary" : "ghost"} size="sm" onClick={() => setDiff(!diff)} aria-pressed={diff}>
                Comparer à l'actuelle
              </Button>
            )}
            <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onPurge}>
              <Trash2 /> Supprimer définitivement
            </Button>
            <Button size="sm" onClick={onRestore}>
              <ArchiveRestore /> Restaurer
            </Button>
          </>
        )}
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {diff && current !== null ? (
          <DiffView old={text ?? ""} cur={current} />
        ) : (
          <pre className="session-text mx-auto max-w-7xl px-6 py-6 whitespace-pre-wrap">{text ?? ""}</pre>
        )}
      </ScrollArea>
    </>
  )
}
