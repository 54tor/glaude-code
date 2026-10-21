import { useCallback, useEffect, useRef, useState } from "react"
import { ArrowDownToLine, ArrowUpToLine, FileText, Loader2, MoreHorizontal, SquareTerminal, Wrench } from "lucide-react"
import { type Choice } from "@/lib/models"
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
import { Message } from "./Message"
import { Composer, type Turn } from "./Composer"
import { LoadedDialog } from "./LoadedDialog"

// Messages chargés d'un coup (par la fin) : une session de plusieurs Go compte des milliers de messages.
const PAGE = 200

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
      {live && <Live waiting={a.waiting.has(s.id)} />}
      <span className="truncate">{(live ? "· " : "") + bits.join(" · ")}</span>
    </p>
  )
}

export function Conversation({ row, onClose, args, choice }: { row: Row | null; onClose: () => void; args: string; choice: Choice }) {
  const a = useActions()
  const session = row?.s ?? null
  // Les effets dépendent des identifiants, pas de l'objet : le rafraîchissement périodique de la liste
  // recrée les sessions sans que la conversation affichée doive être rechargée.
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const [tools, setTools] = useState(false)
  const [loadedOpen, setLoadedOpen] = useState(false) // instructions réellement chargées (hook InstructionsLoaded)
  // Les messages sont rangés avec la clé (session, outils) qui les a produits : tant qu'elle ne correspond
  // pas à la demande courante, on affiche le chargement, sans remise à zéro synchrone dans un effet.
  // start/more : décalage de la première ligne lue et existence de messages plus anciens (lecture par la fin).
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[]; start: number; more: boolean } | null>(null)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const keepScroll = useRef(false) // le chargement de messages plus anciens ne doit pas ramener à la fin
  const [info, setInfo] = useState<{ id: string; d: Detail } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const key = id ? `${dir}/${id}/${tools}` : ""
  const msgs = loaded?.key === key ? loaded.msgs : null
  const detail = info && info.id === id ? info.d : null
  // Tour en cours (ou dernier tour) écrit depuis le navigateur, affiché à la suite de la conversation.
  const [chat, setChat] = useState<Turn | null>(null)
  const turn = chat?.id === id ? chat : null
  const viewport = useCallback(() => area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]'), [])
  const loadedNow = loaded?.key === key ? loaded : null
  // Relecture (après un tour) : au moins la page de base, plus de quoi garder ce qui est déjà affiché.
  const reload = () =>
    api.conv({ dir, id }, tools, { limit: Math.max(PAGE, (loadedNow?.msgs.length ?? 0) + 50) }).then((r) => setLoaded({ key, msgs: r.msgs, start: r.start ?? 0, more: !!r.more }))
  const older = useCallback(async () => {
    if (!loadedNow || loadingOlder) return
    setLoadingOlder(true)
    const r = await api.conv({ dir, id }, tools, { limit: PAGE, before: loadedNow.start })
    keepScroll.current = true
    setLoaded({ key, msgs: [...r.msgs, ...loadedNow.msgs], start: r.start ?? 0, more: !!r.more })
    setLoadingOlder(false)
    // Pas de correction manuelle du défilement : le navigateur garde tout seul l'ancrage sur le message affiché
    // (une correction à la main s'ajoutait à la sienne et faisait sauter la vue).
  }, [loadedNow, loadingOlder, dir, id, tools, key])

  useEffect(() => {
    if (!id) return
    let live = true
    api.conv({ dir, id }, tools, { limit: PAGE }).then((r) => live && setLoaded({ key, msgs: r.msgs, start: r.start ?? 0, more: !!r.more }))
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

  // content-visibility affine les hauteurs en cours de rendu : on recolle à la fin sur quelques images.
  const toEnd = useCallback(() => {
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
  }, [viewport])
  // Chargement paresseux : les messages plus anciens arrivent d'eux-mêmes quand on remonte près du début.
  const top = useRef<HTMLDivElement>(null)
  const more = !!loadedNow?.more
  useEffect(() => {
    const el = top.current
    const vp = viewport()
    if (!el || !vp || !more) return
    const io = new IntersectionObserver(([e]) => e.isIntersecting && older(), { root: vp, rootMargin: "600px 0px 0px 0px" })
    io.observe(el)
    return () => io.disconnect()
  }, [more, older, viewport])
  const toTop = () => viewport()?.scrollTo({ top: 0 })
  useEffect(() => {
    if (keepScroll.current) keepScroll.current = false
    else if (msgs) toEnd()
  }, [msgs, toEnd])
  const shown = turn?.msgs.length ?? 0
  useEffect(() => {
    if (shown) toEnd()
  }, [shown, toEnd])

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
          <Button variant="ghost" size="sm" onClick={() => setLoadedOpen(true)} title="Fichiers d'instructions que Claude a réellement chargés dans cette session">
            <FileText /> Instructions
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
          <div className="session-text mx-auto max-w-7xl space-y-5 px-6 py-6">
            {msgs === null && (
              <>
                <Skeleton className="ml-auto h-10 w-2/3" />
                <Skeleton className="h-24 w-full" />
                <Skeleton className="ml-auto h-10 w-1/2" />
              </>
            )}
            {loadedNow?.more && (
              <div ref={top} className="text-center">
                <Button variant="outline" size="sm" onClick={older} disabled={loadingOlder}>
                  {loadingOlder ? <Loader2 className="animate-spin" /> : <ArrowUpToLine />} Charger les messages plus anciens
                </Button>
              </div>
            )}
            {msgs?.map((m, i) => (
              <Message key={m.o === undefined ? i : `${m.o}.${m.n ?? 0}`} m={m} />
            ))}
            {msgs?.length === 0 && !turn && <p className="text-center text-muted-foreground">Aucun message dans cette session.</p>}
            {turn?.msgs
              .filter((m) => tools || m.r !== "tool")
              .map((m, i) => (
                <Message key={`t${i}`} m={m} />
              ))}
            {turn?.running && (
              <p className="flex items-center gap-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" /> Claude répond…
              </p>
            )}
            {turn?.error && <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-destructive">{turn.error}</p>}
          </div>
        </ScrollArea>
        {session && (
          <Composer
            dir={dir}
            id={id}
            args={args}
            defaultChoice={choice}
            live={a.live.has(id)}
            turn={turn}
            setTurn={(f) => setChat((c) => f(c?.id === id ? c : null))}
            onDone={() => {
              // Le transcript contient maintenant le tour : on le relit, et le tour affiché à part disparaît (sauf erreur).
              reload().then(() => setChat((c) => (c?.id === id && !c.error ? { ...c, msgs: [] } : c)))
            }}
          />
        )}
        <LoadedDialog id={id} home={a.home} open={loadedOpen} onClose={() => setLoadedOpen(false)} />
      </DialogContent>
    </Dialog>
  )
}
