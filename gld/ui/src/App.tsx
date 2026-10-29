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
import { useAppData } from "@/hooks/useAppData"
import { useSearchHits } from "@/hooks/useSearchHits"
import { useShortcuts } from "@/hooks/useShortcuts"
import { useWaitingNotice } from "@/hooks/useWaitingNotice"
import { useUpdateNotice } from "@/hooks/useUpdateNotice"
import { BanAlert } from "@/components/BanAlert"
import { applyFont } from "@/lib/fonts"
import { type Choice } from "@/lib/models"
import { buildView, byDate, groupKey, HOT, isAuto, WEEK } from "@/lib/sessionsView"
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
import { Sidebar } from "@/components/Sidebar"
import { segs } from "@/lib/paths"
import { Compose, type ComposeAsk } from "@/components/Compose"
import { AuthScreen } from "@/components/auth/Login"
import { boot, useAuth } from "@/lib/auth"
import { importLocalQueues } from "@/hooks/importQueues"

// Les grandes vues se chargent à la demande : le premier chargement ne porte que la liste des sessions.
const Conversation = lazy(() => import("@/components/Conversation").then((m) => ({ default: m.Conversation })))
const Snippets = retryLazy(() => import("@/components/Snippets").then((m) => ({ default: m.Snippets })))
const Extensions = retryLazy(() => import("@/components/Extensions").then((m) => ({ default: m.Extensions })))
const TeamRun = retryLazy(() => import("@/components/teams/TeamRun").then((m) => ({ default: m.TeamRun })))
const TeamsPage = retryLazy(() => import("@/components/teams/TeamsPage").then((m) => ({ default: m.TeamsPage })))
const Config = retryLazy(() => import("@/components/Config").then((m) => ({ default: m.Config })))
const Skills = retryLazy(() => import("@/components/Skills").then((m) => ({ default: m.Skills })))
const Memory = retryLazy(() => import("@/components/Memory").then((m) => ({ default: m.Memory })))
const IdeView = retryLazy(() => import("@/components/ide/IdeView").then((m) => ({ default: m.IdeView })))
const Tabs = retryLazy(() => import("@/components/Tabs").then((m) => ({ default: m.Tabs })))

// Mode multi-utilisateur : l'application ne se monte qu'une session ouverte. Une session perdue en cours d'usage la laisse
// montée sous l'écran de connexion, pour retrouver vues et brouillons après reconnexion du même compte ; la clé owner la
// remonte à neuf si un autre compte se connecte.
export default function App() {
  const a = useAuth()
  useEffect(() => void boot(), [])
  if (a.state === "loading") return null
  const open = a.state === "open" || a.state === "ok"
  return (
    <>
      {(open || a.expired) && <Shell key={a.owner ?? ""} />}
      {!open && <AuthScreen />}
    </>
  )
}

// Application : état de navigation (route, recherche, sélection, réglages) et assemblage. Les données viennent de
// useAppData, les actions de useAppActions, les raccourcis de useShortcuts.
function Shell() {
  const [loc, nav] = useLocation()
  const [params, setParams] = useSearchParams()
  const route = parse(loc)
  // String() : le linter du compilateur React tient sinon q pour liée à params (mutable) et refuse les useMemo qui en dépendent.
  const q = String(params.get("q") ?? "")
  const reading = params.get("s")

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
  const setChoice = (c: Choice) => {
    setModel(c.model)
    setEffort(c.effort)
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
  const search = useRef<HTMLInputElement>(null)

  const setQ = (v: string) =>
    setParams(
      (p) => {
        if (v) p.set("q", v)
        else p.delete("q")
        return p
      },
      { replace: true }
    )
  // Changer de vue garde la recherche en cours.
  const adv = params.get("qa") === "1" // recherche avancée : aussi dans le contenu des conversations
  const setAdv = (v: boolean) =>
    setParams(
      (p) => {
        if (v) p.set("qa", "1")
        else p.delete("qa")
        return p
      },
      { replace: true }
    )
  const go = (r: Route) => nav(href(r) + (q ? `?q=${encodeURIComponent(q)}${adv ? "&qa=1" : ""}` : ""))

  const qt = q.trim()
  const hitMap = useSearchHits(adv ? qt : "")
  const searching = adv && qt.length >= 2 && !hitMap && route.kind !== "snippets" && route.kind !== "memory" && route.kind !== "tabs" && route.kind !== "config" && route.kind !== "skills"

  const view = useMemo(() => buildView(data, qt, hitMap, adv), [data, qt, hitMap, adv])

  // Toutes les sessions, même filtrées : le lecteur reste ouvert quand la recherche change.
  const everyId = useMemo(() => {
    const m = new Map<string, Row>()
    for (const g of data?.groups ?? []) for (const s of g.sessions) m.set(s.id, { s, g })
    return m
  }, [data])

  const home = data?.home ?? ""
  // La barre latérale ne suit pas la recherche : elle montre toujours toutes les sessions, la recherche ne filtre que la vue affichée.
  const base = useMemo(() => buildView(data, "", null), [data])
  const tree = useMemo(() => buildTree(view.entries, home), [view.entries, home])
  // « Sessions locales » : seulement les dossiers qui ne sont dans aucun projet (ils ont leur place sous « Projets »).
  const projects = data?.projects
  const localTree = useMemo(() => {
    const inProject = new Set(projects?.flatMap((p) => p.dirs))
    return buildTree(base.entries.filter((e) => !inProject.has(e.g.project)), home)
  }, [projects, base.entries, home])
  const live = useMemo(() => new Set(data?.live), [data])
  const waiting = useMemo(() => new Set(data?.waiting), [data])
  const [carousel, setCarousel] = useState(false) // mode carrousel : on parcourt les sessions qui attendent une réponse
  const vgroups = useMemo(() => data?.vgroups ?? [], [data])
  const favs = useMemo(() => new Set(data?.favs), [data])
  const asking = useMemo(() => new Set(data?.asking), [data])
  useWaitingNotice(waiting, asking, everyId)
  useUpdateNotice()

  const gkey = (x: VGroup) => groupKey(x, home)
  // Listes dérivées mémoïsées : un rendu sans nouvelles données (sélection, menu, saisie) ne les recalcule pas.
  const recent = useMemo(() => view.rows.filter((r) => now - r.s.mtime < WEEK), [view.rows, now])
  const hot = useMemo(
    () =>
      (data?.groups ?? [])
        .flatMap((g) => g.sessions.filter((s) => (live.has(s.id) || now - s.mtime < HOT) && !isAuto(s)).map((s) => ({ s, g })))
        .sort(byDate),
    [data, now, live]
  )
  // Ouvertes d'abord, puis au cache chaud (récentes d'abord) ; la recherche s'applique comme ailleurs.
  const active = useMemo(
    () => view.rows.filter((r) => live.has(r.s.id) || now - r.s.mtime < HOT).sort((a, b) => +live.has(b.s.id) - +live.has(a.s.id) || b.s.mtime - a.s.mtime),
    [view.rows, live, now]
  )
  const selected = [...picked].map((id) => view.byId.get(id)).filter((r): r is Row => !!r)

  const node = route.kind === "dir" ? find(tree, route.key) : null
  // Sessions du dossier affiché qui appartiennent déjà à au moins un de ses groupes.
  const grouped = new Set(node?.g ? vgroups.filter((x) => x.project === node.g!.project).flatMap((x) => x.ids) : [])
  const nGrouped = node?.ss.filter((s) => grouped.has(s.id)).length ?? 0
  const vg = route.kind === "group" ? vgroups.find((g) => g.name === route.name && gkey(g) === route.key) : undefined
  const project = route.kind === "project" ? (data?.projects ?? []).find((x) => x.name === route.name) : undefined
  const rows: Row[] =
    route.kind === "recent"
      ? recent
      : route.kind === "project"
        ? view.rows.filter((r) => project?.dirs.includes(r.g.project)).sort(byDate)
        : route.kind === "group"
        ? (vg?.ids ?? [])
            .map((id) => view.byId.get(id))
            .filter((r): r is Row => !!r)
            .sort(byDate)
        : route.kind === "dir"
          ? (node?.ss ?? []).filter((s) => !(hideGrouped && !qt && grouped.has(s.id))).map((s) => ({ s, g: node!.g! }))
          : route.kind === "all"
            ? view.rows
            : route.kind === "active"
              ? active
              : route.kind === "auto"
                ? view.autoRows
                : []
  // Sessions des sous-dossiers : affichées à part, sous la liste du dossier sélectionné.
  const subs = node ? [...node.kids.values()].flatMap(under).sort(byDate) : []

  const read = (s: { id: string }) =>
    setParams((p) => {
      p.set("s", s.id)
      return p
    })
  // Sessions ouvertes et au cache chaud (actives) qui attendent, depuis le plus longtemps d'abord ; les froides sont écartées.
  const waitingRows = [...waiting].filter((id) => live.has(id)).map((id) => everyId.get(id)).filter((r): r is Row => !!r && now - r.s.mtime < HOT).sort((x, y) => x.s.mtime - y.s.mtime)
  const stepWaiting = (d: 1 | -1) => stepIn(waitingRows, (r) => r.s.id === reading, d)
  const startCarousel = () => {
    if (!waitingRows.length) return void toast.info(t("shell.app.noneWaiting"))
    setCarousel(true)
    read(waitingRows[0].s)
  }
  const advanceCarousel = (d: 1 | -1 = 1) => {
    const n = stepWaiting(d)
    if (n) read(n.s)
    else {
      setCarousel(false)
      toast.info(t("shell.app.noMoreWaiting"))
    }
  }
  // La session affichée par le carrousel a été supprimée (ou a disparu) : on passe à la suivante, ou on sort du mode.
  const gone = !!data && !!reading && !everyId.has(reading)
  useEffect(() => {
    if (!carousel || !gone) return
    const t = setTimeout(() => advanceCarousel(1), 0)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [carousel, gone])
  const closeReader = () => (
    setCarousel(false),
    setParams(
      (p) => {
        p.delete("s")
        return p
      },
      { replace: true }
    )
  )

  const { actions, groupActs, groupRows, targets, newSession, purgeAuto } = useAppActions({
    data,
    setData,
    reload,
    home,
    q,
    go,
    read,
    now,
    live,
    waiting,
    hitMap,
    vgroups,
    favs,
    vg,
    picked,
    setPicked,
    selected,
    everyId,
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
  useShortcuts({ search, picked, clearPicked: () => setPicked(new Set()), byId: view.byId, actions })

  const g: Group | null = node?.g ?? null
  const own = g ? node!.ss.map((s) => ({ dir: s.dir, id: s.id })) : []
  // Le lecteur garde la dernière version connue de la session affichée : si une relecture de la liste ne la contient pas un instant
  // (écriture en cours, erreur passagère), la conversation ne se vide pas ni ne se ferme. Une vraie disparition (suppression) est
  // traitée à part (`gone`).
  const found = reading ? (everyId.get(reading) ?? null) : null
  const [kept, setKept] = useState<Row | null>(null)
  if (found && found !== kept) setKept(found)
  const reader = reading ? (found ?? (kept?.s.id === reading ? kept : null)) : null
  const favRows = useMemo(() => [...favs].map((id) => everyId.get(id)).filter((r): r is Row => !!r), [favs, everyId])
  const favGroups = vgroups.filter((x) => x.fav).map((x) => ({ vg: x, key: gkey(x) }))

  // Récentes : regroupées par projet, chaque projet pouvant être repris d'un coup.
  const isRecent = route.kind === "recent"
  const byProject = useMemo(() => {
    const m = new Map<string, Row[]>()
    if (isRecent) for (const r of recent) m.set(r.g.project, [...(m.get(r.g.project) ?? []), r])
    return m
  }, [isRecent, recent])

  const title = {
    recent: t("shell.app.title.recent"),
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
    dir: node?.name ?? t("shell.app.title.dir"),
  }[route.kind]
  const nSessions = (count: number) => t("shell.count.sessions", { count })
  const sub =
    route.kind === "recent"
      ? t("shell.app.sub.recent", { sessions: nSessions(recent.length), projects: t("shell.count.projects", { count: byProject.size }) })
      : route.kind === "project"
        ? t("shell.app.sub.project", { dirs: t("shell.count.dirs", { count: project?.dirs.length ?? 0 }), sessions: nSessions(rows.length) })
        : route.kind === "active"
        ? t("shell.app.sub.active", {
            open: t("shell.count.open", { count: active.filter((r) => live.has(r.s.id)).length }),
            warm: t("shell.count.warm", { count: active.filter((r) => !live.has(r.s.id)).length }),
          })
        : route.kind === "auto"
          ? t("shell.app.sub.auto")
        : route.kind === "skills"
          ? t("shell.app.sub.skills")
          : route.kind === "extensions"
          ? t("shell.app.sub.extensions")
          : route.kind === "config"
          ? t("shell.app.sub.config")
          : route.kind === "snippets"
            ? (snips?.dir ?? "")
          : route.kind === "memory"
            ? t("shell.app.sub.memory")
            : route.kind === "workspace"
              ? tilde(route.dir, home)
              : route.kind === "projectConfig"
              ? tilde(data?.groups.find((x) => x.sessions[0]?.dir === route.dir)?.project ?? route.dir, home)
              : route.kind === "tabs"
              ? t("shell.app.sub.tabs")
              : route.kind === "group"
                ? t("shell.app.sub.group", { dir: vg ? tilde(vg.project, home) : "?", sessions: nSessions(rows.length) })
                : g
                  ? t("shell.app.sub.dir", { dir: tilde(g.project, home), sessions: nSessions(rows.length) })
                  : nSessions(rows.length)

  return (
    <ActionsCtx.Provider value={actions}>
      <div className="safe-t safe-x flex h-dvh flex-col md:flex-row">
        <Sidebar
          open={navOpen}
          onClose={() => setNavOpen(false)}
          loading={data === null}
          total={view.total}
          searchRef={search}
          route={route}
          go={(r) => (setNavOpen(false), go(r))}
          counts={{ all: base.rows.length, active: base.rows.filter((r) => live.has(r.s.id) || now - r.s.mtime < HOT).length }}
          reload={reload}
          favRows={favRows}
          favGroups={favGroups}
          groupCount={(x) => groupRows(x).length}
          read={(s) => (setNavOpen(false), read(s))}
          tree={localTree}
          hot={hot}
          carousel={{ n: waitingRows.length, on: carousel, start: startCarousel }}
          now={now}
          ttl={HOT}
          snippets={snips ? snips.files.length : null}
          auto={{ n: view.autoItems.length, purge: purgeAuto, on: route.kind === "auto" }}
          args={args}
        />

        <main className="relative flex min-h-0 min-w-0 flex-1 flex-col">
          <PageHeader
            onMenu={() => setNavOpen(true)}
            title={title}
            sub={sub}
            qt={qt}
            route={route}
            g={g}
            vg={vg}
            firstDir={node?.ss[0]?.dir}
            own={own}
            rowCount={rows.length}
            actions={actions}
            groupActs={groupActs}
            split={route.kind === "skills" || route.kind === "memory" || route.kind === "snippets"}
            reload={reload}
            searchBox={
              ["recent", "all", "active", "auto", "project", "group", "dir", "memory"].includes(route.kind) ? <SessionSearch q={q} setQ={setQ} adv={adv} setAdv={setAdv} searching={searching} /> : undefined
            }
          />

          <RouteErrorBoundary key={route.kind}>
          <Suspense fallback={<Skeleton className="m-6 h-32" />}>
          {route.kind === "workspace" ? (
            <IdeView key={route.dir} dir={route.dir} />
          ) : route.kind === "tabs" ? (
            <Tabs args={args} onAsk={setAsk} onPrompt={setPrompt} />
          ) : route.kind === "skills" ? (
            <Skills sel={route.key} home={home} q={qt} onPick={(key) => go({ kind: "skills", key })} onAsk={setAsk} />
          ) : route.kind === "projectConfig" ? (
            <Config key={route.dir} dir={route.dir} home={home} tab="" onTab={() => go({ kind: "config", tab: "permissions" })} ui={{ args, setArgs, choice, setChoice, font, setFont, fontSize, setFontSize }} />
          ) : route.kind === "teams" ? (
            <TeamsPage home={home} />
          ) : route.kind === "team" ? (
            <TeamRun key={route.name} name={route.name} />
          ) : route.kind === "extensions" ? (
            <Extensions home={home} onAsk={setAsk} />
          ) : route.kind === "config" ? (
            <Config home={home} tab={route.tab} onTab={(tab) => go({ kind: "config", tab })} ui={{ args, setArgs, choice, setChoice, font, setFont, fontSize, setFontSize }} />
          ) : route.kind === "memory" ? (
            <Memory sel={route.key} home={home} q={qt} onPick={(key) => go({ kind: "memory", key })} onAsk={setAsk} onCompose={setCompose} args={args} />
          ) : route.kind === "snippets" ? (
            snips ? (
              <Snippets
                files={snips.files}
                scopes={snips.scopes}
                dir={snips.dir}
                sel={route.path}
                q={qt}
                home={home}
                onPick={(key) => go({ kind: "snippets", path: key })}
                onLaunch={(s, text) => {
                  // Le dossier du projet de la commande d'abord, sinon le plus récent.
                  const ts = targets().sort((x, y) => Number(y.project === s.project) - Number(x.project === s.project))
                  newSession(t("shell.app.snippetSession"), s.command, ts, body(text) + "\n\n")
                }}
                onChanged={reload}
                onAsk={setAsk}
                onPrompt={setPrompt}
                onCompose={setCompose}
                args={args}
              />
            ) : (
              <Skeleton className="m-6 h-32" />
            )
          ) : (
            <SessionsPane
              loading={data === null}
              route={route}
              rows={rows}
              subs={subs}
              byProject={byProject}
              qt={qt}
              vg={vg}
              dirMissing={!node}
              projectDir={rows[0]?.s.dir ?? node?.ss[0]?.dir}
              project={node?.g?.project}
              home={home}
              live={live}
              waiting={waiting}
              actions={actions}
              go={go}
              clearSearch={() => setQ("")}
              grouped={{ n: nGrouped, hide: !!hideGrouped, setHide: (v) => setHideGrouped(v ? "1" : "") }}
              error={error}
              denied={denied}
              retry={() => void reload().catch(() => {})}
            />
          )}

          </Suspense>
          </RouteErrorBoundary>

          {selected.length > 0 && (
            <SessionSelectionBar
              selected={selected}
              onSelectAll={() => setPicked(new Set([...picked, ...[...rows, ...subs].map((r) => r.s.id)]))}
              onClear={() => setPicked(new Set())}
            />
          )}
        </main>
      </div>

      <Suspense fallback={null}>
        <Conversation row={reader} onClose={closeReader} onFolder={reader ? () => go({ kind: "dir", key: segs(reader.g.project, home).join("/") }) : undefined} args={args} choice={choice} carousel={carousel ? { n: waitingRows.length, next: () => advanceCarousel(1), prev: () => advanceCarousel(-1), stop: () => setCarousel(false) } : undefined} />
      </Suspense>
      <DirPicker pick={picker} home={home} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
      <BanAlert />
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
      <Compose ask={compose} onClose={() => setCompose(null)} />
      <SessionInfo row={info} onClose={() => setInfo(null)} />
      {share && <ShareDialog key={share.s.id} id={share.s.id} dir={share.s.dir} title={share.s.title} onClose={() => setShare(null)} />}
    </ActionsCtx.Provider>
  )
}
