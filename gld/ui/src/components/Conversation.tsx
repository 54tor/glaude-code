import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react"
import { ArrowDown, ArrowDownToLine, ArrowUpToLine, FileText, Loader2, MessageSquarePlus, MoreHorizontal, MessagesSquare, Repeat, SquareTerminal, Wrench } from "lucide-react"
import { focusRow } from "@/lib/focusRow"
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
import { LiveBar, LiveStats, useLive } from "./LiveStatus"
import { LoadedDialog } from "./LoadedDialog"
import { KeepWarmBanner, KeepWarmButton, SessionStat } from "./KeepWarm"
import { useLocal } from "@/hooks/useLocal"
import { useGit } from "@/hooks/useGit"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"

const FullTerminal = lazy(() => import("./FullTerminal")) // xterm n'est chargé qu'à l'ouverture du terminal

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
    <p className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
      {live && <Live waiting={a.waiting.has(s.id)} />}
      {bits.map((b, i) => (
        <span key={i} className={i === 0 ? "truncate" : "shrink-0"}>
          {b}
        </span>
      ))}
    </p>
  )
}

export function Conversation({ row, onClose, args, choice, carousel }: { row: Row | null; onClose: () => void; args: string; choice: Choice; carousel?: { n: number; next: () => void; stop: () => void } }) {
  const a = useActions()
  const session = row?.s ?? null
  // Les effets dépendent des identifiants, pas de l'objet : le rafraîchissement périodique de la liste
  // recrée les sessions sans que la conversation affichée doive être rechargée.
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const live = useLive(dir, id, a.live.has(id))
  // État git du dossier de la session et de ses dépôts, si ce sont des dépôts : on peut voir les changements et pousser d'ici.
  const git = useGit()
  const gitDirs = row ? [row.g.project, ...(git.repos[row.g.project] ?? [])].filter((d) => git.status[d]) : []
  const lastId = useRef("") // à la fermeture, session vaut déjà null : le focus revient à la ligne de cette session
  useEffect(() => {
    if (id) lastId.current = id
  }, [id])
  const [tools, setTools] = useState(false)
  const [view, setView] = useLocal("gld.session-view", "visuel") // choix gardé dans ce navigateur
  const [termOn, setTermOn] = useState(false) // GLD_WEB_TERMINAL=1 côté serveur
  useEffect(() => {
    api.termInfo().then((r) => setTermOn(r.enabled), () => {})
  }, [])
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
  // Même session, autre clé (outils affichés ou masqués) : l'ancien contenu reste à l'écran jusqu'à l'arrivée du nouveau.
  const msgs = loaded?.key === key || (id && loaded?.key.startsWith(`${dir}/${id}/`)) ? loaded!.msgs : null
  const detail = info && info.id === id ? info.d : null
  // Tour en cours (ou dernier tour) écrit depuis le navigateur, affiché à la suite de la conversation.
  const [chat, setChat] = useState<Turn | null>(null)
  const turn = chat?.id === id ? chat : null
  // Message envoyé à une session ouverte (tmux) : on relit son transcript tant qu'elle travaille, pour montrer la réponse au fil de l'eau.
  const [sentAt, setSentAt] = useState<{ id: string; at: number } | null>(null)
  const viewport = useCallback(() => area.current?.querySelector<HTMLElement>('[data-slot="scroll-area-viewport"]'), [])
  const loadedNow = loaded?.key === key ? loaded : null
  // Relecture (après un tour) : au moins la page de base, plus de quoi garder ce qui est déjà affiché.
  // Les relectures se chevauchent (toutes les 2 s pendant qu'une session travaille, plus celle de l'envoi) : sur un serveur lent, une
  // réponse plus ancienne arrivait après une plus récente et faisait disparaître les derniers messages. Seule la dernière demande
  // compte ; renvoie les messages lus, ou null si une demande plus récente l'a remplacée.
  const seq = useRef(0)
  const reload = () => {
    const n = ++seq.current
    return api.conv({ dir, id }, tools, { limit: Math.max(PAGE, (loadedNow?.msgs.length ?? 0) + 50) }).then((r) => {
      if (n !== seq.current) return null
      setLoaded({ key, msgs: r.msgs, start: r.start ?? 0, more: !!r.more })
      return r.msgs
    })
  }
  const reloadRef = useRef(reload)
  useEffect(() => {
    reloadRef.current = reload
  })
  const older = useCallback(async () => {
    if (!loadedNow || loadingOlder) return
    setLoadingOlder(true)
    const n = ++seq.current // une relecture en vol, plus ancienne que ce chargement, ne doit pas l'écraser
    const r = await api.conv({ dir, id }, tools, { limit: PAGE, before: loadedNow.start })
    if (n !== seq.current) return void setLoadingOlder(false) // une relecture plus récente est passée : on redemandera
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

  useEffect(() => {
    if (!sentAt || sentAt.id !== id) return
    let live = true
    const t = setInterval(async () => {
      if (Date.now() - sentAt.at > 15 * 60_000) return setSentAt(null)
      const [, st] = await Promise.all([reloadRef.current(), api.sessionIdle(id)])
      // Le premier relevé suit de près l'envoi : la session n'a pas encore commencé, on laisse quelques secondes.
      if (live && st.idle && Date.now() - sentAt.at > 4000) setSentAt(null)
    }, 2000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [sentAt, id])

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
  // À l'ouverture : la vue reste collée à la fin pendant que la hauteur du contenu se précise (rendu paresseux, images, code), jusqu'à ce
  // que l'utilisateur prenne la main (molette, toucher, clavier, clic) ou au bout de 3 s.
  const pinEnd = useCallback(() => {
    const vp = viewport()
    if (!vp) return
    const go = () => (vp.scrollTop = vp.scrollHeight)
    go()
    const ro = new ResizeObserver(go)
    if (vp.firstElementChild) ro.observe(vp.firstElementChild)
    const evs = ["wheel", "touchstart", "keydown", "pointerdown"]
    const stop = () => {
      clearTimeout(t)
      ro.disconnect()
      evs.forEach((e) => vp.removeEventListener(e, stop))
    }
    const t = setTimeout(stop, 3000)
    evs.forEach((e) => vp.addEventListener(e, stop, { passive: true }))
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
  // La vue ne descend que dans deux cas : à l'ouverture de la session, et quand l'utilisateur envoie un message. Elle ne suit jamais
  // l'arrivée de messages ou une mise à jour : lire plus haut ne doit pas être interrompu ; le bouton « Aller à la fin » prend le relais.
  const opened = useRef("") // session déjà amenée à la fin une fois
  const [away, setAway] = useState(false)
  const ready = !!session && msgs !== null
  useEffect(() => {
    const vp = viewport()
    if (!vp) return
    const on = () => {
      setAway(vp.scrollHeight - vp.scrollTop - vp.clientHeight >= 80)
    }
    vp.addEventListener("scroll", on, { passive: true })
    return () => vp.removeEventListener("scroll", on)
  }, [viewport, ready])
  useEffect(() => {
    if (!id) opened.current = ""
    if (keepScroll.current) return void (keepScroll.current = false)
    if (!msgs) return
    if (opened.current !== id && viewport()) {
      opened.current = id
      pinEnd()
    }
  }, [msgs, pinEnd, viewport, id, ready])
  const inTerm = termOn && view === "terminal" && !!id && a.live.has(id)
  // Envoi d'un message : on descend pour voir la réponse arriver, puis une seconde fois quand le transcript relu contient le message.
  const sentDown = useCallback(() => {
    toEnd()
    setTimeout(toEnd, 2500)
  }, [toEnd])

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        className="flex h-[94vh] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none"
        onCloseAutoFocus={(e) => focusRow(lastId.current) && e.preventDefault()}
      >
        <DialogHeader className="gap-1 border-b px-4 pt-4 pr-12 pb-3 md:px-6 md:pt-5 md:pr-14 md:pb-4">
          <div className="flex items-start gap-4">
            <div className="min-w-0 flex-1 space-y-1">
              <DialogTitle className="truncate text-lg tracking-tight">{session?.title}</DialogTitle>
              <DialogDescription className="sr-only">Conversation complète de la session</DialogDescription>
              {session && <Meta s={session} d={detail} />}
            </div>
            {session && <SessionStat s={session} live={live} />}
          </div>
        </DialogHeader>
        {session && <KeepWarmBanner s={session} />}
        {carousel && (
          <div className="flex flex-wrap items-center gap-2 border-b bg-amber-500/10 px-4 py-1.5 text-sm">
            <Repeat className="size-4 text-amber-600" aria-hidden /> Carrousel : {carousel.n} session{carousel.n > 1 ? "s" : ""} en attente. La suivante s'ouvre dès que vous répondez.
            <Button variant="outline" size="sm" className="ml-auto" onClick={carousel.next}>
              Suivante
            </Button>
            <Button variant="ghost" size="sm" onClick={carousel.stop}>
              Quitter le carrousel
            </Button>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-1 border-b bg-muted/40 px-2 py-1.5 md:px-4">
          <Button variant={tools ? "secondary" : "ghost"} size="sm" onClick={() => setTools(!tools)} aria-pressed={tools}>
            <Wrench /> Outils {tools ? "affichés" : "masqués"}
          </Button>
          {termOn && a.live.has(id) && (
            <Button variant="secondary" size="sm" onClick={() => setView(inTerm ? "visuel" : "terminal")} title="Bascule entre la conversation et le terminal complet de la session">
              {inTerm ? <MessagesSquare /> : <SquareTerminal />} {inTerm ? "Mode visuel" : "Mode terminal"}
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={() => setLoadedOpen(true)} title="Fichiers d'instructions que Claude a réellement chargés dans cette session">
            <FileText /> Instructions
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toTop} aria-label="Aller au début" title="Aller au début">
            <ArrowUpToLine />
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={toEnd} aria-label="Aller à la fin" title="Aller à la fin">
            <ArrowDownToLine />
          </Button>
          {session && <KeepWarmButton s={session} />}
          {session && <LiveStats live={live} tok={session.tok} />}
          <div className="flex flex-wrap items-center gap-1">
            {gitDirs.map((d) => (
              <span key={d} className="flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs" title={d}>
                {d !== row?.g.project && <span className="max-w-32 truncate text-muted-foreground">{d.split("/").filter(Boolean).pop()}</span>}
                <GitBadge st={git.status[d]} />
                <GitPush dir={d} st={git.status[d]} />
                <GitChanges dir={d} count={git.status[d] ? git.status[d].changed + git.status[d].untracked : 0} />
              </span>
            ))}
            {row && !row.g.missing && (
              <Button variant="outline" size="sm" onClick={() => a.newSession(row.g.project)} title="Nouvelle session Claude dans le même dossier">
                <MessageSquarePlus /> Nouvelle session
              </Button>
            )}
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
        {inTerm && (
          <Suspense fallback={null}>
            <FullTerminal key={id} id={id} />
          </Suspense>
        )}
        <div className={inTerm ? "hidden" : "relative flex min-h-0 flex-1 flex-col"}>
        <ScrollArea ref={area} className="min-h-0 flex-1">
          <div className="session-text mx-auto max-w-5xl space-y-6 px-3 py-4 md:px-8 md:py-6">
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
              <Message key={m.o === undefined ? i : `${m.o}.${m.n ?? 0}`} m={m} conv={{ dir, id, tools }} />
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
            <LiveBar live={live} id={id} />
            {turn?.error && <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-destructive">{turn.error}</p>}
          </div>
        </ScrollArea>
        {away && (
          <Button size="sm" variant="secondary" className="absolute right-4 bottom-3 rounded-full shadow-md" onClick={toEnd} aria-label="Aller à la fin de la conversation">
            <ArrowDown /> Aller à la fin
          </Button>
        )}
        </div>
        {session && !inTerm && (
          <Composer
            dir={dir}
            id={id}
            args={args}
            defaultChoice={choice}
            live={a.live.has(id)}
            liveState={live}
            onSent={() => (a.touch(id), setSentAt({ id, at: Date.now() }), sentDown(), void reloadRef.current(), carousel?.next())}
            onWrite={sentDown}
            turn={turn}
            setTurn={(f) => setChat((c) => f(c?.id === id ? c : null))}
            // Le transcript contient maintenant le tour : on le relit, et le tour affiché à part disparaît (sauf erreur).
            onDone={async () => {
              // Le tour affiché à part ne disparaît que lorsque le transcript relu le contient (Claude l'écrit parfois avec retard) :
              // sinon la réponse s'effaçait un instant, voire jusqu'à la relecture suivante.
              const sent = turn?.msgs.find((m) => m.r === "user")?.t.trim()
              for (let i = 0; i < 5; i++) {
                const got = await reload()
                if (got === null || !sent || got.slice(-12).some((m) => m.r === "user" && m.t.includes(sent.slice(0, 60)))) break
                await new Promise((r) => setTimeout(r, 1000))
              }
              setChat((c) => (c?.id === id && !c.error ? { ...c, msgs: [] } : c))
            }}
          />
        )}
        <LoadedDialog id={id} home={a.home} open={loadedOpen} onClose={() => setLoadedOpen(false)} />
      </DialogContent>
    </Dialog>
  )
}
