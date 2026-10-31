import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { resolveFront, setLang } from "@/i18n"
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react"
import { useLocation, useSearchParams } from "wouter"
import { SessionSearch } from "@/components/SessionSearch"
import { type Group, type VGroup } from "@/lib/api"
import { stepIn } from "@/lib/carousel"
import { body } from "@/lib/commands"
import { href, parse, type Route } from "@/lib/route"
import { buildTree, find, under } from "@/lib/tree"
import { tilde } from "@/lib/format"
import { useLocal } from "@/hooks/useLocal"
import { useAppActions } from "@/hooks/useAppActions"
import { useCacheWarning } from "@/hooks/useCacheWarning"
import { useAppData } from "@/hooks/useAppData"
import { useSearchHits } from "@/hooks/useSearchHits"
import { useShortcuts } from "@/hooks/useShortcuts"
import { useWaitingNotice } from "@/hooks/useWaitingNotice"
import { useUpdateNotice } from "@/hooks/useUpdateNotice"
import { BanAlert } from "@/components/BanAlert"
import { applyFont } from "@/lib/fonts"
import { type Choice } from "@/lib/models"
import { asked, buildView, byDate, groupKey, HOT, isAuto, WEEK } from "@/lib/sessionsView"
import { RouteErrorBoundary } from "@/components/RouteErrorBoundary"
import { retryLazy } from "@/lib/retryLazy"
import { Skeleton } from "@/components/ui/skeleton"
import { ActionsCtx, type Row } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { PageHeader } from "@/components/PageHeader"
import { SessionInfo } from "@/components/SessionInfo"
import { ShareDialog } from "@/components/ShareDialog"
import { Prompt, type Ask as PromptAsk } from "@/components/Prompt"
import { SessionSelectionBar } from "@/components/SessionSelectionBar"
import { SessionsPane } from "@/components/SessionsPane"
import { MD, Sidebar } from "@/components/Sidebar"
import { swipeHandlers } from "@/lib/swipe"
import { Palette } from "@/components/Palette"
import { Dashboard } from "@/components/Dashboard"
import { segs } from "@/lib/paths"
import { Compose, type ComposeAsk } from "@/components/Compose"
import { AuthScreen } from "@/components/auth/Login"
import { boot, useAuth } from "@/lib/auth"
import { importLocalQueues } from "@/hooks/importQueues"

// Les grandes vues se chargent à la demande : le premier chargement ne porte que la liste des sessions.
const Conversation = lazy(() => import("@/components/Conversation").then((componentModule) => ({ default: componentModule.Conversation })))
const Snippets = retryLazy(() => import("@/components/Snippets").then((componentModule) => ({ default: componentModule.Snippets })))
const Extensions = retryLazy(() => import("@/components/Extensions").then((componentModule) => ({ default: componentModule.Extensions })))
const TeamRun = retryLazy(() => import("@/components/teams/TeamRun").then((componentModule) => ({ default: componentModule.TeamRun })))
const TeamsPage = retryLazy(() => import("@/components/teams/TeamsPage").then((componentModule) => ({ default: componentModule.TeamsPage })))
const Config = retryLazy(() => import("@/components/Config").then((componentModule) => ({ default: componentModule.Config })))
const Skills = retryLazy(() => import("@/components/Skills").then((componentModule) => ({ default: componentModule.Skills })))
const Memory = retryLazy(() => import("@/components/Memory").then((componentModule) => ({ default: componentModule.Memory })))
const IdeView = retryLazy(() => import("@/components/ide/IdeView").then((componentModule) => ({ default: componentModule.IdeView })))
const Tabs = retryLazy(() => import("@/components/Tabs").then((componentModule) => ({ default: componentModule.Tabs })))

const MIN_CONTENT_SEARCH_LENGTH = 2
const ROUTES_WITHOUT_CONTENT_SEARCH: Route["kind"][] = ["snippets", "memory", "tabs", "config", "skills"]
const ROUTES_WITH_SEARCH_BOX: Route["kind"][] = ["recent", "all", "active", "auto", "project", "group", "dir", "memory"]
const ROUTES_WITH_SPLIT_LAYOUT: Route["kind"][] = ["skills", "memory", "snippets"]

// Mode multi-utilisateur : l'application ne se monte qu'une session ouverte. Une session perdue en cours d'usage la laisse
// montée sous l'écran de connexion, pour retrouver vues et brouillons après reconnexion du même compte ; la clé owner la
// remonte à neuf si un autre compte se connecte.
export default function App() {
  const auth = useAuth()
  useEffect(() => void boot(), [])
  if (auth.state === "loading") return null
  const isSessionOpen = auth.state === "open" || auth.state === "ok"
  const shouldMountShell = isSessionOpen || auth.expired
  return (
    <>
      {shouldMountShell && <Shell key={auth.owner ?? ""} />}
      {!isSessionOpen && <AuthScreen />}
    </>
  )
}

// Application : état de navigation (route, recherche, sélection, réglages) et assemblage. Les données viennent de
// useAppData, les actions de useAppActions, les raccourcis de useShortcuts.
function Shell() {
  const [currentPath, navigate] = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const route = parse(currentPath)
  // String() : le linter du compilateur React tient sinon searchQuery pour liée à searchParams (mutable) et refuse les useMemo qui en dépendent.
  const searchQuery = String(searchParams.get("q") ?? "")
  const readingSessionId = searchParams.get("s")

  const { data, setData, snips, now, reload, error, denied } = useAppData()
  const { t } = useTranslation() // re-rend toute l'application quand la langue change
  const serverLang = data?.lang
  useEffect(() => setLang(resolveFront(serverLang, navigator.language)), [serverLang])
  const [picked, setPicked] = useState(() => new Set<string>())
  const [args, setArgs] = useLocal("ccArgs", "")
  const [hideGrouped, setHideGrouped] = useLocal("ccHideGrouped", "") // vue d'un dossier : masquer les sessions déjà groupées
  // Modèle et effort par défaut des nouveaux prompts (Réglages), modifiables à chaque prompt.
  const [model, setModel] = useLocal("ccModel", "")
  const [effort, setEffort] = useLocal("ccEffort", "")
  const choice: Choice = { model, effort }
  const setChoice = (nextChoice: Choice) => {
    setModel(nextChoice.model)
    setEffort(nextChoice.effort)
  }
  const [font, setFont] = useLocal("ccFont", "geist")
  const [fontSize, setFontSize] = useLocal("ccFontSize", "15")
  useEffect(() => applyFont(font, fontSize), [font, fontSize])
  useEffect(() => void importLocalQueues(), []) // anciennes files locales : reprises une fois par le serveur
  const [picker, setPicker] = useState<Pick | null>(null)
  const [navOpen, setNavOpen] = useState(false) // mobile : tiroir du menu
  const [ask, setAsk] = useState<Ask | null>(null)
  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
  const [compose, setCompose] = useState<ComposeAsk | null>(null)
  const [info, setInfo] = useState<Row | null>(null)
  const [share, setShare] = useState<Row | null>(null)
  const searchInputRef = useRef<HTMLInputElement>(null)

  const setSearchQuery = (nextQuery: string) =>
    setSearchParams(
      (nextParams) => {
        if (nextQuery) nextParams.set("q", nextQuery)
        else nextParams.delete("q")
        return nextParams
      },
      { replace: true }
    )
  // Changer de vue garde la recherche en cours.
  const advancedSearch = searchParams.get("qa") === "1" // recherche avancée : aussi dans le contenu des conversations
  const setAdvancedSearch = (isAdvanced: boolean) =>
    setSearchParams(
      (nextParams) => {
        if (isAdvanced) nextParams.set("qa", "1")
        else nextParams.delete("qa")
        return nextParams
      },
      { replace: true }
    )
  const goToRoute = (targetRoute: Route) => {
    let searchSuffix = ""
    if (searchQuery) {
      const advancedSuffix = advancedSearch ? "&qa=1" : ""
      searchSuffix = `?q=${encodeURIComponent(searchQuery)}${advancedSuffix}`
    }
    navigate(href(targetRoute) + searchSuffix)
  }

  const trimmedQuery = searchQuery.trim()
  const hitMap = useSearchHits(advancedSearch ? trimmedQuery : "")
  const isContentQuery = advancedSearch && trimmedQuery.length >= MIN_CONTENT_SEARCH_LENGTH
  const isAwaitingHits = isContentQuery && !hitMap
  const isSearchingContent = isAwaitingHits && !ROUTES_WITHOUT_CONTENT_SEARCH.includes(route.kind)

  const view = useMemo(() => buildView(data, trimmedQuery, hitMap, advancedSearch), [data, trimmedQuery, hitMap, advancedSearch])

  // Toutes les sessions, même filtrées : le lecteur reste ouvert quand la recherche change.
  const allRowsById = useMemo(() => {
    const rowsById = new Map<string, Row>()
    for (const group of data?.groups ?? []) {
      for (const session of group.sessions) rowsById.set(session.id, { s: session, g: group })
    }
    return rowsById
  }, [data])

  const home = data?.home ?? ""
  // La barre latérale ne suit pas la recherche : elle montre toujours toutes les sessions, la recherche ne filtre que la vue affichée.
  const unfilteredView = useMemo(() => buildView(data, "", null), [data])
  const filteredTree = useMemo(() => buildTree(view.entries, home), [view.entries, home])
  // « Sessions locales » : seulement les dossiers qui ne sont dans aucun projet (ils ont leur place sous « Projets »).
  const projects = data?.projects
  const localTree = useMemo(() => {
    const dirsInProjects = new Set(projects?.flatMap((projectItem) => projectItem.dirs))
    const entriesOutsideProjects = unfilteredView.entries.filter((entry) => !dirsInProjects.has(entry.g.project))
    return buildTree(entriesOutsideProjects, home)
  }, [projects, unfilteredView.entries, home])
  const liveIds = useMemo(() => new Set(data?.live), [data])
  const waitingIds = useMemo(() => new Set(data?.waiting), [data])
  const [carousel, setCarousel] = useState(false) // mode carrousel : on parcourt les sessions qui attendent une réponse
  const virtualGroups = useMemo(() => data?.vgroups ?? [], [data])
  const favoriteIds = useMemo(() => new Set(data?.favs), [data])
  const askingIds = useMemo(() => new Set(data?.asking), [data])
  useWaitingNotice(waitingIds, askingIds, allRowsById)
  useUpdateNotice()

  const virtualGroupKey = (virtualGroup: VGroup) => groupKey(virtualGroup, home)
  // Listes dérivées mémoïsées : un rendu sans nouvelles données (sélection, menu, saisie) ne les recalcule pas.
  const recentRows = useMemo(() => view.rows.filter((row) => now - row.s.mtime < WEEK), [view.rows, now])
  const hotRows = useMemo(() => {
    const isHotSession = (session: Row["s"]) => {
      const isLiveOrWarm = liveIds.has(session.id) || now - session.mtime < HOT
      return isLiveOrWarm && !isAuto(session)
    }
    const allGroups = data?.groups ?? []
    const rowsOfHotSessions = allGroups.flatMap((group) => group.sessions.filter(isHotSession).map((session) => ({ s: session, g: group })))
    return rowsOfHotSessions.sort(byDate)
  }, [data, now, liveIds])
  // Ouvertes d'abord, puis au cache chaud (récentes d'abord) ; la recherche s'applique comme ailleurs.
  const activeRows = useMemo(() => {
    const isLiveOrWarm = (row: Row) => liveIds.has(row.s.id) || now - row.s.mtime < HOT
    const liveFirstThenMostRecentlyAsked = (firstRow: Row, secondRow: Row) => {
      const liveDifference = +liveIds.has(secondRow.s.id) - +liveIds.has(firstRow.s.id)
      return liveDifference || asked(secondRow.s) - asked(firstRow.s)
    }
    return view.rows.filter(isLiveOrWarm).sort(liveFirstThenMostRecentlyAsked)
  }, [view.rows, liveIds, now])
  const selectedRows = [...picked].map((sessionId) => view.byId.get(sessionId)).filter((row): row is Row => !!row)

  const selectedDirNode = route.kind === "dir" ? find(filteredTree, route.key) : null
  // Sessions du dossier affiché qui appartiennent déjà à au moins un de ses groupes.
  const groupsOfSelectedDir = selectedDirNode?.g ? virtualGroups.filter((virtualGroup) => virtualGroup.project === selectedDirNode.g!.project) : []
  const groupedSessionIds = new Set(groupsOfSelectedDir.flatMap((virtualGroup) => virtualGroup.ids))
  const groupedSessionCount = selectedDirNode?.ss.filter((session) => groupedSessionIds.has(session.id)).length ?? 0
  const currentVirtualGroup =
    route.kind === "group" ? virtualGroups.find((virtualGroup) => virtualGroup.name === route.name && virtualGroupKey(virtualGroup) === route.key) : undefined
  const currentProject = route.kind === "project" ? (data?.projects ?? []).find((projectItem) => projectItem.name === route.name) : undefined

  const computeVisibleRows = (): Row[] => {
    switch (route.kind) {
      case "recent":
        return recentRows
      case "project":
        return view.rows.filter((row) => currentProject?.dirs.includes(row.g.project)).sort(byDate)
      case "group":
        return (currentVirtualGroup?.ids ?? [])
          .map((sessionId) => view.byId.get(sessionId))
          .filter((row): row is Row => !!row)
          .sort(byDate)
      case "dir": {
        const shouldHideGrouped = !!hideGrouped && !trimmedQuery
        const isShown = (session: Row["s"]) => !(shouldHideGrouped && groupedSessionIds.has(session.id))
        return (selectedDirNode?.ss ?? []).filter(isShown).map((session) => ({ s: session, g: selectedDirNode!.g! }))
      }
      case "all":
        return view.rows
      case "active":
        return activeRows
      case "auto":
        return view.autoRows
      default:
        return []
    }
  }
  const visibleRows = computeVisibleRows()
  // Sessions des sous-dossiers : affichées à part, sous la liste du dossier sélectionné.
  const subfolderRows = selectedDirNode ? [...selectedDirNode.kids.values()].flatMap(under).sort(byDate) : []

  const openSession = (session: { id: string }) =>
    setSearchParams((nextParams) => {
      nextParams.set("s", session.id)
      return nextParams
    })
  // Sessions ouvertes et au cache chaud (actives) qui attendent, depuis le plus longtemps d'abord ; les froides sont écartées.
  const waitingRows = [...waitingIds]
    .filter((sessionId) => liveIds.has(sessionId))
    .map((sessionId) => allRowsById.get(sessionId))
    .filter((row): row is Row => !!row && now - row.s.mtime < HOT)
    .sort((firstRow, secondRow) => firstRow.s.mtime - secondRow.s.mtime)
  const liveSessions = [...liveIds]
    .map((sessionId) => allRowsById.get(sessionId))
    .filter((row): row is Row => !!row)
    .map((row) => row.s)
  useCacheWarning(liveSessions, now, (sessionId) => openSession({ id: sessionId }))
  const stepWaiting = (direction: 1 | -1) => stepIn(waitingRows, (row) => row.s.id === readingSessionId, direction)
  const startCarousel = () => {
    if (!waitingRows.length) return void toast.info(t("shell.app.noneWaiting"))
    setCarousel(true)
    openSession(waitingRows[0].s)
  }
  const advanceCarousel = (direction: 1 | -1 = 1) => {
    const nextRow = stepWaiting(direction)
    if (nextRow) {
      openSession(nextRow.s)
      return
    }
    setCarousel(false)
    toast.info(t("shell.app.noMoreWaiting"))
  }
  // La session affichée par le carrousel a été supprimée (ou a disparu) : on passe à la suivante, ou on sort du mode.
  const isReadingSessionGone = !!data && !!readingSessionId && !allRowsById.has(readingSessionId)
  useEffect(() => {
    if (!carousel || !isReadingSessionGone) return
    const advanceTimeoutId = setTimeout(() => advanceCarousel(1), 0)
    return () => clearTimeout(advanceTimeoutId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [carousel, isReadingSessionGone])
  const closeReader = () => {
    setCarousel(false)
    setSearchParams(
      (nextParams) => {
        nextParams.delete("s")
        return nextParams
      },
      { replace: true }
    )
  }

  const { actions, groupActs, groupRows, targets, newSession, purgeAuto } = useAppActions({
    data,
    setData,
    reload,
    home,
    q: searchQuery,
    go: goToRoute,
    read: openSession,
    now,
    live: liveIds,
    waiting: waitingIds,
    hitMap,
    vgroups: virtualGroups,
    favs: favoriteIds,
    vg: currentVirtualGroup,
    picked,
    setPicked,
    selected: selectedRows,
    everyId: allRowsById,
    view,
    args,
    choice,
    snippets: snips?.files ?? [],
    setAsk,
    setPrompt,
    setCompose,
    setInfo,
    setShare,
    setPicker,
  })
  useShortcuts({ search: searchInputRef, picked, clearPicked: () => setPicked(new Set()), byId: view.byId, actions })
  const [palette, setPalette] = useState(false)
  useEffect(() => {
    const togglePaletteOnShortcut = (keyboardEvent: KeyboardEvent) => {
      const hasCommandModifier = keyboardEvent.ctrlKey || keyboardEvent.metaKey
      const isPaletteShortcut = hasCommandModifier && !keyboardEvent.altKey && keyboardEvent.key.toLowerCase() === "k"
      if (!isPaletteShortcut) return
      keyboardEvent.preventDefault()
      setPalette((isOpen) => !isOpen)
    }
    window.addEventListener("keydown", togglePaletteOnShortcut)
    return () => window.removeEventListener("keydown", togglePaletteOnShortcut)
  }, [])

  const currentGroup: Group | null = selectedDirNode?.g ?? null
  const ownSessions = currentGroup ? selectedDirNode!.ss.map((session) => ({ dir: session.dir, id: session.id })) : []
  // Le lecteur garde la dernière version connue de la session affichée : si une relecture de la liste ne la contient pas un instant
  // (écriture en cours, erreur passagère), la conversation ne se vide pas ni ne se ferme. Une vraie disparition (suppression) est
  // traitée à part (`isReadingSessionGone`).
  const foundReaderRow = readingSessionId ? (allRowsById.get(readingSessionId) ?? null) : null
  const [keptReaderRow, setKeptReaderRow] = useState<Row | null>(null)
  if (foundReaderRow && foundReaderRow !== keptReaderRow) setKeptReaderRow(foundReaderRow)
  const resolveReaderRow = (): Row | null => {
    if (!readingSessionId) return null
    if (foundReaderRow) return foundReaderRow
    const isKeptRowForReadingSession = keptReaderRow?.s.id === readingSessionId
    return isKeptRowForReadingSession ? keptReaderRow : null
  }
  const readerRow = resolveReaderRow()
  const favRows = useMemo(
    () => [...favoriteIds].map((sessionId) => allRowsById.get(sessionId)).filter((row): row is Row => !!row),
    [favoriteIds, allRowsById]
  )
  const favGroups = virtualGroups
    .filter((virtualGroup) => virtualGroup.fav)
    .sort((firstGroup, secondGroup) => firstGroup.name.localeCompare(secondGroup.name))
    .map((virtualGroup) => ({ vg: virtualGroup, key: virtualGroupKey(virtualGroup) }))

  // Récentes : regroupées par projet, chaque projet pouvant être repris d'un coup.
  const isRecent = route.kind === "recent"
  const recentRowsByProject = useMemo(() => {
    const rowsByProject = new Map<string, Row[]>()
    if (!isRecent) return rowsByProject
    for (const row of recentRows) rowsByProject.set(row.g.project, [...(rowsByProject.get(row.g.project) ?? []), row])
    return rowsByProject
  }, [isRecent, recentRows])

  const pageTitle = {
    recent: t("shell.app.title.recent"),
    home: t("shell.app.title.home"),
    all: t("shell.app.title.all"),
    active: t("shell.app.title.active"),
    auto: t("shell.app.title.auto"),
    group: route.kind === "group" ? route.name : "",
    project: route.kind === "project" ? route.name : "",
    snippets: t("shell.app.title.snippets"),
    memory: t("shell.app.title.memory"),
    config: t("shell.app.title.config"),
    extensions: t("shell.app.title.extensions"),
    teams: t("teams.title"),
    team: t("teams.title"),
    skills: t("shell.app.title.skills"),
    tabs: t("shell.app.title.tabs"),
    workspace: t("ide.title"),
    projectConfig: t("shell.app.title.projectConfig"),
    dir: selectedDirNode?.name ?? t("shell.app.title.dir"),
  }[route.kind]
  const formatSessionCount = (count: number) => t("shell.count.sessions", { count })
  const computePageSubtitle = (): string => {
    switch (route.kind) {
      case "recent":
        return t("shell.app.sub.recent", {
          sessions: formatSessionCount(recentRows.length),
          projects: t("shell.count.projects", { count: recentRowsByProject.size }),
        })
      case "project":
        return t("shell.app.sub.project", {
          dirs: t("shell.count.dirs", { count: currentProject?.dirs.length ?? 0 }),
          sessions: formatSessionCount(visibleRows.length),
        })
      case "active": {
        const openCount = activeRows.filter((row) => liveIds.has(row.s.id)).length
        const warmCount = activeRows.filter((row) => !liveIds.has(row.s.id)).length
        return t("shell.app.sub.active", {
          open: t("shell.count.open", { count: openCount }),
          warm: t("shell.count.warm", { count: warmCount }),
        })
      }
      case "auto":
        return t("shell.app.sub.auto")
      case "skills":
        return t("shell.app.sub.skills")
      case "extensions":
        return t("shell.app.sub.extensions")
      case "config":
        return t("shell.app.sub.config")
      case "snippets":
        return snips?.dir ?? ""
      case "memory":
        return t("shell.app.sub.memory")
      case "workspace":
        return tilde(route.dir, home)
      case "projectConfig": {
        const configuredGroup = data?.groups.find((group) => group.sessions[0]?.dir === route.dir)
        return tilde(configuredGroup?.project ?? route.dir, home)
      }
      case "tabs":
        return t("shell.app.sub.tabs")
      case "group": {
        const groupDir = currentVirtualGroup ? tilde(currentVirtualGroup.project, home) : "?"
        return t("shell.app.sub.group", { dir: groupDir, sessions: formatSessionCount(visibleRows.length) })
      }
      default:
        if (currentGroup) return t("shell.app.sub.dir", { dir: tilde(currentGroup.project, home), sessions: formatSessionCount(visibleRows.length) })
        return formatSessionCount(visibleRows.length)
    }
  }
  const pageSubtitle = computePageSubtitle()

  const openNavOnRightSwipe = (direction: "left" | "right") => {
    const isDesktopWidth = matchMedia(MD).matches
    if (direction === "right" && !isDesktopWidth) setNavOpen(true)
  }
  const uiSettings = { args, setArgs, choice, setChoice, font, setFont, fontSize, setFontSize }

  const renderMainContent = () => {
    switch (route.kind) {
      case "home":
        return (
          <Dashboard
            open={activeRows.filter((row) => liveIds.has(row.s.id))}
            closed={activeRows.filter((row) => !liveIds.has(row.s.id))}
            waiting={waitingIds}
            now={now}
            home={home}
            go={goToRoute}
            read={openSession}
          />
        )
      case "workspace":
        return <IdeView key={route.dir} dir={route.dir} />
      case "tabs":
        return <Tabs args={args} onAsk={setAsk} onPrompt={setPrompt} />
      case "skills":
        return <Skills sel={route.key} home={home} q={trimmedQuery} onPick={(key) => goToRoute({ kind: "skills", key })} onAsk={setAsk} />
      case "projectConfig":
        return <Config key={route.dir} dir={route.dir} home={home} tab="" onTab={() => goToRoute({ kind: "config", tab: "permissions" })} ui={uiSettings} />
      case "teams":
        return <TeamsPage home={home} />
      case "team":
        return <TeamRun key={route.name} name={route.name} />
      case "extensions":
        return <Extensions home={home} onAsk={setAsk} />
      case "config":
        return <Config home={home} tab={route.tab} onTab={(tab) => goToRoute({ kind: "config", tab })} ui={uiSettings} />
      case "memory":
        return (
          <Memory sel={route.key} home={home} q={trimmedQuery} onPick={(key) => goToRoute({ kind: "memory", key })} onAsk={setAsk} onCompose={setCompose} args={args} />
        )
      case "snippets":
        if (!snips) return <Skeleton className="m-6 h-32" />
        return (
          <Snippets
            files={snips.files}
            scopes={snips.scopes}
            dir={snips.dir}
            sel={route.path}
            q={trimmedQuery}
            home={home}
            onPick={(key) => goToRoute({ kind: "snippets", path: key })}
            onLaunch={(snippet, text) => {
              // Le dossier du projet de la commande d'abord, sinon le plus récent.
              const isSnippetProject = (target: { project: string }) => Number(target.project === snippet.project)
              const orderedTargets = targets().sort((firstTarget, secondTarget) => isSnippetProject(secondTarget) - isSnippetProject(firstTarget))
              newSession(t("shell.app.snippetSession"), snippet.command, orderedTargets, body(text) + "\n\n")
            }}
            onChanged={reload}
            onAsk={setAsk}
            onPrompt={setPrompt}
            onCompose={setCompose}
            args={args}
          />
        )
      default:
        return (
          <SessionsPane
            loading={data === null}
            route={route}
            rows={visibleRows}
            subs={subfolderRows}
            byProject={recentRowsByProject}
            qt={trimmedQuery}
            vg={currentVirtualGroup}
            dirMissing={!selectedDirNode}
            projectDir={visibleRows[0]?.s.dir ?? selectedDirNode?.ss[0]?.dir}
            project={selectedDirNode?.g?.project}
            home={home}
            live={liveIds}
            waiting={waitingIds}
            actions={actions}
            go={goToRoute}
            clearSearch={() => setSearchQuery("")}
            grouped={{ n: groupedSessionCount, hide: !!hideGrouped, setHide: (shouldHide) => setHideGrouped(shouldHide ? "1" : "") }}
            error={error}
            denied={denied}
            retry={() => void reload().catch(() => {})}
            onRefresh={() => reload().catch(() => {})}
          />
        )
    }
  }

  const carouselControls = carousel
    ? { n: waitingRows.length, next: () => advanceCarousel(1), prev: () => advanceCarousel(-1), stop: () => setCarousel(false) }
    : undefined
  const openReaderFolder = readerRow ? () => goToRoute({ kind: "dir", key: segs(readerRow.g.project, home).join("/") }) : undefined

  return (
    <ActionsCtx.Provider value={actions}>
      <div className="safe-t safe-x flex h-dvh flex-col md:flex-row">
        <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-background focus:px-3 focus:py-2 focus:text-sm focus:ring-3 focus:ring-ring/80">
          {t("shell.header.skipToContent")}
        </a>
        <Sidebar
          open={navOpen}
          onClose={() => setNavOpen(false)}
          loading={data === null}
          total={view.total}
          searchRef={searchInputRef}
          route={route}
          go={(targetRoute) => {
            setNavOpen(false)
            goToRoute(targetRoute)
          }}
          counts={{ all: unfilteredView.rows.length, active: unfilteredView.rows.filter((row) => liveIds.has(row.s.id) || now - row.s.mtime < HOT).length }}
          reload={reload}
          favRows={favRows}
          favGroups={favGroups}
          groupCount={(virtualGroup) => groupRows(virtualGroup).length}
          read={(session) => {
            setNavOpen(false)
            openSession(session)
          }}
          tree={localTree}
          hot={hotRows}
          carousel={{ n: waitingRows.length, on: carousel, start: startCarousel }}
          now={now}
          ttl={HOT}
          snippets={snips ? snips.files.length : null}
          auto={{ n: view.autoItems.length, purge: purgeAuto, on: route.kind === "auto" }}
          args={args}
        />

        <main id="main" tabIndex={-1} className="relative flex min-h-0 min-w-0 flex-1 flex-col outline-none" {...swipeHandlers(openNavOnRightSwipe, true)}>
          <PageHeader
            onMenu={() => setNavOpen(true)}
            title={pageTitle}
            sub={pageSubtitle}
            qt={trimmedQuery}
            route={route}
            g={currentGroup}
            vg={currentVirtualGroup}
            firstDir={selectedDirNode?.ss[0]?.dir}
            own={ownSessions}
            rowCount={visibleRows.length}
            actions={actions}
            groupActs={groupActs}
            split={ROUTES_WITH_SPLIT_LAYOUT.includes(route.kind)}
            reload={reload}
            searchBox={
              ROUTES_WITH_SEARCH_BOX.includes(route.kind) ? (
                <SessionSearch q={searchQuery} setQ={setSearchQuery} adv={advancedSearch} setAdv={setAdvancedSearch} searching={isSearchingContent} memory={route.kind === "memory"} />
              ) : undefined
            }
          />

          <RouteErrorBoundary key={route.kind}>
            <Suspense fallback={<Skeleton className="m-6 h-32" />}>{renderMainContent()}</Suspense>
          </RouteErrorBoundary>

          {selectedRows.length > 0 && (
            <SessionSelectionBar
              selected={selectedRows}
              onSelectAll={() => setPicked(new Set([...picked, ...[...visibleRows, ...subfolderRows].map((row) => row.s.id)]))}
              onClear={() => setPicked(new Set())}
            />
          )}
        </main>
      </div>

      <Suspense fallback={null}>
        <Conversation row={readerRow} onClose={closeReader} onFolder={openReaderFolder} args={args} choice={choice} carousel={carouselControls} />
      </Suspense>
      <DirPicker pick={picker} home={home} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
      <BanAlert />
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
      <Compose ask={compose} onClose={() => setCompose(null)} />
      <SessionInfo row={info} onClose={() => setInfo(null)} />
      {share && <ShareDialog key={share.s.id} id={share.s.id} dir={share.s.dir} title={share.s.title} onClose={() => setShare(null)} />}
      <Palette open={palette} onOpenChange={setPalette} rows={[...allRowsById.values()]} home={home} go={goToRoute} read={openSession} />
    </ActionsCtx.Provider>
  )
}
