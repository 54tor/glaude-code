import { Bell, Minimize2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"

// Blocs balisés que Claude Code glisse dans les messages (<task-notification>…) : affichés en carte lisible.
const BLOCK = /<([a-z][a-z-]*)>([\s\S]*?)<\/\1>/g
const NAMES: Record<string, string> = { "task-notification": "conversation.tagged.taskNotification" }
const FIELDS: Record<string, string> = {
  "task-id": "conversation.tagged.fields.task",
  "tool-use-id": "conversation.tagged.fields.call",
  "output-file": "conversation.tagged.fields.output",
  "task-type": "conversation.tagged.fields.type",
  note: "conversation.tagged.fields.note",
}

export const isTagged = (t: string) => /^\s*<[a-z][a-z-]*>/.test(t) && !/^\s*<pasted_content/.test(t)

function Card({ tag, body }: { tag: string; body: string }) {
  const { t } = useTranslation()
  const fields = [...body.matchAll(BLOCK)].map(([, k, v]) => [k, v.trim()] as const)
  const get = (k: string) => fields.find(([f]) => f === k)?.[1]
  const status = get("status")
  return (
    <div className="rounded-xl border bg-muted/40 px-4 py-3 text-sm">
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
        <p className="mt-1.5 break-words whitespace-pre-wrap">{body.trim()}</p>
      ) : (
        <>
          {get("summary") && <p className="mt-1.5 break-words whitespace-pre-wrap">{get("summary")}</p>}
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
            {fields
              .filter(([k]) => k !== "summary" && k !== "status")
              .map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-muted-foreground">{FIELDS[k] ? t(FIELDS[k]) : k}</dt>
                  <dd className="min-w-0 truncate font-mono" title={v}>
                    {v}
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
export function Tagged({ t }: { t: string }) {
  const parts: React.ReactNode[] = []
  let at = 0
  for (const m of t.matchAll(BLOCK)) {
    const before = t.slice(at, m.index).trim()
    if (before)
      parts.push(
        <p key={`t${at}`} className="break-words whitespace-pre-wrap">
          {before}
        </p>
      )
    parts.push(<Card key={m.index} tag={m[1]} body={m[2]} />)
    at = m.index + m[0].length
  }
  const rest = t.slice(at).trim()
  if (rest)
    parts.push(
      <p key="rest" className="break-words whitespace-pre-wrap">
        {rest}
      </p>
    )
  return <div className="space-y-2">{parts}</div>
}

// Résumé laissé par /compact : « <tools-used-before-summary>outils</tools-used-before-summary> » suivi du résumé en Markdown.
const COMPACT = /<tools-used-before-summary>([\s\S]*?)<\/tools-used-before-summary>/
export const isCompactSummary = (t: string) => COMPACT.test(t)

export function CompactSummary({ t: text, md }: { t: string; md: (t: string) => React.ReactNode }) {
  const { t } = useTranslation()
  const m = text.match(COMPACT)!
  const tools = m[1].split(",").map((x) => x.trim()).filter(Boolean)
  const intro = text.slice(0, m.index).trim()
  const body = text.slice((m.index ?? 0) + m[0].length).trim()
  return (
    <div className="space-y-2 rounded-xl border bg-muted/40 px-4 py-3 text-sm">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Minimize2 className="size-3.5" /> {t("conversation.tagged.compacted")}
      </div>
      {intro && <p className="text-xs text-muted-foreground">{intro}</p>}
      {tools.length > 0 && (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground select-none hover:text-foreground">{t("conversation.tagged.toolsBefore", { n: tools.length })}</summary>
          <div className="mt-1.5 flex flex-wrap gap-1">
            {tools.map((x) => (
              <Badge key={x} variant="outline" className="font-mono font-normal">
                {x}
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
