import { useEffect, useRef, useState } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { api, type Msg, type Session } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"

// Messages de Claude : Markdown (tableaux GFM compris). react-markdown n'interprète jamais de HTML brut
// et filtre les URL dangereuses (javascript:) : rien de ce qui est affiché ne peut s'exécuter.
function Assistant({ t }: { t: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // URL filtrée (javascript:…) : plus de href, donc du texte simple plutôt qu'un lien qui rouvrirait la page.
          a: ({ href, children }) =>
            href ? (
              <a href={href} target="_blank" rel="noopener noreferrer">
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
  if (m.r === "user")
    return (
      <div title={title} className="msg ml-8 rounded-lg bg-muted px-3 py-2 break-words whitespace-pre-wrap">
        {m.t}
      </div>
    )
  if (m.r === "tool")
    return (
      <div title={title} className="msg border-l-2 pl-2 font-mono text-xs break-words whitespace-pre-wrap text-muted-foreground">
        {m.t}
      </div>
    )
  return (
    <div title={title} className="msg mr-8 break-words">
      <Assistant t={m.t} />
    </div>
  )
}

export function Conversation({ session, onClose }: { session: Session | null; onClose: () => void }) {
  const [tools, setTools] = useState(false)
  // Les messages sont rangés avec la clé (session, outils) qui les a produits : tant qu'elle ne correspond
  // pas à la demande courante, on affiche le chargement, sans remise à zéro synchrone dans un effet.
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[] } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const key = session ? `${session.dir}/${session.id}/${tools}` : ""
  const msgs = loaded?.key === key ? loaded.msgs : null

  useEffect(() => {
    if (!session) return
    let live = true
    api.conv(session, tools).then((r) => live && setLoaded({ key, msgs: r.msgs }))
    return () => {
      live = false
    }
  }, [session, tools, key])

  // content-visibility affine les hauteurs en cours de rendu : on recolle à la fin sur quelques images.
  const toEnd = () => {
    const vp = area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')
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
  const toTop = () => area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]')?.scrollTo({ top: 0 })
  useEffect(() => {
    if (msgs) toEnd()
  }, [msgs])

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[88vh] max-w-4xl flex-col gap-3 sm:max-w-4xl">
        <DialogHeader className="pr-8">
          <DialogTitle className="truncate">{session?.title}</DialogTitle>
          <DialogDescription className="sr-only">Conversation complète de la session</DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <Checkbox id="tools" checked={tools} onCheckedChange={(c) => setTools(c === true)} />
            <Label htmlFor="tools" className="text-xs text-muted-foreground">
              afficher les outils
            </Label>
          </div>
          <div className="ml-auto flex gap-2">
            <Button variant="outline" size="xs" onClick={toTop}>
              ↑ début
            </Button>
            <Button variant="outline" size="xs" onClick={toEnd}>
              ↓ fin
            </Button>
          </div>
        </div>
        <ScrollArea ref={area} className="min-h-0 flex-1 rounded-md border">
          <div className="space-y-3 p-4 text-sm">
            {msgs === null && (
              <>
                <Skeleton className="h-10 w-2/3" />
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-10 w-1/2" />
              </>
            )}
            {msgs?.map((m, i) => <Message key={i} m={m} />)}
            {msgs?.length === 0 && <p className="text-muted-foreground">(aucun message)</p>}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
