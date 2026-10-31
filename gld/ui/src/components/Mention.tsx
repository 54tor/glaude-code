import { useState } from "react"
import { MessageSquare } from "lucide-react"
import { useTranslation } from "react-i18next"
import { ago, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useActions } from "./actions"

const AT = /(^|\s)@([^\s]*)$/
const MAX = 6
const MAX_TITLE_LENGTH = 80

// Mention d'une autre session : « @ » dans un message ouvre la liste des sessions (filtrée au fil de la frappe) ; le choix insère sa
// référence (titre, identifiant, transcript), que Claude peut relire. Le curseur n'est suivi qu'à la frappe (track).
export function useMention(text: string, setText: (t: string) => void, exceptId = "") {
  const { t } = useTranslation()
  const actions = useActions()
  const [caret, setCaret] = useState(0)
  const [sel, setSel] = useState(0)
  const [off, setOff] = useState("") // texte « @… » écarté par Échap
  const before = text.slice(0, Math.min(caret, text.length))
  const mentionMatch = AT.exec(before)
  const query = mentionMatch?.[2].toLowerCase() ?? ""
  const words = query.split(/[^\p{L}\p{N}]+/u).filter(Boolean)
  const isMentionActive = mentionMatch && before !== off
  const matchesQuery = (row: (typeof actions.rows)[number]) => {
    const searchableText = `${row.s.title} ${row.s.id} ${row.g.project}`.toLowerCase()
    return words.every((word) => searchableText.includes(word))
  }
  const hits = isMentionActive ? actions.rows.filter((row) => row.s.id !== exceptId && matchesQuery(row)).slice(0, MAX) : []
  const at = mentionMatch ? mentionMatch.index + mentionMatch[1].length : 0
  const selectedIndex = sel % hits.length
  const pick = (hitIndex: number) => {
    const row = hits[hitIndex]
    if (!row) return
    const reference =
      t("shell.act.mention", {
        title: row.s.title.replace(/\s+/g, " ").trim().slice(0, MAX_TITLE_LENGTH),
        id: row.s.id,
        path: `${actions.root}/${row.s.dir}/${row.s.id}.jsonl`,
      }) + " "
    const caretPosition = at + reference.length
    setText(text.slice(0, at) + reference + text.slice(caret))
    setCaret(caretPosition)
    requestAnimationFrame(() => (document.activeElement as HTMLTextAreaElement | null)?.setSelectionRange?.(caretPosition, caretPosition))
  }
  return {
    track: (textArea: HTMLTextAreaElement) => {
      setCaret(textArea.selectionStart)
      setSel(0)
    },
    // true : la touche a été prise par la liste
    onKey: (event: React.KeyboardEvent) => {
      if (!hits.length) return false
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        const step = event.key === "ArrowDown" ? 1 : hits.length - 1
        setSel((currentIndex) => (currentIndex + step) % hits.length)
      } else if (event.key === "Enter" || event.key === "Tab") pick(selectedIndex)
      else if (event.key === "Escape") setOff(before)
      else return false
      event.preventDefault()
      return true
    },
    list: hits.length > 0 && (
      <ul className="max-h-56 overflow-y-auto rounded-lg border bg-popover p-1 text-sm shadow-md" role="listbox" aria-label={t("conversation.composer.mentionAria")}>
        {hits.map((row, hitIndex) => (
          <li
            key={row.s.id}
            role="option"
            aria-selected={hitIndex === selectedIndex}
            onMouseDown={(event) => {
              event.preventDefault()
              pick(hitIndex)
            }}
            className={cn("flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5", hitIndex === selectedIndex && "bg-accent")}
          >
            <MessageSquare className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">{row.s.title}</span>
            <span className="max-w-32 shrink-0 truncate text-xs text-muted-foreground">{tilde(row.g.project, actions.home).split("/").pop()}</span>
            <span className="shrink-0 text-xs text-muted-foreground">{ago(row.s.mtime)}</span>
          </li>
        ))}
      </ul>
    ),
  }
}
