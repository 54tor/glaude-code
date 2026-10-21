import { useEffect, useRef, useState } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ArrowDownToLine, ArrowUpToLine, MoreHorizontal, SquareTerminal, Wrench } from "lucide-react"
import { api, type Detail, type Msg, type Session } from "@/lib/api"
import { full, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useActions } from "./actions"
import { SessionDropdown } from "./SessionMenu"
import { Live } from "./SessionList"
import type { Row } from "./actions"
import { isTagged, Tagged } from "./Tagged"

// Messages de Claude : Markdown (tableaux GFM compris). react-markdown n'interprète jamais de HTML brut
// et filtre les URL dangereuses (javascript:) : rien de ce qui est affiché ne peut s'exécuter.
// onLink : intercepte un lien relatif (ex. une autre entrée de mémoire) ; renvoie false pour le laisser faire.
export function Markdown({ t, onLink }: { t: string; onLink?: (href: string) => boolean }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
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

function Message({ m }: { m: Msg }) {
  const title = m.ts ? new Date(m.ts).toLocaleString("fr-FR") : undefined
  if (m.r === "user" && isTagged(m.t))
    return (
      <div title={title} className="msg ml-auto max-w-[85%]">
        <Tagged t={m.t} />
      </div>
    )
  if (m.r === "user")
    return (
      <div title={title} className="msg flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-md bg-primary/[0.07] px-4 py-2.5 break-words whitespace-pre-wrap dark:bg-primary/10">
          {m.t}
        </div>
      </div>
    )
  if (m.r === "tool")
    return (
      <div title={title} className="msg border-l-2 py-0.5 pl-3 break-words whitespace-pre-wrap text-muted-foreground">
        {m.t}
      </div>
    )
  return (
    <div title={title} className="msg leading-relaxed break-words">
      <Markdown t={m.t} />
    </div>
  )
}

function Meta({ s, d }: { s: Session; d: Detail | null }) {
  const a = useActions()
  const live = a.live.has(s.id)
  const bits = [
    tilde(s.cwd, a.home),
    full(s.mtime),
    size(s.kb),
    d && `${d.n} prompt${d.n > 1 ? "s" : ""}`,
    d?.subagents ? `${d.subagents} sous-agent${d.subagents > 1 ? "s" : ""}` : null,
    d?.branch && `branche ${d.branch}`,
    d?.models.filter((m) => !m.startsWith("<")).join(", "),
  ].filter(Boolean)
  return (
    <p className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      {live && <Live />}
      <span className="truncate">{(live ? "· " : "") + bits.join(" · ")}</span>
    </p>
  )
}

export function Conversation({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const a = useActions()
  const session = row?.s ?? null
  // Les effets dépendent des identifiants, pas de l'objet : le rafraîchissement périodique de la liste
  // recrée les sessions sans que la conversation affichée doive être rechargée.
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const [tools, setTools] = useState(false)
  // Les messages sont rangés avec la clé (session, outils) qui les a produits : tant qu'elle ne correspond
  // pas à la demande courante, on affiche le chargement, sans remise à zéro synchrone dans un effet.
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const [info, setInfo] = useState<{ id: string; d: Detail } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const key = id ? `${dir}/${id}/${tools}` : ""
  const msgs = loaded?.key === key ? loaded.msgs : null
  const detail = info && info.id === id ? info.d : null

  useEffect(() => {
    if (!id) return
    let live = true
    api.conv({ dir, id }, tools).then((r) => live && setLoaded({ key, msgs: r.msgs }))
    return () => {
      live = false
    }
  }, [dir, id, tools, key])

  useEffect(() => {
    if (!id) return
    let live = true
    api.info({ dir, id }).then((d) => live && setInfo({ id, d }))
    return () => {
      live = false
    }
  }, [dir, id])

  const viewport = () => area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
  // content-visibility affine les hauteurs en cours de rendu : on recolle à la fin sur quelques images.
  const toEnd = () => {
    const vp = viewport()
    if (!vp) return
    const go = () => (vp.scrollTop = vp.scrollHeight)
    go()
    requestAnimationFrame(() => {
      go()
      requestAnimationFrame(() => {
        go()
        setTimeout(go, 120)
      })
    })
  }
  const toTop = () => viewport()?.scrollTo({ top: 0 })
  useEffect(() => {
    if (msgs) toEnd()
  }, [msgs])

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[94vh] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="gap-1 border-b px-6 pt-5 pr-14 pb-4">
          <DialogTitle className="truncate text-base">{session?.title}</DialogTitle>
          <DialogDescription className="sr-only">Conversation complète de la session</DialogDescription>
          {session && <Meta s={session} d={detail} />}
        </DialogHeader>
        <div className="flex items-center gap-1 border-b bg-muted/30 px-4 py-2">
          <Button variant={tools ? "secondary" : "ghost"} size="sm" onClick={() => setTools(!tools)} aria-pressed={tools}>
            <Wrench /> Outils {tools ? "affichés" : "masqués"}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toTop} aria-label="Aller au début" title="Aller au début">
            <ArrowUpToLine />
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toEnd} aria-label="Aller à la fin" title="Aller à la fin">
            <ArrowDownToLine />
          </Button>
          <div className="ml-auto flex items-center gap-1">
            {session && (
              <Button size="sm" onClick={() => a.open({ dir: session.dir, id: session.id })}>
                <SquareTerminal /> Reprendre dans un terminal
              </Button>
            )}
            {row && (
              <SessionDropdown row={row} after={onClose}>
                <Button variant="ghost" size="icon-sm" aria-label="Plus d'actions">
                  <MoreHorizontal />
                </Button>
              </SessionDropdown>
            )}
          </div>
        </div>
        <ScrollArea ref={area} className="min-h-0 flex-1">
          <div className="session-text mx-auto max-w-4xl space-y-5 px-6 py-6">
            {msgs === null && (
              <>
                <Skeleton className="ml-auto h-10 w-2/3" />
                <Skeleton className="h-24 w-full" />
                <Skeleton className="ml-auto h-10 w-1/2" />
              </>
            )}
            {msgs?.map((m, i) => (
              <Message key={i} m={m} />
            ))}
            {msgs?.length === 0 && <p className="text-center text-muted-foreground">Aucun message dans cette session.</p>}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
