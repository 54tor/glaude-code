import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, Wrench } from "lucide-react"
import { api, type Msg } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Message } from "./Message"

export type ToolGroup = { count: number; from: number; until: number }

// Outils masqués : les appels consécutifs d'une réponse tiennent sur une ligne repliée ; un clic relit et déplie ces messages.
export function ToolAnchor({ dir, id, g }: { dir: string; id: string; g: ToolGroup }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [got, setGot] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const key = `${g.from}/${g.until}/${g.count}`
  useEffect(() => {
    if (!open) return
    let live = true
    api.convTools({ dir, id }, g.from, g.until).then((r) => live && setGot({ key, msgs: r.msgs }), () => {})
    return () => {
      live = false
    }
  }, [open, dir, id, g.from, g.until, key])
  const msgs = got?.key === key ? got.msgs : null
  return (
    <div className="msg">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full min-w-0 cursor-pointer items-center gap-2 border-l-2 border-primary/30 py-0.5 pl-3 text-left text-xs text-muted-foreground hover:text-foreground"
      >
        <Wrench className="size-3.5 shrink-0" aria-hidden />
        {t("conversation.view.toolAnchor", { count: g.count })}
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} aria-hidden />
      </button>
      {open && (
        <div className="mt-1 space-y-1">
          {msgs === null ? <p className="pl-3 text-xs text-muted-foreground">…</p> : msgs.map((m, i) => <Message key={`${m.o}.${m.n ?? 0}.${i}`} m={m} />)}
        </div>
      )}
    </div>
  )
}

// Regroupe les ancres vides consécutives (appels d'outils masqués) ; until : décalage du message suivant, 0 en fin de conversation.
export function groupTools(msgs: Msg[]): (Msg | ToolGroup)[] {
  const out: (Msg | ToolGroup)[] = []
  for (let i = 0; i < msgs.length; i++) {
    const m = msgs[i]
    if (m.r !== "tool" || m.t !== "") {
      out.push(m)
      continue
    }
    let j = i
    while (j + 1 < msgs.length && msgs[j + 1].r === "tool" && msgs[j + 1].t === "") j++
    out.push({ count: j - i + 1, from: m.o ?? 0, until: msgs[j + 1]?.o ?? 0 })
    i = j
  }
  return out
}
