import { loc } from "@/i18n"
import { memo, useState } from "react"
import { useTranslation } from "react-i18next"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ChevronRight, CornerDownRight, Wrench } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { api, type Msg } from "@/lib/api"
import { size } from "@/lib/format"
import { noBreak, rehypeNoBreak } from "@/lib/noBreak"
import { CompactSummary, isCompactSummary, isTagged, Tagged } from "./Tagged"

// Messages de Claude : Markdown (tableaux GFM compris). react-markdown n'interprète jamais de HTML brut
// et filtre les URL dangereuses (javascript:) : rien de ce qui est affiché ne peut s'exécuter.
// onLink : intercepte un lien relatif (ex. une autre entrée de mémoire) ; renvoie false pour le laisser faire.
export function Markdown({ t, onLink }: { t: string; onLink?: (href: string) => boolean }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeNoBreak]}
        components={{
          // URL filtrée (javascript:…) : plus de href, donc du texte simple plutôt qu'un lien qui rouvrirait la page.
          a: ({ href, children }) =>
            href ? (
              <a href={href} target="_blank" rel="noopener noreferrer" onClick={(e) => onLink?.(href) && e.preventDefault()}>
                {children}
              </a>
            ) : (
              <span>{children}</span>
            ),
        }}
      >
        {t}
      </ReactMarkdown>
    </div>
  )
}

// Texte collé dans Claude Code (<pasted_content id="…">…</pasted_content id="…">) : un bloc repliable.
const PASTED = /<pasted_content id="[^"]*">\n?([\s\S]*?)\n?<\/pasted_content(?: id="[^"]*")?>/g

function UserText({ t: text }: { t: string }) {
  const { t } = useTranslation()
  const parts: React.ReactNode[] = []
  let at = 0
  for (const m of text.matchAll(PASTED)) {
    const before = text.slice(at, m.index).trim()
    if (before) parts.push(<span key={`t${at}`}>{noBreak(before)}</span>)
    const lines = m[1].split("\n").length
    parts.push(
      <details key={m.index} className="group/paste my-1 rounded-md border bg-background/60">
        <summary className="cursor-pointer list-none px-3 py-1.5 text-xs text-muted-foreground select-none hover:text-foreground">
          {t("conversation.message.pasted", { count: lines })}
        </summary>
        <pre className="max-h-96 overflow-auto border-t px-3 py-2 text-xs whitespace-pre-wrap">{m[1]}</pre>
      </details>
    )
    at = m.index + m[0].length
  }
  const rest = text.slice(at).trim()
  if (rest) parts.push(<span key="rest">{noBreak(rest)}</span>)
  return <>{parts}</>
}

// Spécification ou plan superpowers cité par un appel d'outil : lu à la demande dans le dossier de la session et rendu en Markdown.
const SP_DOC = /docs\/superpowers\/(?:specs|plans)\/[^\s"']+\.md/
function SuperpowersDoc({ dir, path }: { dir: string; path: string }) {
  const { t } = useTranslation()
  const [doc, setDoc] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const flip = () => {
    setOpen(!open)
    if (doc === null) {
      const fail = () => (toast.error(t("conversation.message.docFail")), setOpen(false))
      api.fsFile(dir, path).then((r) => (r.ok && r.body.content !== undefined ? setDoc(r.body.content) : fail()), fail)
    }
  }
  return (
    <div className="mt-1">
      <button type="button" onClick={flip} aria-expanded={open} className="flex cursor-pointer items-center gap-1 text-xs text-primary hover:underline">
        <ChevronRight className={`size-3.5 transition-transform ${open ? "rotate-90" : ""}`} aria-hidden />
        {t("conversation.message.showDoc")}
      </button>
      {open && doc !== null && (
        <div className="mt-2 rounded-lg border bg-card px-4 py-3 leading-relaxed text-card-foreground">
          <Markdown t={doc} />
        </div>
      )}
    </div>
  )
}

// Mémoïsé (voir plus bas) : la conversation se redessine pour l'état de la session, pas pour chacun de ses messages.
type Conv = { dir: string; id: string; tools?: boolean }

function MessageView({ m, conv }: { m: Msg; conv?: Conv }) {
  const { t } = useTranslation()
  const title = m.ts ? new Date(m.ts).toLocaleString(loc()) : undefined
  // Image de la conversation : relue à la demande dans le transcript (GET /conv-image), jamais embarquée dans la liste des messages.
  if (m.img && conv) {
    const src = `/conv-image?dir=${encodeURIComponent(conv.dir)}&id=${encodeURIComponent(conv.id)}&o=${m.o ?? 0}&k=${m.k ?? 0}`
    return (
      <div title={title} className={`msg flex ${m.r === "user" ? "justify-end" : ""}`}>
        <a href={src} target="_blank" rel="noopener noreferrer" title={t("conversation.message.openImage")}>
          <img src={src} loading="lazy" alt={t("conversation.message.imageAlt")} className="max-h-80 max-w-full rounded-xl border bg-muted/30 object-contain" />
        </a>
      </div>
    )
  }
  if (m.r === "user" && isCompactSummary(m.t))
    return (
      <div title={title} className="msg mx-auto max-w-3xl">
        <CompactSummary t={m.t} md={(t) => <Markdown t={t} />} />
      </div>
    )
  if (m.r === "user" && isTagged(m.t))
    return (
      <div title={title} className="msg ml-auto max-w-[85%]">
        <Tagged t={m.t} />
      </div>
    )
  if (m.r === "user")
    return (
      <div title={title} className="msg flex justify-end">
        <div className="max-w-[85%] rounded-lg bg-accent px-4 py-2.5 break-words whitespace-pre-wrap text-accent-foreground">
          <UserText t={m.t} />
        </div>
      </div>
    )
  if (m.r === "tool") {
    // « ↳ … » : résultat d'un appel ; sinon l'appel lui-même (« Outil  argument »).
    const result = m.t.startsWith("↳")
    const Icon = result ? CornerDownRight : Wrench
    return (
      <div title={title} className="msg flex gap-2 border-l-2 border-primary/30 py-0.5 pl-3 text-muted-foreground">
        <Icon className="mt-0.5 size-3.5 shrink-0" aria-label={result ? t("conversation.message.toolResult") : t("conversation.message.toolCall")} />
        <div className="min-w-0">
          <span className="break-words whitespace-pre-wrap">{noBreak(result ? m.t.slice(1).trimStart() : m.t)}</span>
          {!result && conv && SP_DOC.test(m.t) && <SuperpowersDoc dir={conv.dir} path={SP_DOC.exec(m.t)![0]} />}
        </div>
      </div>
    )
  }
  return (
    <div title={title} className="msg leading-relaxed break-words">
      <Markdown t={m.t} />
    </div>
  )
}

// Message démesuré (coupé par le serveur) : affiché par morceaux, chacun à la demande.
function CutMessage({ m, conv }: { m: Msg; conv?: Conv }) {
  const { t } = useTranslation()
  const [more, setMore] = useState({ t: "", cut: m.cut ?? 0 })
  const [busy, setBusy] = useState(false)
  const next = async () => {
    if (!conv) return
    setBusy(true)
    try {
      const r = await api.convText(conv, !!conv.tools, m, more.cut)
      setMore((p) => ({ t: p.t + r.t, cut: r.cut }))
    } catch {
      toast.error(t("conversation.message.readFail"))
    } finally {
      setBusy(false)
    }
  }
  const left = Math.max(1, Math.round(((m.len ?? 0) - more.cut) / 1024))
  return (
    <>
      <MessageView m={more.t ? { ...m, t: m.t + more.t } : m} conv={conv} />
      {more.cut > 0 && conv && (
        <div className={m.r === "user" ? "flex justify-end" : ""}>
          <Button variant="outline" size="sm" disabled={busy} onClick={next}>
            {t("conversation.message.showMore", { size: size(left) })}
          </Button>
        </div>
      )}
    </>
  )
}

function MessageOrCut(p: { m: Msg; conv?: Conv }) {
  return p.m.cut ? <CutMessage {...p} /> : <MessageView {...p} />
}

export const Message = memo(MessageOrCut, (a, b) => a.m === b.m && a.conv?.dir === b.conv?.dir && a.conv?.id === b.conv?.id && a.conv?.tools === b.conv?.tools)
