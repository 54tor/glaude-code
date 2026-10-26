import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowDown, ArrowUpToLine, BarChart3, FileText, Loader2, MessageSquarePlus, MoreHorizontal, MessagesSquare, Repeat, SquareTerminal, Wrench } from "lucide-react"
import { focusRow } from "@/lib/focusRow"
import { type Choice } from "@/lib/models"
import { api, type Detail, type Msg, type Session } from "@/lib/api"
import { full, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenuCheckboxItem, DropdownMenuGroup, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { useActions } from "./actions"
import { SessionDropdown } from "./SessionMenu"
import { BgBadge, Live } from "./SessionList"
import type { Row } from "./actions"
import { Message } from "./Message"
import { Composer, type Turn } from "./Composer"
import { LiveBar, LiveStats, useLive } from "./LiveStatus"
import { LoadedDialog } from "./LoadedDialog"
import { KeepWarmBanner, KeepWarmItem, SessionStat } from "./KeepWarm"
import { useLocal } from "@/hooks/useLocal"
import { useGit } from "@/hooks/useGit"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"

const FullTerminal = lazy(() => import("./FullTerminal")) // xterm n'est chargé qu'à l'ouverture du terminal

// Messages chargés d'un coup (par la fin) : une session de plusieurs Go compte des milliers de messages.
const PAGE = 200

function Meta({ s, d, onFolder }: { s: Session; d: Detail | null; onFolder?: () => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const live = a.live.has(s.id)
  const bits = [
    tilde(s.cwd, a.home),
    full(s.mtime),
    size(s.kb),
    d && t("conversation.view.meta.prompts", { count: d.n }),
    d?.subagents ? t("conversation.view.meta.subagents", { count: d.subagents }) : null,
    d?.branch && t("conversation.view.meta.branch", { name: d.branch }),
    d?.models.filter((m) => !m.startsWith("<")).join(", "),
  ].filter(Boolean)
  return (
    <p className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground max-md:flex-nowrap max-md:overflow-hidden in-data-[kb]:hidden">
      {live && <Live waiting={a.waiting.has(s.id)} />}
      {live && <BgBadge id={s.id} />}
      {bits.map((b, i) => (
        <span key={i} className={i === 0 ? "truncate" : i > 1 ? "shrink-0 max-md:hidden" : "shrink-0"}>
          {i === 0 && onFolder ? (
            <button type="button" onClick={onFolder} title={t("shell.common.showFolder")} className="max-w-full cursor-pointer truncate underline-offset-2 hover:text-foreground hover:underline">
              {b}
            </button>
          ) : (
            b
          )}
        </span>
      ))}
    </p>
  )
}

// Position de lecture par session, gardée à la fermeture de la fenêtre : `end` vaut vrai quand on était à moins d'une demi-page de la fin.
const scrollMem = new Map<string, { top: number; end: boolean }>()

export function Conversation({ row, onClose, onFolder, args, choice, carousel }: { row: Row | null; onClose: () => void; onFolder?: () => void; args: string; choice: Choice; carousel?: { n: number; next: () => void; stop: () => void } }) {
  const { t } = useTranslation()
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
  const [statsOpen, setStatsOpen] = useState(false)
  // Les messages sont rangés avec la clé (session, outils) qui les a produits : tant qu'elle ne correspond
  // pas à la demande courante, on affiche le chargement, sans remise à zéro synchrone dans un effet.
  // start/more : décalage de la première ligne lue et existence de messages plus anciens (lecture par la fin).
  const [loaded, setLoaded] = useState<{ key: string; msgs: Msg[]; start: number; more: boolean } | null>(null)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const idRef = useRef(id)
  useEffect(() => {
    idRef.current = id // lu par le gestionnaire de défilement, qui ne se réabonne pas à chaque changement de session
  }, [id])
  const restoring = useRef(false) // position mémorisée en cours de rétablissement : le suivi de la fin attend
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
    // Hauteurs enfin stables, l'utilisateur n'ayant pas touché à la vue : à moins d'une demi-page de la fin, on y reste collé.
    const t = setTimeout(() => {
      stop()
      if (vp.scrollHeight - vp.scrollTop - vp.clientHeight < vp.clientHeight / 2) vp.scrollTop = vp.scrollHeight
    }, 3000)
    evs.forEach((e) => vp.addEventListener(e, stop, { passive: true }))
  }, [viewport])
  // Rétablit la position mémorisée pendant que les hauteurs se précisent (rendu paresseux), jusqu'à ce que l'utilisateur prenne la main
  // ou au bout de 1,5 s.
  const restore = useCallback(
    (top: number) => {
      const vp = viewport()
      if (!vp) return
      restoring.current = true
      const go = () => (vp.scrollTop = top)
      go()
      const iv = setInterval(go, 50)
      const evs = ["wheel", "touchstart", "keydown", "pointerdown"]
      const stop = () => {
        clearInterval(iv)
        clearTimeout(t)
        restoring.current = false
        evs.forEach((e) => vp.removeEventListener(e, stop))
      }
      const t = setTimeout(stop, 1500)
      evs.forEach((e) => vp.addEventListener(e, stop, { passive: true }))
    },
    [viewport]
  )
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
  // La vue descend à l'ouverture, à l'envoi d'un message, et suit la fin tant que l'utilisateur y est. Dès qu'il remonte lire, elle ne
  // bouge plus ; le bouton « Aller à la fin » prend le relais.
  const opened = useRef("") // session déjà amenée à la fin une fois
  const [away, setAway] = useState(false)
  const [fromTop, setFromTop] = useState(false)
  const ready = !!session && msgs !== null
  useEffect(() => {
    const vp = viewport()
    if (!vp) return
    // Collée à la fin tant que l'utilisateur y est : la bande d'état (réflexion, outil, file d'attente) change de hauteur sans que la vue décale.
    let stuck = vp.scrollHeight - vp.scrollTop - vp.clientHeight < 80
    const on = () => {
      const gap = vp.scrollHeight - vp.scrollTop - vp.clientHeight
      stuck = gap < 80
      setAway(!stuck)
      setFromTop(vp.scrollTop > 200)
      if (idRef.current && !restoring.current) scrollMem.set(idRef.current, { top: vp.scrollTop, end: gap < vp.clientHeight / 2 })
    }
    // Aimant : à la fin d'un défilement (pas pendant, sinon il lutte avec le suivi de la fin), à moins d'une demi-page de la fin la vue s'y colle.
    const snap = () => {
      const gap = vp.scrollHeight - vp.scrollTop - vp.clientHeight
      if (gap > 0 && gap < vp.clientHeight / 2) vp.scrollTop = vp.scrollHeight
    }
    vp.addEventListener("scrollend", snap, { passive: true })
    vp.addEventListener("scroll", on, { passive: true })
    const ro = new ResizeObserver(() => stuck && !keepScroll.current && !restoring.current && (vp.scrollTop = vp.scrollHeight))
    if (vp.firstElementChild) ro.observe(vp.firstElementChild)
    return () => {
      vp.removeEventListener("scroll", on)
      vp.removeEventListener("scrollend", snap)
      ro.disconnect()
    }
  }, [viewport, ready])
  useEffect(() => {
    if (!id) opened.current = ""
    if (keepScroll.current) return void (keepScroll.current = false)
    if (!msgs) return
    if (opened.current !== id && viewport()) {
      opened.current = id
      const m = scrollMem.get(id)
      if (m && !m.end) restore(m.top)
      else pinEnd()
    }
  }, [msgs, pinEnd, restore, viewport, id, ready])
  const inTerm = termOn && view === "terminal" && !!id && a.live.has(id)
  // Envoi d'un message : on descend pour voir la réponse arriver, puis une seconde fois quand le transcript relu contient le message.
  const sentDown = useCallback(() => {
    toEnd()
    setTimeout(toEnd, 2500)
  }, [toEnd])

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        className="flex h-[calc(var(--vvh,100dvh)*0.94)] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 max-md:top-[var(--vvt,0px)] max-md:left-0 max-md:h-[var(--vvh,100dvh)] max-md:w-screen max-md:translate-x-0 max-md:translate-y-0 max-md:rounded-none max-md:border-0 sm:max-w-none"
        onCloseAutoFocus={(e) => focusRow(lastId.current) && e.preventDefault()}
      >
        <DialogHeader className="gap-1 border-b px-4 pt-4 pr-12 pb-3 md:px-6 md:pt-5 md:pr-14 md:pb-4">
          <div className="flex items-start gap-4">
            <div className="min-w-0 flex-1 space-y-1">
              <DialogTitle className="truncate text-lg tracking-tight">{session?.title}</DialogTitle>
              <DialogDescription className="sr-only">{t("conversation.view.description")}</DialogDescription>
              {session && <Meta s={session} d={detail} onFolder={onFolder} />}
            </div>
            {session && <SessionStat s={session} live={live} onStats={() => setStatsOpen(true)} />}
          </div>
        </DialogHeader>
        {session && <KeepWarmBanner s={session} />}
        {carousel && (
          <div className="flex flex-wrap items-center gap-2 border-b bg-warning/10 px-4 py-1.5 text-sm">
            <Repeat className="size-4 text-warning" aria-hidden /> {t("conversation.view.carousel", { count: carousel.n })}
            <Button variant="outline" size="sm" className="ml-auto" onClick={carousel.next}>
              {t("conversation.view.next")}
            </Button>
            <Button variant="ghost" size="sm" onClick={carousel.stop}>
              {t("conversation.view.quitCarousel")}
            </Button>
          </div>
        )}
        {/* Petit écran : une seule ligne (reprendre, aller à la fin, menu « … » qui reprend les autres commandes) ; l'état git passe dessous. */}
        <div className="flex flex-wrap items-center gap-1 border-b bg-muted/40 px-2 py-1.5 in-data-[kb]:hidden md:px-4">
          {termOn && a.live.has(id) && (
            <Button variant="secondary" size="sm" className="max-md:hidden" onClick={() => setView(inTerm ? "visuel" : "terminal")} title={t("conversation.view.modeTitle")}>
              {inTerm ? <MessagesSquare /> : <SquareTerminal />} {inTerm ? t("conversation.view.modeVisual") : t("conversation.view.modeTerminal")}
            </Button>
          )}
          <Button variant="ghost" size="sm" className="max-md:hidden" onClick={() => setLoadedOpen(true)} title={t("conversation.view.instructionsTitle")}>
            <FileText /> {t("conversation.view.instructions")}
          </Button>
          {session && <LiveStats live={live} tok={session.tok} shown={statsOpen} onShown={setStatsOpen} />}
          {gitDirs.length > 0 && (
            <div className="flex flex-wrap items-center gap-1 max-md:order-last max-md:w-full md:contents">
              {gitDirs.map((d) => (
                <span key={d} className="flex min-w-0 flex-wrap items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs md:shrink-0" title={d}>
                  {d !== row?.g.project && <span className="max-w-32 truncate text-muted-foreground">{d.split("/").filter(Boolean).pop()}</span>}
                  <GitBadge st={git.status[d]} />
                  <GitPush dir={d} st={git.status[d]} />
                  <GitChanges dir={d} count={git.status[d] ? git.status[d].changed + git.status[d].untracked : 0} />
                </span>
              ))}
            </div>
          )}
          {row && !row.g.missing && (
            <Button variant="outline" size="sm" className="max-md:hidden" onClick={() => a.newSession(row.g.project)} title={t("conversation.view.newSessionTitle")}>
              <MessageSquarePlus /> {t("conversation.view.newSession")}
            </Button>
          )}
          {session && (
            <Button size="sm" className="max-md:ml-auto" onClick={() => a.open({ dir: session.dir, id: session.id })}>
              <SquareTerminal /> {t("conversation.view.resume")}
            </Button>
          )}
          {row && (
            <SessionDropdown
              row={row}
              after={onClose}
              extra={
                <DropdownMenuGroup className="md:hidden">
                  <DropdownMenuCheckboxItem checked={tools} onCheckedChange={(c) => setTools(c === true)}>
                    {t("conversation.view.showTools")}
                  </DropdownMenuCheckboxItem>
                  {termOn && a.live.has(id) && (
                    <DropdownMenuItem onSelect={() => setView(inTerm ? "visuel" : "terminal")}>
                      {inTerm ? <MessagesSquare /> : <SquareTerminal />} {inTerm ? t("conversation.view.modeVisual") : t("conversation.view.modeTerminal")}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onSelect={() => setLoadedOpen(true)}>
                    <FileText /> {t("conversation.view.instructions")}
                  </DropdownMenuItem>
                  {live.st && (
                    <DropdownMenuItem onSelect={() => setStatsOpen(true)}>
                      <BarChart3 /> {t("conversation.live.stats")}
                    </DropdownMenuItem>
                  )}
                  {session && <KeepWarmItem s={session} />}
                  <DropdownMenuSeparator />
                </DropdownMenuGroup>
              }
            >
              <Button variant="ghost" size="icon-sm" aria-label={t("conversation.view.moreActions")}>
                <MoreHorizontal />
              </Button>
            </SessionDropdown>
          )}
        </div>
        {inTerm && (
          <Suspense fallback={null}>
            <FullTerminal key={id} id={id} />
          </Suspense>
        )}
        <div className={inTerm ? "hidden" : "relative flex min-h-0 flex-1 flex-col"}>
          {fromTop && (
            <Button size="sm" variant="secondary" className="absolute top-3 right-4 z-10 rounded-full shadow-md" onClick={toTop} aria-label={t("conversation.view.toTop")}>
              <ArrowUpToLine /> {t("conversation.view.toTop")}
            </Button>
          )}
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
                  {loadingOlder ? <Loader2 className="animate-spin" /> : <ArrowUpToLine />} {t("conversation.view.loadOlder")}
                </Button>
              </div>
            )}
            {msgs?.map((m, i) => (
              <Message key={m.o === undefined ? i : `${m.o}.${m.n ?? 0}`} m={m} conv={{ dir, id, tools }} />
            ))}
            {msgs?.length === 0 && !turn && <p className="text-center text-muted-foreground">{t("conversation.view.empty")}</p>}
            {turn?.msgs
              .filter((m) => tools || m.r !== "tool")
              .map((m, i) => (
                <Message key={`t${i}`} m={m} />
              ))}
            {turn?.running && (
              <p role="status" className="flex items-center gap-2 text-muted-foreground">
                <Loader2 className="size-4 animate-spin" aria-hidden /> {t("conversation.view.replying")}
              </p>
            )}
            <LiveBar live={live} id={id} />
            {turn?.error && <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-destructive">{turn.error}</p>}
          </div>
        </ScrollArea>
        {away && (
          <Button size="sm" variant="secondary" className="absolute right-4 bottom-3 rounded-full shadow-md" onClick={toEnd} aria-label={t("conversation.view.toEndAria")}>
            <ArrowDown /> {t("conversation.view.toEnd")}
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
            extra={
              <Button variant={tools ? "secondary" : "ghost"} size="xs" className="text-muted-foreground max-md:hidden" onClick={() => setTools(!tools)} aria-pressed={tools}>
                <Wrench /> {tools ? t("conversation.view.toolsShown") : t("conversation.view.toolsHidden")}
              </Button>
            }
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
