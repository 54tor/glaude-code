import { useState } from "react"
import { MessageSquare } from "lucide-react"
import { useTranslation } from "react-i18next"
import { ago, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useActions } from "./actions"

const AT = /(^|\s)@([^\s]*)$/
const MAX = 6

// Mention d'une autre session : « @ » dans un message ouvre la liste des sessions (filtrée au fil de la frappe) ; le choix insère sa
// référence (titre, identifiant, transcript), que Claude peut relire. Le curseur n'est suivi qu'à la frappe (track).
export function useMention(text: string, setText: (t: string) => void, exceptId = "") {
  const { t } = useTranslation()
  const a = useActions()
  const [caret, setCaret] = useState(0)
  const [sel, setSel] = useState(0)
  const [off, setOff] = useState("") // texte « @… » écarté par Échap
  const before = text.slice(0, Math.min(caret, text.length))
  const m = AT.exec(before)
  const q = m?.[2].toLowerCase() ?? ""
  const words = q.split(/[^\p{L}\p{N}]+/u).filter(Boolean)
  const hits =
    m && before !== off
      ? a.rows
          .filter((r) => r.s.id !== exceptId && words.every((w) => `${r.s.title} ${r.s.id} ${r.g.project}`.toLowerCase().includes(w)))
          .slice(0, MAX)
      : []
  const at = m ? m.index + m[1].length : 0
  const pick = (i: number) => {
    const r = hits[i]
    if (!r) return
    const ref = t("shell.act.mention", { title: r.s.title.replace(/\s+/g, " ").trim().slice(0, 80), id: r.s.id, path: `${a.root}/${r.s.dir}/${r.s.id}.jsonl` }) + " "
    const pos = at + ref.length
    setText(text.slice(0, at) + ref + text.slice(caret))
    setCaret(pos)
    requestAnimationFrame(() => (document.activeElement as HTMLTextAreaElement | null)?.setSelectionRange?.(pos, pos))
  }
  return {
    track: (el: HTMLTextAreaElement) => (setCaret(el.selectionStart), setSel(0)),
    // true : la touche a été prise par la liste
    onKey: (e: React.KeyboardEvent) => {
      if (!hits.length) return false
      if (e.key === "ArrowDown" || e.key === "ArrowUp") setSel((i) => (i + (e.key === "ArrowDown" ? 1 : hits.length - 1)) % hits.length)
      else if (e.key === "Enter" || e.key === "Tab") pick(sel % hits.length)
      else if (e.key === "Escape") setOff(before)
      else return false
      e.preventDefault()
      return true
    },
    list: hits.length > 0 && (
      <ul className="max-h-56 overflow-y-auto rounded-lg border bg-popover p-1 text-sm shadow-md" role="listbox" aria-label={t("conversation.composer.mentionAria")}>
        {hits.map((r, i) => (
          <li
            key={r.s.id}
            role="option"
            aria-selected={i === sel % hits.length}
            onMouseDown={(e) => (e.preventDefault(), pick(i))}
            className={cn("flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5", i === sel % hits.length && "bg-accent")}
          >
            <MessageSquare className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">{r.s.title}</span>
            <span className="max-w-32 shrink-0 truncate text-xs text-muted-foreground">{tilde(r.g.project, a.home).split("/").pop()}</span>
            <span className="shrink-0 text-xs text-muted-foreground">{ago(r.s.mtime)}</span>
          </li>
        ))}
      </ul>
    ),
  }
}
