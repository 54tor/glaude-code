import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { swipeHandlers } from "@/lib/swipe"
import { ArrowDown, ArrowUpToLine, Copy, Undo2, BarChart3, Bug, ChevronLeft, ChevronRight, FolderOpen, Power, FileText, Fan, MessageSquarePlus, MoreHorizontal, MessagesSquare, GalleryHorizontalEnd, SquareTerminal, Wrench, RefreshCcwDot } from "lucide-react"
import { focusRow } from "@/lib/focusRow"
import { type Choice } from "@/lib/models"
import { api, type Detail, type Msg, type Session } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { HOT } from "@/lib/sessionsView"
import { ago, brief, compact, dedupeDocs, full, tilde } from "@/lib/format"
import { Chip } from "@/components/ui/chip"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenuCheckboxItem, DropdownMenuGroup, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { toast } from "sonner"
import { copyText } from "@/lib/utils"
import { Confirm, type Ask } from "./Confirm"
import { Skeleton } from "@/components/ui/skeleton"
import { useActions } from "./actions"
import { SessionDropdown } from "./SessionMenu"
import { BgBadge, Live } from "./SessionList"
import type { Row } from "./actions"
import { Message } from "./Message"
import { CloseSession, Composer, type Turn } from "./Composer"
import { LiveTasks } from "./LiveTasks"
import { LiveAgents, LiveBar, LiveStats, StatsButton, useLive } from "./LiveStatus"
import { LoadedDialog } from "./LoadedDialog"
import { CacheState, KeepWarmBanner, KeepWarmButton, KeepWarmItem } from "./KeepWarm"
import { useLocal } from "@/hooks/useLocal"
import { useGit } from "@/hooks/useGit"
import { groupTools, ToolAnchor } from "./ToolAnchor"
import { GitRepo } from "./GitRepo"
import { GitRepos } from "./GitRepos"

const FullTerminal = lazy(() => import("./FullTerminal")) // xterm n'est chargé qu'à l'ouverture du terminal

// Messages chargés d'un coup (par la fin) : une session de plusieurs Go compte des milliers de messages.
const PAGE = 200
const COMPACT_WINDOW = 200_000
// Fenêtre par modèle : la famille 5 en a une de 1 M ; le transcript ne porte jamais le suffixe « [1m] » des modèles plus anciens.
const windowOf = (model: string, ctx: number) => {
  const hasLargeWindow = /-5-\d/.test(model) || model.includes("[1m]") || ctx > COMPACT_WINDOW
  return hasLargeWindow ? LARGE_WINDOW : COMPACT_WINDOW
}
const COLD_HINT_MIN_TOKENS = 50_000 // en dessous, reprendre une session froide coûte trop peu pour la signaler
const COMPACT_HINT_AT = 0.6 // part de la fenêtre à partir de laquelle on conseille de compacter
const LARGE_WINDOW = 1_000_000
const RELOAD_EXTRA_MESSAGES = 50
const LIVE_RELOAD_INTERVAL_MS = 2000
const LIVE_RELOAD_MAX_DURATION_MS = 15 * 60_000
const LIVE_IDLE_GRACE_MS = 4000
const FINAL_RELOAD_DELAY_MS = 1500
const PIN_END_DURATION_MS = 3000
const RESTORE_INTERVAL_MS = 50
const RESTORE_DURATION_MS = 1500
const LAST_FRAME_DELAY_MS = 120
const AT_END_THRESHOLD_PX = 80
const FROM_TOP_THRESHOLD_PX = 200
const SECOND_SCROLL_DELAY_MS = 2500
const TURN_RELOAD_ATTEMPTS = 5
const TURN_RELOAD_DELAY_MS = 1000
const RECENT_MESSAGES_CHECKED = 12
const SENT_PREFIX_LENGTH = 60
const USER_INPUT_EVENTS = ["wheel", "touchstart", "keydown", "pointerdown"]

const distanceToEnd = (viewportElement: HTMLElement) => viewportElement.scrollHeight - viewportElement.scrollTop - viewportElement.clientHeight

function metaBitClassName(bitIndex: number, hasFolderButton: boolean) {
  if (bitIndex !== 0) return "shrink-0 max-md:hidden @max-[60rem]:hidden"
  return hasFolderButton ? "min-w-0 max-w-full" : "min-w-0 truncate"
}

function Meta({ session, onFolder, extra }: { session: Session; onFolder?: () => void; extra?: React.ReactNode }) {
  const { t } = useTranslation()
  const actions = useActions()
  const live = actions.live.has(session.id)
  const bits = [tilde(session.cwd, actions.home), brief(session.mtime)]
  return (
    <div className="@container flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground in-data-[kb]:hidden">
      {live && <Live waiting={actions.waiting.has(session.id)} />}
      {bits.map((bit, bitIndex) => (
        <span key={bitIndex} title={bitIndex === 1 ? full(session.mtime) : undefined} className={metaBitClassName(bitIndex, !!onFolder)}>
          {bitIndex === 0 && onFolder ? (
            <Button variant="outline" size="xs" onClick={onFolder} title={t("shell.common.showFolder")} className="max-w-full justify-start max-md:[&>svg]:hidden">
              <FolderOpen /> <span className="truncate">{bit}</span>
            </Button>
          ) : (
            bit
          )}
        </span>
      ))}
      {extra}
    </div>
  )
}

// Position de lecture par session, gardée à la fermeture de la fenêtre : `end` vaut vrai quand on était à moins d'une demi-page de la fin.
const scrollMem = new Map<string, { top: number; end: boolean }>()

export function Conversation({ row, onClose, onFolder, args, choice, carousel }: { row: Row | null; onClose: () => void; onFolder?: () => void; args: string; choice: Choice; carousel?: { n: number; next: () => void; prev: () => void; stop: () => void } }) {
  const { t } = useTranslation()
  const actions = useActions()
  const session = row?.s ?? null
  // Les effets dépendent des identifiants, pas de l'objet : le rafraîchissement périodique de la liste
  // recrée les sessions sans que la conversation affichée doive être rechargée.
  const dir = session?.dir ?? ""
  const id = session?.id ?? ""
  const live = useLive(dir, id, actions.live.has(id))
  // État git du dossier de la session et de ses dépôts, si ce sont des dépôts : on peut voir les changements et pousser d'ici.
  const git = useGit()
  const gitDirs = row ? [row.g.project].filter((repoDir) => git.status[repoDir]) : []
  const gitSubs = row ? (git.repos[row.g.project] ?? []).filter((repoDir) => git.status[repoDir]) : []
  // Ce qui se trouvait sous le clic droit : sélection, bloc de code, lien.
  const [hit, setHit] = useState({ sel: "", code: "", link: "" })
  const copy = (text: string) =>
    copyText(text).then(
      () => toast.success(t("common.copied")),
      () => toast.error(t("conversation.view.copyFail")),
    )
  const lastId = useRef("") // à la fermeture, session vaut déjà null : le focus revient à la ligne de cette session
  useEffect(() => {
    if (id) lastId.current = id
  }, [id])
  const [tools, setTools] = useState(false)
  const [view, setView] = useLocal("gld.session-view", "visuel") // choix gardé dans ce navigateur
  const [termOn, setTermOn] = useState(false) // GLD_WEB_TERMINAL=1 côté serveur
  useEffect(() => {
    api.termInfo().then((terminalInfo) => setTermOn(terminalInfo.enabled), () => {})
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
  const [info, setInfo] = useState<{ id: string; detail: Detail } | null>(null)
  const area = useRef<HTMLDivElement>(null)
  const key = id ? `${dir}/${id}/${tools}` : ""
  // Même session, autre clé (outils affichés ou masqués) : l'ancien contenu reste à l'écran jusqu'à l'arrivée du nouveau.
  const isSameSessionLoaded = !!id && !!loaded?.key.startsWith(`${dir}/${id}/`)
  const msgs = loaded?.key === key || isSameSessionLoaded ? loaded!.msgs : null
  const detail = info && info.id === id ? info.detail : null
  // Tour en cours (ou dernier tour) écrit depuis le navigateur, affiché à la suite de la conversation.
  const [chat, setChat] = useState<Turn | null>(null)
  const [ask, setAsk] = useState<Ask | null>(null)
  const rewind = (messageOffset: number) =>
    setAsk({
      title: t("conversation.view.rewind"),
      body: t("conversation.view.rewindBody"),
      confirm: t("conversation.view.rewind"),
      run: () =>
        void api.convRewind(dir, id, messageOffset).then(
          (response) => {
            toast.success(response.message)
            setChat(null)
            void reloadRef.current()
          },
          (error) => toast.error(errMsg(error))
        ),
    })
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
    seq.current += 1
    const requestNumber = seq.current
    const limit = Math.max(PAGE, (loadedNow?.msgs.length ?? 0) + RELOAD_EXTRA_MESSAGES)
    return api.conv({ dir, id }, tools, { limit, anchors: true }).then((conversation) => {
      if (requestNumber !== seq.current) return null
      setLoaded({ key, msgs: conversation.msgs, start: conversation.start ?? 0, more: !!conversation.more })
      return conversation.msgs
    })
  }
  const reloadRef = useRef(reload)
  useEffect(() => {
    reloadRef.current = reload
  })
  const older = useCallback(async () => {
    if (!loadedNow || loadingOlder) return
    setLoadingOlder(true)
    seq.current += 1 // une relecture en vol, plus ancienne que ce chargement, ne doit pas l'écraser
    const requestNumber = seq.current
    const olderPage = await api.conv({ dir, id }, tools, { limit: PAGE, before: loadedNow.start, anchors: true })
    if (requestNumber !== seq.current) {
      setLoadingOlder(false) // une relecture plus récente est passée : on redemandera
      return
    }
    keepScroll.current = true
    setLoaded({ key, msgs: [...olderPage.msgs, ...loadedNow.msgs], start: olderPage.start ?? 0, more: !!olderPage.more })
    setLoadingOlder(false)
    // Pas de correction manuelle du défilement : le navigateur garde tout seul l'ancrage sur le message affiché
    // (une correction à la main s'ajoutait à la sienne et faisait sauter la vue).
  }, [loadedNow, loadingOlder, dir, id, tools, key])

  useEffect(() => {
    if (!id) return
    let isMounted = true
    api.conv({ dir, id }, tools, { limit: PAGE, anchors: true }).then((conversation) => {
      if (isMounted) setLoaded({ key, msgs: conversation.msgs, start: conversation.start ?? 0, more: !!conversation.more })
    })
    return () => {
      isMounted = false
    }
  }, [dir, id, tools, key])

  useEffect(() => {
    if (!id) return
    let isMounted = true
    api.info({ dir, id }).then((sessionDetail) => isMounted && setInfo({ id, detail: sessionDetail }))
    return () => {
      isMounted = false
    }
  }, [dir, id])

  useEffect(() => {
    if (!sentAt || sentAt.id !== id) return
    let isMounted = true
    const reloadTimer = setInterval(async () => {
      const elapsedMs = Date.now() - sentAt.at
      if (elapsedMs > LIVE_RELOAD_MAX_DURATION_MS) {
        setSentAt(null)
        return
      }
      const idleState = await Promise.all([reloadRef.current(), api.sessionIdle(id)]).then(([, sessionIdle]) => sessionIdle, () => null) // serveur coupé : on réessaie au tour suivant
      // Le premier relevé suit de près l'envoi : la session n'a pas encore commencé, on laisse quelques secondes.
      const isIdleAfterGrace = isMounted && !!idleState?.idle && Date.now() - sentAt.at > LIVE_IDLE_GRACE_MS
      if (!isIdleAfterGrace) return
      setSentAt(null)
      // Le relevé a pu précéder l'écriture de la dernière réponse dans le transcript : une relecture de plus, juste après.
      setTimeout(() => void reloadRef.current(), FINAL_RELOAD_DELAY_MS)
    }, LIVE_RELOAD_INTERVAL_MS)
    return () => {
      isMounted = false
      clearInterval(reloadTimer)
    }
  }, [sentAt, id])

  // Session qui passe en attente alors qu'elle est affichée (même pilotée depuis son terminal) : sa dernière réponse vient d'être écrite.
  const idle = !!id && actions.waiting.has(id)
  useEffect(() => {
    if (!idle) return
    void reloadRef.current()
    const reloadTimer = setTimeout(() => void reloadRef.current(), FINAL_RELOAD_DELAY_MS)
    return () => clearTimeout(reloadTimer)
  }, [idle, id])

  // content-visibility affine les hauteurs en cours de rendu : on recolle à la fin sur quelques images.
  const toEnd = useCallback(() => {
    const viewportElement = viewport()
    if (!viewportElement) return
    const scrollToBottom = () => {
      viewportElement.scrollTop = viewportElement.scrollHeight
    }
    scrollToBottom()
    requestAnimationFrame(() => {
      scrollToBottom()
      requestAnimationFrame(() => {
        scrollToBottom()
        setTimeout(scrollToBottom, LAST_FRAME_DELAY_MS)
      })
    })
  }, [viewport])
  // À l'ouverture : la vue reste collée à la fin pendant que la hauteur du contenu se précise (rendu paresseux, images, code), jusqu'à ce
  // que l'utilisateur prenne la main (molette, toucher, clavier, clic) ou au bout de 3 s.
  const pinEnd = useCallback(() => {
    const viewportElement = viewport()
    if (!viewportElement) return
    const scrollToBottom = () => {
      viewportElement.scrollTop = viewportElement.scrollHeight
    }
    scrollToBottom()
    const resizeObserver = new ResizeObserver(scrollToBottom)
    if (viewportElement.firstElementChild) resizeObserver.observe(viewportElement.firstElementChild)
    const stop = () => {
      clearTimeout(pinTimer)
      resizeObserver.disconnect()
      USER_INPUT_EVENTS.forEach((eventName) => viewportElement.removeEventListener(eventName, stop))
    }
    // Hauteurs enfin stables, l'utilisateur n'ayant pas touché à la vue : à moins d'une demi-page de la fin, on y reste collé.
    const pinTimer = setTimeout(() => {
      stop()
      if (distanceToEnd(viewportElement) < viewportElement.clientHeight / 2) scrollToBottom()
    }, PIN_END_DURATION_MS)
    USER_INPUT_EVENTS.forEach((eventName) => viewportElement.addEventListener(eventName, stop, { passive: true }))
  }, [viewport])
  // Rétablit la position mémorisée pendant que les hauteurs se précisent (rendu paresseux), jusqu'à ce que l'utilisateur prenne la main
  // ou au bout de 1,5 s.
  const restore = useCallback(
    (top: number) => {
      const viewportElement = viewport()
      if (!viewportElement) return
      restoring.current = true
      const scrollToSavedTop = () => {
        viewportElement.scrollTop = top
      }
      scrollToSavedTop()
      const restoreInterval = setInterval(scrollToSavedTop, RESTORE_INTERVAL_MS)
      const stop = () => {
        clearInterval(restoreInterval)
        clearTimeout(restoreTimer)
        restoring.current = false
        USER_INPUT_EVENTS.forEach((eventName) => viewportElement.removeEventListener(eventName, stop))
      }
      const restoreTimer = setTimeout(stop, RESTORE_DURATION_MS)
      USER_INPUT_EVENTS.forEach((eventName) => viewportElement.addEventListener(eventName, stop, { passive: true }))
    },
    [viewport]
  )
  // Chargement paresseux : les messages plus anciens arrivent d'eux-mêmes quand on remonte près du début.
  const top = useRef<HTMLDivElement>(null)
  const more = !!loadedNow?.more
  useEffect(() => {
    const topSentinel = top.current
    const viewportElement = viewport()
    if (!topSentinel || !viewportElement || !more) return
    const intersectionObserver = new IntersectionObserver(
      ([sentinelEntry]) => {
        if (sentinelEntry.isIntersecting) void older()
      },
      { root: viewportElement, rootMargin: "600px 0px 0px 0px" },
    )
    intersectionObserver.observe(topSentinel)
    return () => intersectionObserver.disconnect()
  }, [more, older, viewport])
  const toTop = () => viewport()?.scrollTo({ top: 0 })
  // La vue descend à l'ouverture, à l'envoi d'un message, et suit la fin tant que l'utilisateur y est. Dès qu'il remonte lire, elle ne
  // bouge plus ; le bouton « Aller à la fin » prend le relais.
  const opened = useRef("") // session déjà amenée à la fin une fois
  const [away, setAway] = useState(false)
  const [fromTop, setFromTop] = useState(false)
  const ready = !!session && msgs !== null
  useEffect(() => {
    const viewportElement = viewport()
    if (!viewportElement) return
    // Collée à la fin tant que l'utilisateur y est : la bande d'état (réflexion, outil, file d'attente) change de hauteur sans que la vue décale.
    let stuck = distanceToEnd(viewportElement) < AT_END_THRESHOLD_PX
    // Boutons « Aller à la fin / au début » : relus aussi à l'ouverture et quand le contenu change, pas seulement au défilement.
    const updateJumpButtons = () => {
      setAway(distanceToEnd(viewportElement) >= AT_END_THRESHOLD_PX)
      setFromTop(viewportElement.scrollTop > FROM_TOP_THRESHOLD_PX)
    }
    updateJumpButtons()
    const onScroll = () => {
      const gap = distanceToEnd(viewportElement)
      stuck = gap < AT_END_THRESHOLD_PX
      updateJumpButtons()
      if (idRef.current && !restoring.current) scrollMem.set(idRef.current, { top: viewportElement.scrollTop, end: gap < viewportElement.clientHeight }) // à moins d'une page de la fin : la réouverture redescend à la fin
    }
    viewportElement.addEventListener("scroll", onScroll, { passive: true })
    const resizeObserver = new ResizeObserver(() => {
      if (stuck && !keepScroll.current && !restoring.current) viewportElement.scrollTop = viewportElement.scrollHeight
      updateJumpButtons()
    })
    if (viewportElement.firstElementChild) resizeObserver.observe(viewportElement.firstElementChild)
    resizeObserver.observe(viewportElement) // la fenêtre, la zone de saisie ou le clavier changent la hauteur visible sans toucher au contenu
    return () => {
      viewportElement.removeEventListener("scroll", onScroll)
      resizeObserver.disconnect()
    }
  }, [viewport, ready])
  useEffect(() => {
    if (!id) opened.current = ""
    // Messages plus anciens chargés : la position reste. Mais jamais avant que la session ait été amenée à la fin (ou à sa position mémorisée) :
    // sinon un chargement déclenché dès l'ouverture sautait l'ancrage et laissait la vue à une demi-ligne de la fin.
    const keepsOlderPagePosition = keepScroll.current && opened.current === id
    keepScroll.current = false
    if (keepsOlderPagePosition) return
    if (!msgs) return
    if (opened.current === id || !viewport()) return
    opened.current = id
    const savedPosition = scrollMem.get(id)
    if (savedPosition && !savedPosition.end) restore(savedPosition.top)
    else pinEnd()
  }, [msgs, pinEnd, restore, viewport, id, ready])
  const inTerm = termOn && view === "terminal" && !!id && actions.live.has(id)
  // Envoi d'un message : on descend pour voir la réponse arriver, puis une seconde fois quand le transcript relu contient le message.
  const sentDown = useCallback(() => {
    toEnd()
    setTimeout(toEnd, SECOND_SCROLL_DELAY_MS)
  }, [toEnd])

  // Contexte du dernier appel : fenêtre de 1 M de tokens si le modèle l'indique ou si on l'a déjà dépassée de 200 k, sinon 200 k.
  const ctx = live.st?.context ?? 0
  const win = windowOf(live.st?.model ?? "", ctx)
  const shouldHintCompact = actions.live.has(id) && ctx >= win * COMPACT_HINT_AT
  const hint = shouldHintCompact ? t("conversation.view.compactHint", { ctx: compact(ctx), pct: Math.round((ctx / win) * 100) }) : null
  // Cache expiré (voir HOT) sur une session qui a déjà un gros contexte : la reprendre le réécrit entièrement au tarif plein.
  const isColdWithLargeContext = !!session && actions.now > 0 && actions.now - session.mtime >= HOT && session.tok.cr + session.tok.cw >= COLD_HINT_MIN_TOKENS
  const cold = isColdWithLargeContext
    ? { text: t("conversation.view.coldHint", { when: ago(session.mtime) }), onNew: () => row && actions.newSession(row.g.project) }
    : null
  const metaExtra = session && (
    <>
      {(live.st?.model || detail?.models.at(-1)) && (
        <Chip asChild>
          <code title={t("conversation.live.model")}>{live.st?.model || detail?.models.at(-1)}</code>
        </Chip>
      )}
      <LiveAgents live={live} dir={dir} id={id} />
      <LiveTasks live={live} dir={dir} id={id} />
      {gitDirs.map((repoDir) => (
        <span key={repoDir} className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs md:shrink-0" title={repoDir}>
          <GitRepo dir={repoDir} st={git.status[repoDir]} chip />
        </span>
      ))}
      <GitRepos dirs={gitSubs} status={git.status} />
    </>
  )
  const moreMenu = row && (
    <SessionDropdown
      row={row}
      after={onClose}
      extra={
        <>
          {session && (
            <DropdownMenuItem onSelect={() => actions.open({ dir: session.dir, id: session.id })}>
              <RefreshCcwDot /> {t("conversation.view.resume")}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => setLoadedOpen(true)} title={t("conversation.view.instructionsTitle")}>
            <FileText /> {t("conversation.view.instructions")}
          </DropdownMenuItem>
          <DropdownMenuGroup className="md:hidden">
            <DropdownMenuCheckboxItem checked={tools} onCheckedChange={(checked) => setTools(checked === true)}>
              {t("conversation.view.showTools")}
            </DropdownMenuCheckboxItem>
            <DropdownMenuItem onSelect={() => setStatsOpen(true)}>
              <BarChart3 /> {t("conversation.live.stats")}
            </DropdownMenuItem>
            {session && <KeepWarmItem s={session} />}
            {actions.live.has(id) && !actions.background.has(id) && (
              <DropdownMenuItem onSelect={() => actions.stop([id], onClose)} title={t("conversation.composer.closeTitle")}>
                <Power /> {t("conversation.composer.close")}
              </DropdownMenuItem>
            )}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
        </>
      }
    >
      <Button variant="ghost" size="icon-sm" aria-label={t("conversation.view.moreActions")}>
        <MoreHorizontal />
      </Button>
    </SessionDropdown>
  )

  return (
    <Dialog open={!!session} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent sheet={false}
        {...swipeHandlers((swipeDirection) => swipeDirection === "right" && onClose(), true)}
        className="safe-t safe-b flex h-[calc(var(--vvh,100dvh)*0.94)] max-md:[&>[data-slot=dialog-close]]:top-[calc(env(safe-area-inset-top)+0.625rem)] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0 max-md:top-[var(--vvt,0px)] max-md:left-0 max-md:h-[var(--vvh,100dvh)] max-md:w-screen max-md:translate-x-0 max-md:translate-y-0 max-md:rounded-none max-md:border-0 sm:max-w-none"
        onCloseAutoFocus={(focusEvent) => focusRow(lastId.current) && focusEvent.preventDefault()}
      >
        <DialogHeader className="gap-1 border-b px-4 pt-2.5 pr-12 pb-3 md:px-6 md:pr-12 md:pb-4">
          <div className="flex items-start gap-4">
            <div className="min-w-0 flex-1 space-y-1">
              <DialogTitle className="truncate text-lg tracking-tight">{session?.title}</DialogTitle>
              <DialogDescription className="sr-only">{t("conversation.view.description")}</DialogDescription>
              {session && <Meta session={session} onFolder={onFolder} extra={metaExtra} />}
            </div>
            {session && (
              <div className="flex shrink-0 flex-col items-end gap-1 text-xs text-muted-foreground">
                <div className="flex min-h-7 items-center gap-2 max-md:hidden">
                  {actions.live.has(id) && !actions.background.has(id) && <CloseSession id={id} onClosed={onClose} />}
                  <KeepWarmButton s={session} />
                  <CacheState s={session} />
                  {actions.background.has(id) ? (
                    <>
                      <BgBadge id={id} />
                      <Button variant="outline" size="xs" className="text-destructive" onClick={() => actions.stop([id])} title={t("shell.list.backgroundHint")}>
                        <Power /> {t("conversation.view.stopBackground")}
                      </Button>
                    </>
                  ) : null}
                </div>
                <div className="flex items-center gap-1 md:-mr-[38px]">
                  <StatsButton live={live} s={session} onClick={() => setStatsOpen(true)} />
                  {row && !row.g.missing && (
                    <Button variant="outline" size="xs" className="max-md:hidden" onClick={() => actions.newSession(row.g.project)} title={t("conversation.view.newSessionTitle")}>
                      <MessageSquarePlus /> {t("conversation.view.newSession")}
                    </Button>
                  )}
                  {moreMenu}
                </div>
              </div>
            )}
          </div>
        </DialogHeader>
        {session && <KeepWarmBanner s={session} />}
        {carousel && (
          <div className="flex flex-wrap items-center gap-2 border-b bg-warning/10 px-4 py-1.5 text-sm">
            <GalleryHorizontalEnd className="size-4 text-warning" aria-hidden /> {t("conversation.view.carousel", { count: carousel.n })}
            <Button variant="outline" size="icon-sm" className="ml-auto" onClick={carousel.prev} title={t("conversation.view.prev")} aria-label={t("conversation.view.prev")}>
              <ChevronLeft />
            </Button>
            <Button variant="outline" size="icon-sm" onClick={carousel.next} title={t("conversation.view.next")} aria-label={t("conversation.view.next")}>
              <ChevronRight />
            </Button>
            <Button variant="ghost" size="sm" onClick={carousel.stop}>
              {t("conversation.view.quitCarousel")}
            </Button>
          </div>
        )}
        {session && <LiveStats live={live} s={session} project={row?.g.project ?? session.cwd} detail={detail} shown={statsOpen} onShown={setStatsOpen} />}
        <div className="relative flex min-h-0 flex-1 flex-col">
          {fromTop && (
            <Button size="sm" variant="secondary" className="absolute top-3 right-4 z-10 rounded-full shadow-md" onClick={toTop} aria-label={t("conversation.view.toTop")}>
              <ArrowUpToLine /> {t("conversation.view.toTop")}
            </Button>
          )}
        <ScrollArea ref={area} className="min-h-0 flex-1">
          <div className="session-text mx-auto max-w-page space-y-6 px-3 py-4 md:px-8 md:py-6">
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
                  {loadingOlder ? <Fan className="animate-spin" /> : <ArrowUpToLine />} {t("conversation.view.loadOlder")}
                </Button>
              </div>
            )}
            {msgs &&
              groupTools(dedupeDocs(msgs)).map((entry, entryIndex) =>
                "count" in entry ? (
                  <ToolAnchor key={`a${entry.from}`} dir={dir} id={id} g={entry} />
                ) : (
                  entry.o === undefined ? (
                    <Message key={entryIndex} m={entry} conv={{ dir, id, tools }} />
                  ) : (
                    <ContextMenu key={`${entry.o}.${entry.n ?? 0}`}>
                      <ContextMenuTrigger asChild className="select-text">
                        <div onContextMenu={(contextMenuEvent) => {
                          const clickedElement = contextMenuEvent.target as HTMLElement
                          setHit({
                            sel: window.getSelection()?.toString() ?? "",
                            code: clickedElement.closest("pre")?.textContent ?? "",
                            link: clickedElement.closest("a")?.href ?? "",
                          })
                        }}>
                          <Message m={entry} conv={{ dir, id, tools }} />
                        </div>
                      </ContextMenuTrigger>
                      <ContextMenuContent>
                        {hit.sel.trim() && (
                          <ContextMenuItem onSelect={() => copy(hit.sel)}>
                            <Copy /> {t("conversation.view.copySelection")}
                          </ContextMenuItem>
                        )}
                        {hit.code && (
                          <ContextMenuItem onSelect={() => copy(hit.code.replace(/\n$/, ""))}>
                            <Copy /> {t("conversation.view.copyBlock")}
                          </ContextMenuItem>
                        )}
                        {hit.link && (
                          <ContextMenuItem onSelect={() => copy(hit.link)}>
                            <Copy /> {t("conversation.view.copyLink")}
                          </ContextMenuItem>
                        )}
                        <ContextMenuItem onSelect={() => copy(entry.t)}>
                          <Copy /> {t("conversation.view.copyMessage")}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem variant="destructive" onSelect={() => rewind(entry.o!)}>
                          <Undo2 /> {t("conversation.view.rewind")}
                        </ContextMenuItem>
                      </ContextMenuContent>
                    </ContextMenu>
                  )
                )
              )}
            {msgs?.length === 0 && !turn && <p className="text-center text-muted-foreground">{t("conversation.view.empty")}</p>}
            {turn?.msgs
              .filter((turnMessage) => tools || turnMessage.r !== "tool")
              .map((turnMessage, turnMessageIndex) => (
                <Message key={`t${turnMessageIndex}`} m={turnMessage} />
              ))}
            {turn?.running && (
              <p role="status" className="flex items-center gap-2 text-muted-foreground">
                <Fan className="size-4 animate-spin" aria-hidden /> {t("conversation.view.replying")}
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
        {inTerm && (
          <div className="flex h-1/2 min-h-48 shrink-0 flex-col border-t">
            <div className="flex items-center border-b bg-muted/40 px-2 py-1">
              <Button variant="secondary" size="xs" onClick={() => setView("visuel")} title={t("conversation.view.modeTitle")}>
                <MessagesSquare /> {t("conversation.view.modeVisual")}
              </Button>
            </div>
            <Suspense fallback={null}>
              <FullTerminal key={id} id={id} />
            </Suspense>
          </div>
        )}
        {session && !inTerm && (
          <Composer
            dir={dir}
            id={id}
            args={args}
            defaultChoice={choice}
            live={actions.live.has(id)}
            liveState={live}
            onSent={() => {
              actions.touch(id)
              setSentAt({ id, at: Date.now() })
              sentDown()
              void reloadRef.current()
              carousel?.next()
            }}
            onWrite={sentDown}
            compactHint={hint}
            coldHint={cold}
            extra={
              <>
                {termOn && actions.live.has(id) && (
                  <Button variant="ghost" size="xs" className="text-muted-foreground max-md:[&>svg]:hidden" onClick={() => setView("terminal")} title={t("conversation.view.modeTitle")}>
                    <SquareTerminal /> {t("conversation.view.modeTerminal")}
                  </Button>
                )}
                {!actions.live.has(id) && (
                  <Button size="xs" variant="ghost" disabled className="text-muted-foreground max-md:[&>svg]:hidden" title={t("conversation.composer.rawScreenTitle")}>
                    <Bug /> {t("conversation.composer.rawScreen")}
                  </Button>
                )}
                <Button variant={tools ? "secondary" : "ghost"} size="xs" className="text-muted-foreground max-md:hidden" onClick={() => setTools(!tools)} aria-pressed={tools}>
                  <Wrench /> {tools ? t("conversation.view.toolsShown") : t("conversation.view.toolsHidden")}
                </Button>
              </>
            }
            turn={turn}
            setTurn={(update) => setChat((currentChat) => update(currentChat?.id === id ? currentChat : null))}
            // Le transcript contient maintenant le tour : on le relit, et le tour affiché à part disparaît (sauf erreur).
            onDone={async () => {
              // Le tour affiché à part ne disparaît que lorsque le transcript relu le contient (Claude l'écrit parfois avec retard) :
              // sinon la réponse s'effaçait un instant, voire jusqu'à la relecture suivante.
              const sent = turn?.msgs.find((turnMessage) => turnMessage.r === "user")?.t.trim()
              for (let attempt = 0; attempt < TURN_RELOAD_ATTEMPTS; attempt++) {
                const reloadedMessages = await reload()
                if (reloadedMessages === null || !sent) break
                const sentPrefix = sent.slice(0, SENT_PREFIX_LENGTH)
                const transcriptHasSent = reloadedMessages
                  .slice(-RECENT_MESSAGES_CHECKED)
                  .some((reloadedMessage) => reloadedMessage.r === "user" && reloadedMessage.t.includes(sentPrefix))
                if (transcriptHasSent) break
                await new Promise((resolve) => setTimeout(resolve, TURN_RELOAD_DELAY_MS))
              }
              setChat((currentChat) => (currentChat?.id === id && !currentChat.error ? { ...currentChat, msgs: [] } : currentChat))
            }}
          />
        )}
        <Confirm ask={ask} onClose={() => setAsk(null)} />
        <LoadedDialog id={id} home={actions.home} open={loadedOpen} onClose={() => setLoadedOpen(false)} />
      </DialogContent>
    </Dialog>
  )
}
