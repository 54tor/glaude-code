import { memo, useState } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { CornerDownRight, Wrench } from "lucide-react"
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

function UserText({ t }: { t: string }) {
  const parts: React.ReactNode[] = []
  let at = 0
  for (const m of t.matchAll(PASTED)) {
    const before = t.slice(at, m.index).trim()
    if (before) parts.push(<span key={`t${at}`}>{noBreak(before)}</span>)
    const lines = m[1].split("\n").length
    parts.push(
      <details key={m.index} className="group/paste my-1 rounded-md border bg-background/60">
        <summary className="cursor-pointer list-none px-3 py-1.5 text-xs text-muted-foreground select-none hover:text-foreground">
          Texte collé · {lines} ligne{lines > 1 ? "s" : ""}
        </summary>
        <pre className="max-h-96 overflow-auto border-t px-3 py-2 text-xs whitespace-pre-wrap">{m[1]}</pre>
      </details>
    )
    at = m.index + m[0].length
  }
  const rest = t.slice(at).trim()
  if (rest) parts.push(<span key="rest">{noBreak(rest)}</span>)
  return <>{parts}</>
}

// Mémoïsé (voir plus bas) : la conversation se redessine pour l'état de la session, pas pour chacun de ses messages.
type Conv = { dir: string; id: string; tools?: boolean }

function MessageView({ m, conv }: { m: Msg; conv?: Conv }) {
  const title = m.ts ? new Date(m.ts).toLocaleString("fr-FR") : undefined
  // Image de la conversation : relue à la demande dans le transcript (GET /conv-image), jamais embarquée dans la liste des messages.
  if (m.img && conv) {
    const src = `/conv-image?dir=${encodeURIComponent(conv.dir)}&id=${encodeURIComponent(conv.id)}&o=${m.o ?? 0}&k=${m.k ?? 0}`
    return (
      <div title={title} className={`msg flex ${m.r === "user" ? "justify-end" : ""}`}>
        <a href={src} target="_blank" rel="noopener noreferrer" title="Ouvrir l'image en grand">
          <img src={src} loading="lazy" alt="Image de la conversation" className="max-h-80 max-w-full rounded-xl border bg-muted/30 object-contain" />
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
        <Icon className="mt-0.5 size-3.5 shrink-0" aria-label={result ? "résultat d'outil" : "appel d'outil"} />
        <span className="min-w-0 break-words whitespace-pre-wrap">{noBreak(result ? m.t.slice(1).trimStart() : m.t)}</span>
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
  const [more, setMore] = useState({ t: "", cut: m.cut ?? 0 })
  const [busy, setBusy] = useState(false)
  const next = async () => {
    if (!conv) return
    setBusy(true)
    try {
      const r = await api.convText(conv, !!conv.tools, m, more.cut)
      setMore((p) => ({ t: p.t + r.t, cut: r.cut }))
    } catch {
      toast.error("Suite du message illisible")
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
            Afficher la suite · {size(left)} restants
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
