import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { resolveFront, setLang } from "@/i18n"
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react"
import { useLocation, useSearchParams } from "wouter"
import { type Group, type Session, type VGroup } from "@/lib/api"
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
import { applyFont } from "@/lib/fonts"
import { type Choice } from "@/lib/models"
import { AUTO, buildView, byDate, groupKey, HOT, WEEK } from "@/lib/sessionsView"
import { Skeleton } from "@/components/ui/skeleton"
import { ActionsCtx, type Row } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { PageHeader } from "@/components/PageHeader"
import { SessionInfo } from "@/components/SessionInfo"
import { Prompt, type Ask as PromptAsk } from "@/components/Prompt"
import { SessionSelectionBar } from "@/components/SessionSelectionBar"
import { SessionsPane } from "@/components/SessionsPane"
import { Sidebar } from "@/components/Sidebar"
import { Compose, type ComposeAsk } from "@/components/Compose"

// Les grandes vues se chargent à la demande : le premier chargement ne porte que la liste des sessions.
const Conversation = lazy(() => import("@/components/Conversation").then((m) => ({ default: m.Conversation })))
const Snippets = lazy(() => import("@/components/Snippets").then((m) => ({ default: m.Snippets })))
const Config = lazy(() => import("@/components/Config").then((m) => ({ default: m.Config })))
const Skills = lazy(() => import("@/components/Skills").then((m) => ({ default: m.Skills })))
const Memory = lazy(() => import("@/components/Memory").then((m) => ({ default: m.Memory })))
const Tabs = lazy(() => import("@/components/Tabs").then((m) => ({ default: m.Tabs })))

// Application : état de navigation (route, recherche, sélection, réglages) et assemblage. Les données viennent de
// useAppData, les actions de useAppActions, les raccourcis de useShortcuts.
export default function App() {
  const [loc, nav] = useLocation()
  const [params, setParams] = useSearchParams()
  const route = parse(loc)
  const q = params.get("q") ?? ""
  const reading = params.get("s")

  const { data, setData, snips, now, reload } = useAppData()
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
  const [picker, setPicker] = useState<Pick | null>(null)
  const [navOpen, setNavOpen] = useState(false) // mobile : tiroir du menu
  const [ask, setAsk] = useState<Ask | null>(null)
  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
  const [compose, setCompose] = useState<ComposeAsk | null>(null)
  const [info, setInfo] = useState<Row | null>(null)
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
  const go = (r: Route) => nav(href(r) + (q ? `?q=${encodeURIComponent(q)}` : ""))

  const qt = q.trim()
  const hitMap = useSearchHits(qt)
  const searching = qt.length >= 2 && !hitMap && route.kind !== "snippets" && route.kind !== "memory" && route.kind !== "tabs" && route.kind !== "config" && route.kind !== "skills"

  const tags = useMemo(() => data?.tags ?? {}, [data])
  const view = buildView(data, qt, tags, hitMap)

  // Toutes les sessions, même filtrées : le lecteur reste ouvert quand la recherche change.
  const everyId = useMemo(() => {
    const m = new Map<string, Row>()
    for (const g of data?.groups ?? []) for (const s of g.sessions) m.set(s.id, { s, g })
    return m
  }, [data])

  const home = data?.home ?? ""
  const tree = useMemo(() => buildTree(view.entries, home), [view.entries, home])
  // « Sessions locales » : seulement les dossiers qui ne sont dans aucun projet (ils ont leur place sous « Projets »).
  const projects = data?.projects
  const localTree = useMemo(() => {
    const inProject = new Set(projects?.flatMap((p) => p.dirs))
    return inProject.size ? buildTree(view.entries.filter((e) => !inProject.has(e.g.project)), home) : tree
  }, [projects, view.entries, home, tree])
  const live = useMemo(() => new Set(data?.live), [data])
  const waiting = useMemo(() => new Set(data?.waiting), [data])
  const [carousel, setCarousel] = useState(false) // mode carrousel : on parcourt les sessions qui attendent une réponse
  const vgroups = useMemo(() => data?.vgroups ?? [], [data])
  const favs = useMemo(() => new Set(data?.favs), [data])
  useWaitingNotice(waiting, everyId)

  const gkey = (x: VGroup) => groupKey(x, home)
  const recent = view.rows.filter((r) => now - r.s.mtime < WEEK)
  const hot = (data?.groups ?? [])
    .flatMap((g) => g.sessions.filter((s) => now - s.mtime < HOT && !AUTO.some((p) => s.title.includes(p))).map((s) => ({ s, g })))
    .sort(byDate)
  // Ouvertes d'abord, puis au cache chaud (récentes d'abord) ; la recherche s'applique comme ailleurs.
  const active = view.rows
    .filter((r) => live.has(r.s.id) || now - r.s.mtime < HOT)
    .sort((a, b) => +live.has(b.s.id) - +live.has(a.s.id) || b.s.mtime - a.s.mtime)
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
          ? (node?.ss ?? []).filter((s) => !(hideGrouped && grouped.has(s.id))).map((s) => ({ s, g: node!.g! }))
          : route.kind === "all"
            ? view.rows
            : route.kind === "active"
              ? active
              : []
  // Sessions des sous-dossiers : affichées à part, sous la liste du dossier sélectionné.
  const subs = node ? [...node.kids.values()].flatMap(under).sort(byDate) : []

  const read = (s: Session) =>
    setParams((p) => {
      p.set("s", s.id)
      return p
    })
  // Sessions ouvertes et au cache chaud (actives) qui attendent, depuis le plus longtemps d'abord ; les froides sont écartées.
  const waitingRows = [...waiting].filter((id) => live.has(id)).map((id) => everyId.get(id)).filter((r): r is Row => !!r && now - r.s.mtime < HOT).sort((x, y) => x.s.mtime - y.s.mtime)
  const nextWaiting = () => waitingRows.find((r) => r.s.id !== reading)
  const startCarousel = () => {
    if (!waitingRows.length) return void toast.info(t("shell.app.noneWaiting"))
    setCarousel(true)
    read(waitingRows[0].s)
  }
  const advanceCarousel = () => {
    const n = nextWaiting()
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
    const t = setTimeout(advanceCarousel, 0)
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
    setQ,
    go,
    read,
    now,
    live,
    waiting,
    hitMap,
    tags,
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
  const favRows = [...favs].map((id) => everyId.get(id)).filter((r): r is Row => !!r)
  const favGroups = vgroups.filter((x) => x.fav).map((x) => ({ vg: x, key: gkey(x) }))

  // Récentes : regroupées par projet, chaque projet pouvant être repris d'un coup.
  const byProject = new Map<string, Row[]>()
  if (route.kind === "recent") for (const r of recent) byProject.set(r.g.project, [...(byProject.get(r.g.project) ?? []), r])

  const title = {
    recent: t("shell.app.title.recent"),
    all: t("shell.app.title.all"),
    active: t("shell.app.title.active"),
    group: route.kind === "group" ? route.name : "",
    project: route.kind === "project" ? route.name : "",
    snippets: t("shell.app.title.snippets"),
    memory: t("shell.app.title.memory"),
    config: t("shell.app.title.config"),
    skills: t("shell.app.title.skills"),
    tabs: t("shell.app.title.tabs"),
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
        : route.kind === "skills"
          ? t("shell.app.sub.skills")
          : route.kind === "config"
          ? t("shell.app.sub.config")
          : route.kind === "snippets"
            ? (snips?.dir ?? "")
          : route.kind === "memory"
            ? t("shell.app.sub.memory")
            : route.kind === "tabs"
              ? t("shell.app.sub.tabs")
              : route.kind === "group"
                ? t("shell.app.sub.group", { dir: vg ? tilde(vg.project, home) : "?", sessions: nSessions(rows.length) })
                : g
                  ? t("shell.app.sub.dir", { dir: tilde(g.project, home), sessions: nSessions(rows.length) })
                  : nSessions(rows.length)

  return (
    <ActionsCtx.Provider value={actions}>
      <div className="flex h-dvh flex-col md:flex-row">
        <Sidebar
          open={navOpen}
          onClose={() => setNavOpen(false)}
          loading={data === null}
          total={view.total}
          q={q}
          setQ={setQ}
          searchRef={search}
          searching={searching}
          route={route}
          go={(r) => (setNavOpen(false), go(r))}
          counts={{ recent: recent.length, all: view.rows.length, active: active.length }}
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
          auto={{ n: view.autoItems.length, kb: view.akb, purge: purgeAuto }}
          args={args}
          setArgs={setArgs}
          choice={choice}
          setChoice={setChoice}
          font={font}
          setFont={setFont}
          fontSize={fontSize}
          setFontSize={setFontSize}
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
            nGrouped={nGrouped}
            hideGrouped={hideGrouped}
            setHideGrouped={setHideGrouped}
            actions={actions}
            groupActs={groupActs}
            split={route.kind === "skills" || route.kind === "memory" || route.kind === "snippets"}
          />

          <Suspense fallback={<Skeleton className="m-6 h-32" />}>
          {route.kind === "tabs" ? (
            <Tabs args={args} onAsk={setAsk} onPrompt={setPrompt} />
          ) : route.kind === "skills" ? (
            <Skills sel={route.key} home={home} q={qt} onPick={(key) => go({ kind: "skills", key })} onAsk={setAsk} />
          ) : route.kind === "config" ? (
            <Config home={home} tab={route.tab} onTab={(tab) => go({ kind: "config", tab })} />
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
            />
          )}

          </Suspense>

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
        <Conversation row={reader} onClose={closeReader} args={args} choice={choice} carousel={carousel ? { n: waitingRows.length, next: advanceCarousel, stop: () => setCarousel(false) } : undefined} />
      </Suspense>
      <DirPicker pick={picker} home={home} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
      <Compose ask={compose} onClose={() => setCompose(null)} />
      <SessionInfo row={info} onClose={() => setInfo(null)} />
    </ActionsCtx.Provider>
  )
}
