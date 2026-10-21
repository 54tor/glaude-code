import { useEffect, useState } from "react"
import { Check, Copy, FileText, MessageSquarePlus } from "lucide-react"
import { toast } from "sonner"
import { api, type Snippet } from "@/lib/api"
import { ago, size } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Markdown } from "./Conversation"

// Bibliothèque de snippets (dossier GLD_WEB_SNIPPETS) : liste à gauche, contenu à droite, copie en un clic.
export function Snippets({
  files,
  dir,
  path,
  q,
  onPick,
  onLaunch,
}: {
  files: Snippet[]
  dir: string
  path: string
  q: string
  onPick: (p: string) => void
  onLaunch: (path: string, text: string) => void
}) {
  const f = q.toLowerCase()
  const shown = files.filter((s) => !f || s.path.toLowerCase().includes(f))
  const [loaded, setLoaded] = useState<{ path: string; text: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const text = loaded?.path === path ? loaded.text : null

  useEffect(() => {
    if (!path) return
    let live = true
    api.snippet(path).then((r) => live && setLoaded({ path, text: r.text ?? r.error ?? "" }))
    return () => {
      live = false
    }
  }, [path])

  const copy = () =>
    text !== null &&
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      },
      () => toast.error("Copie impossible")
    )

  if (files.length === 0) return <p className="px-6 py-20 text-center text-sm text-muted-foreground">Aucun fichier dans {dir}.</p>

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <ScrollArea className="max-h-64 shrink-0 border-b md:max-h-none md:w-72 md:border-r md:border-b-0">
        <ul className="space-y-px p-2">
          {shown.map((s) => {
            const i = s.path.lastIndexOf("/")
            return (
              <li key={s.path}>
                <button
                  onClick={() => onPick(s.path)}
                  className={cn(
                    "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
                    s.path === path ? "bg-accent font-medium" : "hover:bg-muted"
                  )}
                >
                  <FileText className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0">
                    <span className="block truncate">{s.path.slice(i + 1)}</span>
                    <span className="block truncate text-xs font-normal text-muted-foreground">
                      {i > 0 ? s.path.slice(0, i) + " · " : ""}
                      {ago(s.mtime)} · {size(s.kb)}
                    </span>
                  </span>
                </button>
              </li>
            )
          })}
          {shown.length === 0 && <li className="px-2 py-2 text-xs text-muted-foreground">Aucun snippet ne correspond.</li>}
        </ul>
      </ScrollArea>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!path ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">Choisis un snippet à afficher.</p>
        ) : (
          <>
            <div className="flex items-center gap-2 border-b px-6 py-2">
              <code className="flex-1 truncate text-xs text-muted-foreground">{path}</code>
              <Button variant="outline" size="sm" onClick={copy} disabled={text === null}>
                {copied ? <Check /> : <Copy />} {copied ? "Copié" : "Copier"}
              </Button>
              <Button size="sm" onClick={() => text !== null && onLaunch(path, text)} disabled={text === null}>
                <MessageSquarePlus /> Nouvelle session avec ce snippet
              </Button>
            </div>
            <ScrollArea className="min-h-0 flex-1">
              <div className="mx-auto max-w-7xl px-6 py-6 text-sm leading-relaxed">
                {text === null ? (
                  <Skeleton className="h-32 w-full" />
                ) : path.endsWith(".md") ? (
                  <Markdown t={text} />
                ) : (
                  <pre className="whitespace-pre-wrap">{text}</pre>
                )}
              </div>
            </ScrollArea>
          </>
        )}
      </div>
    </div>
  )
}
