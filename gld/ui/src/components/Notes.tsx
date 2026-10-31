import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Check, ChevronRight, Fan, FolderOpen, History, Undo2, MessageSquarePlus, NotebookPen, Plus, Send, X } from "lucide-react"
import { toast } from "sonner"
import { api, type SentNote } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { setOpen, useOpen } from "@/hooks/useOpen"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions, type Row } from "./actions"

const SAVE_DELAY_MS = 700
const MAX_ITEM_LENGTH = 4000
const MAX_SENT_HISTORY = 50
const MAX_CLOSED_SESSIONS = 30
const SEND_TIMEOUT_MS = 20_000
const SHORT_ID_LENGTH = 8

// Une note = une liste d'items, enregistrée en JSON ; un ancien texte libre devient un item par ligne non vide.
export const notesOf = (raw: string): string[] => parse(raw)
const parse = (raw: string): string[] => {
  try {
    const parsed: unknown = JSON.parse(raw)
    if (Array.isArray(parsed)) return parsed.filter((item): item is string => typeof item === "string")
  } catch {
    /* texte libre */
  }
  return raw.split("\n").filter((line) => line.trim())
}
const pack = (items: string[]) => (items.some((item) => item.trim()) ? JSON.stringify(items) : "")

const rowLabel = (row: Row) => row.s.title || row.s.id.slice(0, SHORT_ID_LENGTH)

// Bloc-notes d'un projet ou d'un dossier, enregistré tout seul. Le texte peut partir vers une nouvelle session (dans folder) ou vers
// une session ouverte dans tmux parmi celles de la vue.
export function Notes({ noteKey, folder: base, scope, rows }: { noteKey: string; folder?: string; scope: string[]; rows: Row[] }) {
  const { t } = useTranslation()
  const actions = useActions()
  const dirKey = "gld.notes.dir/" + noteKey
  const [custom, setCustom] = useState(() => {
    try {
      return localStorage.getItem(dirKey) ?? ""
    } catch {
      return ""
    }
  })
  const folder = custom || base
  const setFolder = (directory: string) => {
    setCustom(directory)
    try {
      if (directory) localStorage.setItem(dirKey, directory)
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
  const [sent, setSent] = useState<SentNote[]>([]) // historique des envois : filet si la session ne reçoit jamais le texte
  const [sentOpen, flipSent] = useOpen("ns/" + noteKey, false)
  const [picked, setPicked] = useState<Set<number>>(new Set()) // lignes cochées ; aucune cochée = tout part
  const fields = useRef<(HTMLTextAreaElement | null)[]>([])
  const focusNext = useRef<number | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const latest = useRef({ text: "", key: noteKey })

  useEffect(() => {
    let isMounted = true
    api.notes().then(
      (notesResponse) => {
        if (!isMounted) return
        const rawNote = notesResponse.notes[noteKey] ?? ""
        saved.current = pack(parse(rawNote))
        latest.current = { text: saved.current, key: noteKey } // sinon le flush de sortie croit la note vidée et la supprime
        setSavedList(parse(rawNote))
        setItems(parse(rawNote))
      },
      () => isMounted && setItems([])
    )
    api.notesSent(noteKey).then((sentResponse) => isMounted && setSent(sentResponse.sent), () => {})
    return () => {
      isMounted = false
    }
  }, [noteKey])

  const flush = () => {
    clearTimeout(timer.current)
    const { text: pendingText, key } = latest.current
    if (pendingText === saved.current) return
    const before = saved.current
    saved.current = pendingText
    api.notesSave({ key, text: pendingText }).then(
      () => {
        window.dispatchEvent(new Event("gld:notes"))
        setSavedList(parse(pendingText))
      },
      (saveError) => {
        saved.current = before
        toast.error(errMsg(saveError))
      }
    )
  }
  useEffect(() => flush, []) // enregistre ce qui reste en quittant la vue
  const addRef = useRef<() => void>(() => {})
  useEffect(() => {
    const onNoteAdd = () => addRef.current()
    window.addEventListener("gld:note-add", onNoteAdd)
    return () => window.removeEventListener("gld:note-add", onNoteAdd)
  }, [])

  const edit = (next: string[]) => {
    setItems(next)
    latest.current = { text: pack(next), key: noteKey }
    clearTimeout(timer.current)
    timer.current = setTimeout(flush, SAVE_DELAY_MS)
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
  const isItemChosen = (item: { text: string; index: number }) => item.text && (picked.size === 0 || picked.has(item.index))
  const chosen = items.map((item, itemIndex) => ({ text: item.trim(), index: itemIndex })).filter(isItemChosen)
  const body = chosen.map((item) => "- " + item.text).join("\n")
  const count = items.filter((item) => item.trim()).length
  const togglePick = (itemIndex: number) =>
    setPicked((pickedIndexes) => {
      if (pickedIndexes.has(itemIndex)) return new Set([...pickedIndexes].filter((pickedIndex) => pickedIndex !== itemIndex))
      return new Set(pickedIndexes).add(itemIndex)
    })
  const dropIdx = new Set(chosen.map((item) => item.index))
  // Seules les sessions du dossier (ou des dossiers du projet) auquel appartient la note reçoivent son texte.
  const dirs = custom ? [custom] : scope
  const here = rows.filter((row) => dirs.includes(row.s.cwd))
  const targets = here.filter((row) => sendable.has(row.s.id) && actions.live.has(row.s.id))
  const closed = here.filter((row) => !actions.live.has(row.s.id)).slice(0, MAX_CLOSED_SESSIONS)
  const filled = items.map((item, itemIndex) => (item.trim() ? itemIndex : -1)).filter((itemIndex) => itemIndex >= 0)
  const allPicked = filled.length > 0 && filled.every((itemIndex) => picked.has(itemIndex))
  // écrit l'historique avant tout envoi ; sans trace côté serveur, l'envoi n'a pas lieu
  const record = async (to: string) => {
    try {
      const response = await api.notesSentAdd({ key: noteKey, text: body, to })
      setSent((history) => [...history, response.entry].slice(-MAX_SENT_HISTORY))
      return true
    } catch (recordError) {
      toast.error(errMsg(recordError))
      return false
    }
  }
  const restore = (sentNote: SentNote) => {
    const lines = sentNote.text.split("\n").map((line) => line.replace(/^- /, ""))
    edit([...items.filter((item) => item.trim()), ...lines])
    void forget(sentNote)
  }
  const forget = async (sentNote: SentNote) => {
    try {
      await api.notesSentRemove(noteKey, sentNote.id)
      setSent((history) => history.filter((entry) => entry.id !== sentNote.id))
    } catch (removeError) {
      toast.error(errMsg(removeError))
    }
  }
  const dropSentItems = () => {
    edit(items.filter((_, itemIndex) => !dropIdx.has(itemIndex)))
    setPicked(new Set())
    flush()
  }
  const sendTo = async (row: Row) => {
    if (!(await record(rowLabel(row)))) return
    const isLive = actions.live.has(row.s.id)
    let delivered = false
    try {
      // Session en cours de réponse : le texte entre dans la file serveur (envoyé à la fin du tour) au lieu d'être tapé dans son terminal.
      if (isLive && !actions.waiting.has(row.s.id)) await api.queueAdd(row.s.id, body)
      else if (isLive) await api.sessionSend(row.s.id, body, SEND_TIMEOUT_MS)
      delivered = isLive || (await actions.resumeSend(row, body))
    } catch (sendError) {
      toast.error(errMsg(sendError))
    }
    if (!delivered) return
    toast.success(t("conversation.notes.sent", { title: rowLabel(row) }))
    dropSentItems()
  }
  const sendToNewSession = async () => {
    if (!folder) return
    if (!(await record(t("conversation.notes.newTarget") + " · " + folder))) return
    actions.newSession(folder, body)
    dropSentItems()
  }
  const pickFolder = () => void actions.pickDir(t("conversation.notes.folderTitle"), folder ?? "").then((directory) => directory && setFolder(directory))
  const loadSendable = (isOpen: boolean) => isOpen && api.sendInfo().then((info) => setSendable(new Set(info.ids)), () => {})
  const removeItem = (itemIndex: number) => {
    setPicked(new Set())
    edit(items.filter((_, otherIndex) => otherIndex !== itemIndex))
  }
  const handleItemKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>, item: string, itemIndex: number) => {
    const isSubmitShortcut = event.key === "Enter" && (event.ctrlKey || event.metaKey)
    if (!isSubmitShortcut) return
    event.preventDefault()
    if (!item.trim()) return
    flush()
    const isLastItem = itemIndex === items.length - 1
    if (isLastItem || items[itemIndex + 1].trim()) edit([...items.slice(0, itemIndex + 1), "", ...items.slice(itemIndex + 1)])
    focusNext.current = itemIndex + 1
  }
  return (
    <div className="mb-4 rounded-lg border">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button onClick={flip} aria-expanded={open} className="flex w-full items-center gap-2 px-3 py-2 text-sm font-medium">
            <ChevronRight className={cn("size-4 transition-transform", open && "rotate-90")} />
            <NotebookPen className="size-4 text-muted-foreground" />
            {t("conversation.notes.title")}
            {count > 0 && <span className="text-xs font-normal text-muted-foreground">{count}</span>}
            {custom && <span className="truncate text-xs font-normal text-muted-foreground" title={custom}>{custom}</span>}
          </button>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={pickFolder}>
            <FolderOpen /> {t("conversation.notes.changeFolder")}
          </ContextMenuItem>
          {custom && (
            <ContextMenuItem onSelect={() => setFolder("")}>
              <Undo2 /> {t("conversation.notes.resetFolder")}
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>
      {open && (
        <div className="space-y-2 border-t p-3">
          {filled.length > 1 && (
            <label className="flex w-fit cursor-pointer items-center gap-1.5 px-2 text-xs text-muted-foreground">
              <Checkbox checked={allPicked} onCheckedChange={() => setPicked(allPicked ? new Set() : new Set(filled))} />
              {t("conversation.notes.selectAll")}
            </label>
          )}
          <ul className="space-y-1.5">
            {items.map((item, itemIndex) => (
              <li key={itemIndex} className="flex items-start gap-1.5 rounded-lg border bg-background px-2 py-1">
                <Checkbox checked={picked.has(itemIndex)} onCheckedChange={() => togglePick(itemIndex)} aria-label={t("conversation.notes.pick")} className="mt-2.5 shrink-0" />
                <textarea
                  ref={(field) => void (fields.current[itemIndex] = field)}
                  value={item}
                  onKeyDown={(event) => handleItemKeyDown(event, item, itemIndex)}
                  onChange={(event) => edit(items.map((otherItem, otherIndex) => (otherIndex === itemIndex ? event.target.value : otherItem)))}
                  onBlur={flush}
                  maxLength={MAX_ITEM_LENGTH}
                  rows={1}
                  aria-label={t("conversation.notes.item")}
                  className="session-text field-sizing-content max-h-48 min-w-0 flex-1 resize-none bg-transparent py-1 outline-none"
                />
                {item.trim() &&
                  (savedList[itemIndex] === item ? (
                    <Check className="mt-1.5 size-3.5 shrink-0 text-success" aria-label={t("conversation.notes.saved")} />
                  ) : (
                    <Fan className="mt-1.5 size-3.5 shrink-0 animate-spin text-muted-foreground" aria-label={t("conversation.notes.saving")} />
                  ))}
                <Button variant="ghost" size="icon-xs" onClick={() => removeItem(itemIndex)} aria-label={t("conversation.notes.remove")}>
                  <X />
                </Button>
              </li>
            ))}
          </ul>
          <Button size="sm" variant="ghost" onClick={() => edit([...items, ""])}>
            <Plus /> {t("conversation.notes.add")}
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            {folder && (
              <Button size="sm" variant="outline" disabled={!body} onClick={sendToNewSession}>
                <MessageSquarePlus /> {t("conversation.notes.newSession")}
              </Button>
            )}
            <DropdownMenu onOpenChange={loadSendable}>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="outline" disabled={!body}>
                  <Send /> {t("conversation.notes.sendOpen")}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="max-h-72 w-80 overflow-y-auto">
                <DropdownMenuLabel className="whitespace-normal">{t("conversation.notes.sendLabel")}</DropdownMenuLabel>
                {targets.length === 0 && closed.length === 0 && <DropdownMenuItem disabled>{t("conversation.notes.none")}</DropdownMenuItem>}
                {targets.map((row) => (
                  <DropdownMenuItem key={row.s.id} onSelect={() => void sendTo(row)}>
                    <span className="truncate">{rowLabel(row)}</span>
                  </DropdownMenuItem>
                ))}
                {closed.length > 0 && <DropdownMenuLabel className="whitespace-normal text-xs font-normal text-muted-foreground">{t("conversation.notes.closedLabel")}</DropdownMenuLabel>}
                {closed.map((row) => (
                  <DropdownMenuItem key={row.s.id} onSelect={() => void sendTo(row)}>
                    <span className="truncate">{rowLabel(row)}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
            <span className="ml-auto text-xs text-muted-foreground tabular-nums">{t("conversation.notes.count", { count })}</span>
          </div>
          {sent.length > 0 && (
            <div>
              <button onClick={flipSent} aria-expanded={sentOpen} className="flex items-center gap-1.5 px-1 py-1 text-xs text-muted-foreground">
                <ChevronRight className={cn("size-3.5 transition-transform", sentOpen && "rotate-90")} />
                <History className="size-3.5" />
                {t("conversation.notes.sentTitle")} <span className="tabular-nums">{sent.length}</span>
              </button>
              {sentOpen && (
                <ul className="mt-1 space-y-1.5">
                  {[...sent].reverse().map((sentNote) => (
                    <li key={sentNote.id} className="flex items-start gap-1.5 rounded-lg border bg-background px-2 py-1">
                      <div className="min-w-0 flex-1 py-1">
                        <div className="session-text whitespace-pre-wrap break-words">{sentNote.text}</div>
                        <div className="truncate text-xs text-muted-foreground">
                          {new Date(sentNote.at).toLocaleString()} · {sentNote.to}
                        </div>
                      </div>
                      <Button variant="ghost" size="icon-xs" onClick={() => restore(sentNote)} aria-label={t("conversation.notes.restore")} title={t("conversation.notes.restore")}>
                        <Undo2 />
                      </Button>
                      <Button variant="ghost" size="icon-xs" onClick={() => void forget(sentNote)} aria-label={t("conversation.notes.forget")} title={t("conversation.notes.forget")}>
                        <X />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
