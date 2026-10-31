import { useContext, useLayoutEffect, useRef, useState } from "react"
import { Bell, Minimize2, Reply, Send, Users } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import { ActionsCtx } from "./actions"

// Blocs balisés que Claude Code glisse dans les messages (<task-notification>…) : affichés en carte lisible.
const BLOCK = /<([a-z][a-z-]*)((?:\s[^<>]*)?)>([\s\S]*?)<\/\1>/g
const NAMES: Record<string, string> = { "task-notification": "conversation.tagged.taskNotification" }
const attr = (attributes: string, attributeName: string) => attributes.match(new RegExp(`${attributeName}="([^"]*)"`))?.[1]
const FIELDS: Record<string, string> = {
  "task-id": "conversation.tagged.fields.task",
  "tool-use-id": "conversation.tagged.fields.call",
  "output-file": "conversation.tagged.fields.output",
  "task-type": "conversation.tagged.fields.type",
  note: "conversation.tagged.fields.note",
}

// Corps d'un message d'équipier qui est un objet JSON (ex. idle_notification) : décodé pour être listé champ par champ.
export function parseJsonBody(body: string): Record<string, unknown> | null {
  const trimmedBody = body.trim()
  if (!trimmedBody.startsWith("{")) return null
  try {
    const parsedBody = JSON.parse(trimmedBody)
    const isPlainObject = parsedBody && typeof parsedBody === "object" && !Array.isArray(parsedBody)
    return isPlainObject ? parsedBody : null
  } catch {
    return null
  }
}
const show = (fieldValue: unknown) => (typeof fieldValue === "string" ? fieldValue : JSON.stringify(fieldValue))

const KNOWN = /<(?:teammate-message|task-notification|cross-session-message|agent-message)[\s>][\s\S]*?<\/(?:teammate-message|task-notification|cross-session-message|agent-message)>/
// Début de message balisé quelconque, ou balise connue glissée plus loin dans le texte (ex. après un tiret de liste).
export const isTagged = (text: string) => {
  const startsWithTag = /^\s*<[a-z][a-z-]*[\s>]/.test(text) && !/^\s*<pasted_content/.test(text)
  return startsWithTag || KNOWN.test(text)
}

// Message de service d'un agent ou d'une session (pas un prompt qui commencerait par « < »).
export const isServiceMsg = (text: string) => /^\s*<(?:agent-message|cross-session-message|teammate-message)[\s>]/.test(text)

// Corps replié à 5 lignes avec ellipse ; le bouton n'apparaît que si le texte déborde.
function Clamp({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const clampedBody = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [overflows, setOverflows] = useState(false)
  useLayoutEffect(() => {
    const bodyElement = clampedBody.current
    if (bodyElement && !open) setOverflows(bodyElement.scrollHeight > bodyElement.clientHeight + 1)
  }, [open])
  return (
    <div className="mt-1.5">
      <div ref={clampedBody} className={`break-words ${open ? "" : "line-clamp-5"}`}>{children}</div>
      {(overflows || open) && (
        <button type="button" onClick={() => setOpen(!open)} className="mt-1 text-xs text-primary underline underline-offset-2 hover:decoration-2">
          {t(open ? "conversation.tagged.less" : "conversation.tagged.more")}
        </button>
      )}
    </div>
  )
}

type Md = (text: string) => React.ReactNode
// Répondre à une session : la consigne part dans le composer, Claude l'envoie avec SendMessage.
export type OnReply = (to: string, name: string) => void

function Card({ tag, attrs, body, md, onReply }: { tag: string; attrs: string; body: string; md: Md; onReply?: OnReply }) {
  const { t } = useTranslation()
  const fields = [...body.matchAll(BLOCK)].map(([, fieldName, , fieldValue]) => [fieldName, fieldValue.trim()] as const)
  const fieldValueOf = (fieldName: string) => fields.find(([candidateName]) => candidateName === fieldName)?.[1]
  const status = fieldValueOf("status")
  const acts = useContext(ActionsCtx) // absent côté invité
  const isMessageCard = tag === "teammate-message" || tag === "cross-session-message" || tag === "agent-message"
  if (isMessageCard) {
    const cross = tag === "cross-session-message"
    const agent = tag === "agent-message"
    const senderOf = () => {
      if (cross) return attr(attrs, "from-name") ?? attr(attrs, "from")
      if (agent) return attr(attrs, "from")
      return attr(attrs, "teammate_id")
    }
    const from = senderOf()
    const summary = cross ? attr(attrs, "from-mode") : attr(attrs, "summary")
    const json = parseJsonBody(body)
    const Icon = cross ? Send : Users
    const peer = cross && from ? acts?.rows.find((row) => row.s.title === from) : undefined
    const titleKeyOf = () => {
      if (cross) return "conversation.tagged.crossSessionMessage"
      if (agent) return "conversation.tagged.agentMessage"
      return "conversation.tagged.teammateMessage"
    }
    const renderBody = () => {
      if (json)
        return (
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            {Object.entries(json).map(([fieldName, fieldValue]) => (
              <div key={fieldName} className="contents">
                <dt className="text-muted-foreground">{fieldName}</dt>
                <dd className="min-w-0 break-words whitespace-pre-wrap">{show(fieldValue)}</dd>
              </div>
            ))}
          </dl>
        )
      if (agent || cross) return <Clamp>{md(body.trim())}</Clamp>
      return <div className="mt-1.5 break-words">{md(body.trim())}</div>
    }
    return (
      <div className="rounded-lg border bg-muted/40 px-4 py-3 text-sm">
        <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <Icon className="size-3.5" />
          {peer && acts ? (
            <>
              {t("conversation.tagged.crossSessionMessage", { name: "" }).trim()}
              <button type="button" onClick={() => acts.read(peer.s)} title={peer.g.project} className="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground hover:bg-accent">{from}</button>
            </>
          ) : (
            t(titleKeyOf(), { name: from ?? "?" })
          )}
          {summary && <Badge variant="outline" className="ml-auto max-w-[60%] truncate font-normal">{summary}</Badge>}
          {cross && onReply && attr(attrs, "from") && (
            <button type="button" onClick={() => onReply(attr(attrs, "from")!, from ?? "?")} title={t("conversation.tagged.reply")} aria-label={t("conversation.tagged.reply")} className={`${summary ? "" : "ml-auto "}rounded p-0.5 hover:bg-muted hover:text-foreground`}>
              <Reply className="size-3.5" />
            </button>
          )}
        </div>
        {renderBody()}
      </div>
    )
  }
  return (
    <div className="rounded-lg border bg-muted/40 px-4 py-3 text-sm">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Bell className="size-3.5" />
        {NAMES[tag] ? t(NAMES[tag]) : tag}
        {status && (
          <Badge variant={status === "completed" ? "secondary" : "outline"} className="ml-auto">
            {status}
          </Badge>
        )}
      </div>
      {fields.length === 0 ? (
        <div className="mt-1.5 break-words">{md(body.trim())}</div>
      ) : (
        <>
          {fieldValueOf("summary") && <div className="mt-1.5 break-words">{md(fieldValueOf("summary")!)}</div>}
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
            {fields
              .filter(([fieldName]) => fieldName !== "summary" && fieldName !== "status")
              .map(([fieldName, fieldValue]) => (
                <div key={fieldName} className="contents">
                  <dt className="text-muted-foreground">{FIELDS[fieldName] ? t(FIELDS[fieldName]) : fieldName}</dt>
                  <dd className="min-w-0 truncate font-mono" title={fieldValue}>
                    {fieldValue}
                  </dd>
                </div>
              ))}
          </dl>
        </>
      )}
    </div>
  )
}

// Un message fait de blocs balisés et, éventuellement, de texte libre autour.
export function Tagged({ t, md, onReply }: { t: string; md: Md; onReply?: OnReply }) {
  const parts: React.ReactNode[] = []
  let consumedUntil = 0
  for (const blockMatch of t.matchAll(BLOCK)) {
    const textBefore = t.slice(consumedUntil, blockMatch.index).trim()
    if (textBefore)
      parts.push(
        <div key={`t${consumedUntil}`} className="break-words">
          {md(textBefore)}
        </div>
      )
    parts.push(<Card key={blockMatch.index} tag={blockMatch[1]} attrs={blockMatch[2]} body={blockMatch[3]} md={md} onReply={onReply} />)
    consumedUntil = blockMatch.index + blockMatch[0].length
  }
  const rest = t.slice(consumedUntil).trim()
  if (rest)
    parts.push(
      <div key="rest" className="break-words">
        {md(rest)}
      </div>
    )
  return <div className="space-y-2">{parts}</div>
}

// Résumé laissé par /compact : « <tools-used-before-summary>outils</tools-used-before-summary> » suivi du résumé en Markdown.
const COMPACT = /<tools-used-before-summary>([\s\S]*?)<\/tools-used-before-summary>/
export const isCompactSummary = (text: string) => COMPACT.test(text)

export function CompactSummary({ t: text, md }: { t: string; md: (text: string) => React.ReactNode }) {
  const { t } = useTranslation()
  const compactMatch = text.match(COMPACT)!
  const tools = compactMatch[1]
    .split(",")
    .map((toolName) => toolName.trim())
    .filter(Boolean)
  const intro = text.slice(0, compactMatch.index).trim()
  const body = text.slice((compactMatch.index ?? 0) + compactMatch[0].length).trim()
  return (
    <div className="space-y-2 rounded-lg border bg-muted/40 px-4 py-3 text-sm">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Minimize2 className="size-3.5" /> {t("conversation.tagged.compacted")}
      </div>
      {intro && <p className="text-xs text-muted-foreground">{intro}</p>}
      {tools.length > 0 && (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground select-none hover:text-foreground">{t("conversation.tagged.toolsBefore", { n: tools.length })}</summary>
          <div className="mt-1.5 flex flex-wrap gap-1">
            {tools.map((toolName) => (
              <Badge key={toolName} variant="outline" className="font-mono font-normal">
                {toolName}
              </Badge>
            ))}
          </div>
        </details>
      )}
      {body && (
        <details open>
          <summary className="cursor-pointer text-xs text-muted-foreground select-none hover:text-foreground">{t("conversation.tagged.summary")}</summary>
          <div className="mt-1.5">{md(body)}</div>
        </details>
      )}
    </div>
  )
}
