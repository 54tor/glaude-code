import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Check, ChevronRight, Fan, FolderOpen, Undo2, MessageSquarePlus, NotebookPen, Plus, Send, X } from "lucide-react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { setOpen, useOpen } from "@/hooks/useOpen"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions, type Row } from "./actions"

// Une note = une liste d'items, enregistrée en JSON ; un ancien texte libre devient un item par ligne non vide.
export const notesOf = (raw: string): string[] => parse(raw)
const parse = (raw: string): string[] => {
  try {
    const v: unknown = JSON.parse(raw)
    if (Array.isArray(v)) return v.filter((x): x is string => typeof x === "string")
  } catch {
    /* texte libre */
  }
  return raw.split("\n").filter((l) => l.trim())
}
const pack = (items: string[]) => (items.some((i) => i.trim()) ? JSON.stringify(items) : "")

// Bloc-notes d'un projet ou d'un dossier, enregistré tout seul. Le texte peut partir vers une nouvelle session (dans folder) ou vers
// une session ouverte dans tmux parmi celles de la vue.
export function Notes({ noteKey, folder: base, rows }: { noteKey: string; folder?: string; rows: Row[] }) {
  const { t: tr } = useTranslation()
  const a = useActions()
  const dirKey = "gld.notes.dir/" + noteKey
  const [custom, setCustom] = useState(() => {
    try {
      return localStorage.getItem(dirKey) ?? ""
    } catch {
      return ""
    }
  })
  const folder = custom || base
  const setFolder = (d: string) => {
    setCustom(d)
    try {
      if (d) localStorage.setItem(dirKey, d)
      else localStorage.removeItem(dirKey)
    } catch {
      /* préférence locale seulement */
    }
  }
  const [items, setItems] = useState<string[] | null>(null) // null : pas encore lu
  const [open, flip] = useOpen("n/" + noteKey, true)
  const [sendable, setSendable] = useState<Set<string>>(new Set())
  const saved = useRef("")
  const [savedList, setSavedList] = useState<string[]>([]) // ce que le serveur a : une ligne différente est encore en attente
  const [picked, setPicked] = useState<Set<number>>(new Set()) // lignes cochées ; aucune cochée = tout part
  const fields = useRef<(HTMLTextAreaElement | null)[]>([])
  const focusNext = useRef<number | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const latest = useRef({ text: "", key: noteKey })

  useEffect(() => {
    let live = true
    api.notes().then(
      (r) => {
        if (!live) return
        const t = r.notes[noteKey] ?? ""
        saved.current = pack(parse(t))
        latest.current = { text: saved.current, key: noteKey } // sinon le flush de sortie croit la note vidée et la supprime
        setSavedList(parse(t))
        setItems(parse(t))
      },
      () => live && setItems([])
    )
    return () => {
      live = false
    }
  }, [noteKey])

  const flush = () => {
    clearTimeout(timer.current)
    const { text: t, key } = latest.current
    if (t === saved.current) return
    const before = saved.current
    saved.current = t
    api.notesSave({ key, text: t }).then(
      () => (window.dispatchEvent(new Event("gld:notes")), setSavedList(parse(t))),
      (e) => {
        saved.current = before
        toast.error(errMsg(e))
      }
    )
  }
  useEffect(() => flush, []) // enregistre ce qui reste en quittant la vue
  const addRef = useRef<() => void>(() => {})
  useEffect(() => {
    const f = () => addRef.current()
    window.addEventListener("gld:note-add", f)
    return () => window.removeEventListener("gld:note-add", f)
  }, [])

  const edit = (next: string[]) => {
    setItems(next)
    latest.current = { text: pack(next), key: noteKey }
    clearTimeout(timer.current)
    timer.current = setTimeout(flush, 700)
  }
  useEffect(() => {
    addRef.current = () => {
      if (items === null) return
      setOpen("n/" + noteKey, true)
      focusNext.current = items.length
      edit([...items, ""])
    }
  })
  useEffect(() => {
    if (focusNext.current !== null) fields.current[focusNext.current]?.focus()
    focusNext.current = null
  })
  if (items === null) return null
  const chosen = items.map((it, i) => ({ it: it.trim(), i })).filter((x) => x.it && (picked.size === 0 || picked.has(x.i)))
  const body = chosen.map((x) => "- " + x.it).join("\n")
  const count = items.filter((i) => i.trim()).length
  const togglePick = (i: number) => setPicked((p) => (p.has(i) ? new Set([...p].filter((x) => x !== i)) : new Set(p).add(i)))
  const dropIdx = new Set(chosen.map((x) => x.i))
  const here = folder ? rows.filter((r) => r.s.cwd === folder) : rows
  const targets = here.filter((r) => sendable.has(r.s.id) && a.live.has(r.s.id))
  const closed = here.filter((r) => !a.live.has(r.s.id)).slice(0, 30)
  const filled = items.map((x, i) => (x.trim() ? i : -1)).filter((i) => i >= 0)
  const allPicked = filled.length > 0 && filled.every((i) => picked.has(i))
  const sendTo = async (r: Row) => {
    const live = a.live.has(r.s.id)
    let sent = false
    try {
      if (live) await api.sessionSend(r.s.id, body, 20_000)
      sent = live || (await a.resumeSend(r, body))
    } catch (e) {
      toast.error(errMsg(e))
    }
    if (sent) {
      toast.success(tr("conversation.notes.sent", { title: r.s.title || r.s.id.slice(0, 8) }))
      edit(items.filter((_, j) => !dropIdx.has(j)))
      setPicked(new Set())
      flush()
    }
  }
  return (
    <div className="mb-4 rounded-lg border">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button onClick={flip} aria-expanded={open} className="flex w-full items-center gap-2 px-3 py-2 text-sm font-medium">
            <ChevronRight className={cn("size-4 transition-transform", open && "rotate-90")} />
            <NotebookPen className="size-4 text-muted-foreground" />
            {tr("conversation.notes.title")}
            {count > 0 && <span className="text-xs font-normal text-muted-foreground">{count}</span>}
            {custom && <span className="truncate text-xs font-normal text-muted-foreground" title={custom}>{custom}</span>}
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={() => void a.pickDir(tr("conversation.notes.folderTitle"), folder ?? "").then((d) => d && setFolder(d))}>
            <FolderOpen /> {tr("conversation.notes.changeFolder")}
          </ContextMenuItem>
          {custom && (
            <ContextMenuItem onSelect={() => setFolder("")}>
              <Undo2 /> {tr("conversation.notes.resetFolder")}
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>
      {open && (
        <div className="space-y-2 border-t p-3">
          {filled.length > 1 && (
            <label className="flex w-fit cursor-pointer items-center gap-1.5 px-2 text-xs text-muted-foreground">
              <Checkbox checked={allPicked} onCheckedChange={() => setPicked(allPicked ? new Set() : new Set(filled))} />
              {tr("conversation.notes.selectAll")}
            </label>
          )}
          <ul className="space-y-1.5">
            {items.map((it, i) => (
              <li key={i} className="flex items-start gap-1.5 rounded-lg border bg-background px-2 py-1">
                <Checkbox checked={picked.has(i)} onCheckedChange={() => togglePick(i)} aria-label={tr("conversation.notes.pick")} className="mt-2.5 shrink-0" />
                <textarea
                  ref={(el) => void (fields.current[i] = el)}
                  value={it}
                  onKeyDown={(e) => {
                    if (e.key !== "Enter" || !(e.ctrlKey || e.metaKey)) return
                    e.preventDefault()
                    if (!it.trim()) return
                    flush()
                    if (i === items.length - 1 || items[i + 1].trim()) edit([...items.slice(0, i + 1), "", ...items.slice(i + 1)])
                    focusNext.current = i + 1
                  }}
                  onChange={(e) => edit(items.map((x, j) => (j === i ? e.target.value : x)))}
                  onBlur={flush}
                  maxLength={4000}
                  rows={1}
                  aria-label={tr("conversation.notes.item")}
                  className="session-text field-sizing-content max-h-48 min-w-0 flex-1 resize-none bg-transparent py-1 outline-none"
                />
                {it.trim() &&
                  (savedList[i] === it ? (
                    <Check className="mt-1.5 size-3.5 shrink-0 text-success" aria-label={tr("conversation.notes.saved")} />
                  ) : (
                    <Fan className="mt-1.5 size-3.5 shrink-0 animate-spin text-muted-foreground" aria-label={tr("conversation.notes.saving")} />
                  ))}
                <Button variant="ghost" size="icon-xs" onClick={() => { setPicked(new Set()); edit(items.filter((_, j) => j !== i)) }} aria-label={tr("conversation.notes.remove")}>
                  <X />
                </Button>
              </li>
            ))}
          </ul>
          <Button size="sm" variant="ghost" onClick={() => edit([...items, ""])}>
            <Plus /> {tr("conversation.notes.add")}
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            {folder && (
              <Button size="sm" variant="outline" disabled={!body} onClick={() => {
                  a.newSession(folder, body, () => { edit([]); flush() })
                  edit(items.filter((_, j) => !dropIdx.has(j)))
                  setPicked(new Set())
                  flush()
                }}>
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
                {targets.length === 0 && closed.length === 0 && <DropdownMenuItem disabled>{tr("conversation.notes.none")}</DropdownMenuItem>}
                {targets.map((r) => (
                  <DropdownMenuItem key={r.s.id} onSelect={() => void sendTo(r)}>
                    <span className="truncate">{r.s.title || r.s.id.slice(0, 8)}</span>
                  </DropdownMenuItem>
                ))}
                {closed.length > 0 && <DropdownMenuLabel className="whitespace-normal text-xs font-normal text-muted-foreground">{tr("conversation.notes.closedLabel")}</DropdownMenuLabel>}
                {closed.map((r) => (
                  <DropdownMenuItem key={r.s.id} onSelect={() => void sendTo(r)}>
                    <span className="truncate">{r.s.title || r.s.id.slice(0, 8)}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">{tr("conversation.notes.count", { count })}</span>
          </div>
        </div>
      )}
    </div>
  )
}
