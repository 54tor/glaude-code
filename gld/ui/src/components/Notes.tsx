import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, MessageSquarePlus, NotebookPen, Send } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { useOpen } from "@/hooks/useOpen"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions, type Row } from "./actions"

// Bloc-notes d'un projet ou d'un dossier, enregistré tout seul. Le texte peut partir vers une nouvelle session (dans folder) ou vers
// une session ouverte dans tmux parmi celles de la vue.
export function Notes({ noteKey, folder, rows }: { noteKey: string; folder?: string; rows: Row[] }) {
  const { t: tr } = useTranslation()
  const a = useActions()
  const [text, setText] = useState<string | null>(null) // null : pas encore lu
  const [open, flip] = useOpen("n/" + noteKey)
  const [sendable, setSendable] = useState<Set<string>>(new Set())
  const saved = useRef("")
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const latest = useRef({ text: "", key: noteKey })

  useEffect(() => {
    let live = true
    api.notes().then(
      (r) => {
        if (!live) return
        const t = r.notes[noteKey] ?? ""
        saved.current = t
        setText(t)
      },
      () => live && setText("")
    )
    return () => {
      live = false
    }
  }, [noteKey])

  const flush = () => {
    clearTimeout(timer.current)
    const { text: t, key } = latest.current
    if (t === saved.current) return
    saved.current = t
    api.post("/notes", { key, text: t }).then((r) => r.ok || toast.error(r.out))
  }
  useEffect(() => flush, []) // enregistre ce qui reste en quittant la vue

  const edit = (t: string) => {
    setText(t)
    latest.current = { text: t, key: noteKey }
    clearTimeout(timer.current)
    timer.current = setTimeout(flush, 700)
  }
  if (text === null) return null
  const body = text.trim()
  const targets = rows.filter((r) => sendable.has(r.s.id) && a.live.has(r.s.id))
  const sendTo = async (r: Row) => {
    const res = await api.post("/session-send", { id: r.s.id, text: body }, 20_000)
    if (res.ok) toast.success(tr("conversation.notes.sent", { title: r.s.title || r.s.id.slice(0, 8) }))
    else toast.error(res.out)
  }
  return (
    <div className="mb-4 rounded-lg border">
      <button onClick={flip} aria-expanded={open} className="flex w-full items-center gap-2 px-3 py-2 text-sm font-medium">
        <ChevronRight className={cn("size-4 transition-transform", open && "rotate-90")} />
        <NotebookPen className="size-4 text-muted-foreground" />
        {tr("conversation.notes.title")}
        {body && !open && <span className="min-w-0 truncate text-xs font-normal text-muted-foreground">{body.split("\n")[0]}</span>}
      </button>
      {open && (
        <div className="space-y-2 border-t p-3">
          <textarea
            value={text}
            onChange={(e) => edit(e.target.value)}
            onBlur={flush}
            maxLength={65000}
            rows={4}
            placeholder={tr("conversation.notes.placeholder")}
            aria-label={tr("conversation.notes.title")}
            className="session-text field-sizing-content max-h-96 min-h-24 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
          <div className="flex flex-wrap items-center gap-2">
            {folder && (
              <Button size="sm" variant="outline" disabled={!body} onClick={() => a.newSession(folder, body)}>
                <MessageSquarePlus /> {tr("conversation.notes.newSession")}
              </Button>
            )}
            <DropdownMenu onOpenChange={(o) => o && api.sendInfo().then((r) => setSendable(new Set(r.ids)), () => {})}>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="outline" disabled={!body}>
                  <Send /> {tr("conversation.notes.sendOpen")}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="max-h-72 w-80 overflow-y-auto">
                <DropdownMenuLabel className="whitespace-normal">{tr("conversation.notes.sendLabel")}</DropdownMenuLabel>
                {targets.length === 0 && <DropdownMenuItem disabled>{tr("conversation.notes.none")}</DropdownMenuItem>}
                {targets.map((r) => (
                  <DropdownMenuItem key={r.s.id} onSelect={() => void sendTo(r)}>
                    <span className="truncate">{r.s.title || r.s.id.slice(0, 8)}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">{tr("conversation.notes.chars", { n: text.length })}</span>
          </div>
        </div>
      )}
    </div>
  )
}
