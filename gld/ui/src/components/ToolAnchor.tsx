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
    let isMounted = true
    api.convTools({ dir, id }, g.from, g.until).then((toolsResponse) => isMounted && setGot({ key, msgs: toolsResponse.msgs }), () => {})
    return () => {
      isMounted = false
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
          {msgs === null ? <p className="pl-3 text-xs text-muted-foreground">…</p> : msgs.map((message, messageIndex) => <Message key={`${message.o}.${message.n ?? 0}.${messageIndex}`} m={message} conv={{ dir, id }} />)}
        </div>
      )}
    </div>
  )
}

// Regroupe les ancres vides consécutives (appels d'outils masqués) ; until : décalage du message suivant, 0 en fin de conversation.
export function groupTools(msgs: Msg[]): (Msg | ToolGroup)[] {
  const isHiddenToolCall = (message: Msg | undefined) => message !== undefined && message.r === "tool" && message.t === ""
  const groupedMessages: (Msg | ToolGroup)[] = []
  for (let messageIndex = 0; messageIndex < msgs.length; messageIndex++) {
    const message = msgs[messageIndex]
    if (!isHiddenToolCall(message)) {
      groupedMessages.push(message)
      continue
    }
    let lastHiddenIndex = messageIndex
    while (isHiddenToolCall(msgs[lastHiddenIndex + 1])) lastHiddenIndex++
    groupedMessages.push({ count: lastHiddenIndex - messageIndex + 1, from: message.o ?? 0, until: msgs[lastHiddenIndex + 1]?.o ?? 0 })
    messageIndex = lastHiddenIndex
  }
  return groupedMessages
}
