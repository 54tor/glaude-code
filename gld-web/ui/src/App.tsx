import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useLocation, useSearchParams } from "wouter"
import { toast } from "sonner"
import { ChevronRight, FolderInput, MoreHorizontal, Pencil, Search, SquareTerminal, Trash2, TriangleAlert, X } from "lucide-react"
import { api, type Data, type Group, type Hit, type Item, type Session, type Snippet, type VGroup } from "@/lib/api"
import { exportMarkdown } from "@/lib/export"
import { segs } from "@/lib/paths"
import { href, parse, type Route } from "@/lib/route"
import { buildTree, find, under, type Entry } from "@/lib/tree"
import { size, tilde } from "@/lib/format"
import { useLocal } from "@/hooks/useLocal"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ActionsCtx, type Actions, type Row } from "@/components/actions"
import { Confirm, type Ask } from "@/components/Confirm"
import { Conversation } from "@/components/Conversation"
import { DirPicker, type Pick } from "@/components/DirPicker"
import { Prompt, type Ask as PromptAsk } from "@/components/Prompt"
import { Live, SessionList } from "@/components/SessionList"
import { DD, SessionItems } from "@/components/SessionMenu"
import { Sidebar } from "@/components/Sidebar"
import { Snippets } from "@/components/Snippets"
import { Memory } from "@/components/Memory"
import { Tabs } from "@/components/Tabs"

// Sessions lancées automatiquement (revues de sécurité) : leur titre est leur premier prompt. Elles sont masquées
// de l'arbre, comptées dans la barre latérale, et se suppriment d'un coup avec le bouton dédié.
const AUTO = ["Review this change for security vulnerabilities", "You previously flagged these candidate"]
const WEEK = 7 * 86400
const REFRESH = 15_000 // rafraîchissement de la liste, onglet visible seulement

const byDate = (a: Row, b: Row) => b.s.mtime - a.s.mtime
const plural = (n: number, w = "session") => `${n} ${w}${n > 1 ? "s" : ""}`

function Empty({ title, text, children }: { title: string; text: string; children?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 py-20 text-center">
      <Search className="size-8 text-muted-foreground/50" />
      <p className="font-medium">{title}</p>
      <p className="max-w-sm text-sm text-muted-foreground">{text}</p>
      {children}
    </div>
  )
}

export default function App() {
  const [loc, nav] = useLocation()
  const [params, setParams] = useSearchParams()
  const route = parse(loc)
  const q = params.get("q") ?? ""
  const reading = params.get("s")

  const [data, setData] = useState<Data | null>(null)
  const [snips, setSnips] = useState<{ dir: string; files: Snippet[] } | null>(null)
  const [hits, setHits] = useState<{ q: string; m: Map<string, Hit> } | null>(null)
  const [picked, setPicked] = useState(() => new Set<string>())
  const [args, setArgs] = useLocal("ccArgs", "")
  const [picker, setPicker] = useState<Pick | null>(null)
  const [ask, setAsk] = useState<Ask | null>(null)
  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
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

  // now : heure du dernier chargement, pour « Récentes » (le rendu reste pur).
  const [now, setNow] = useState(0)
  const reload = useCallback(async () => {
    const [d, s] = await Promise.all([api.data(), api.snippets()])
    setData(d)
    setSnips(s)
    setNow(Date.now() / 1000)
  }, [setData, setSnips, setNow])
  useEffect(() => {
    let live = true
    Promise.all([api.data(), api.snippets()]).then(([d, s]) => {
      if (!live) return
      setData(d)
      setSnips(s)
      setNow(Date.now() / 1000)
    })
    // Rafraîchissement régulier tant que l'onglet est visible, et au retour sur l'onglet.
    const tick = () => document.visibilityState === "visible" && reload().catch(() => {})
    const t = setInterval(tick, REFRESH)
    document.addEventListener("visibilitychange", tick)
    return () => {
      live = false
      clearInterval(t)
      document.removeEventListener("visibilitychange", tick)
    }
  }, [reload])

  // Recherche dans le texte des conversations, côté serveur, après une courte pause de frappe.
  const qt = q.trim()
  useEffect(() => {
    if (qt.length < 2) return
    let live = true
    const t = setTimeout(() => api.search(qt).then((r) => live && setHits({ q: qt, m: new Map(r.hits.map((h) => [h.id, h])) })), 250)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [qt])
  const hitMap = qt.length >= 2 && hits?.q === qt ? hits.m : null
  const searching = qt.length >= 2 && !hitMap && route.kind !== "snippets" && route.kind !== "memory" && route.kind !== "tabs"

  // Sessions affichées (hors revues automatiques, selon la recherche) et revues automatiques masquées.
  const view = useMemo(() => {
    const f = qt.toLowerCase()
    const entries: Entry[] = []
    const autoItems: Item[] = []
    let akb = 0
    let total = 0
    for (const g of data?.groups ?? []) {
      const vis: Session[] = []
      for (const s of g.sessions) {
        if (AUTO.some((p) => s.title.includes(p))) {
          autoItems.push({ dir: s.dir, id: s.id })
          akb += s.kb
        } else vis.push(s)
      }
      total += vis.length
      const ss = vis.filter((s) => !f || g.project.toLowerCase().includes(f) || s.title.toLowerCase().includes(f) || hitMap?.has(s.id))
      if (ss.length) entries.push({ g, ss })
    }
    const rows = entries.flatMap(({ g, ss }) => ss.map((s) => ({ s, g }))).sort(byDate)
    return {
      entries,
      rows,
      byId: new Map(rows.map((r) => [r.s.id, r])),
      autoItems,
      akb,
      total,
    }
  }, [data, qt, hitMap])

  // Toutes les sessions, même filtrées : le lecteur reste ouvert quand la recherche change.
  const everyId = useMemo(() => {
    const m = new Map<string, Row>()
    for (const g of data?.groups ?? []) for (const s of g.sessions) m.set(s.id, { s, g })
    return m
  }, [data])

  const home = data?.home ?? ""
  const tree = useMemo(() => buildTree(view.entries, home), [view.entries, home])
  const live = useMemo(() => new Set(data?.live), [data])
  const vgroups = useMemo(() => data?.vgroups ?? [], [data])
  const recent = view.rows.filter((r) => now - r.s.mtime < WEEK)
  const selected = [...picked].map((id) => view.byId.get(id)).filter((r): r is Row => !!r)

  const node = route.kind === "dir" ? find(tree, route.key) : null
  const vg = route.kind === "group" ? vgroups.find((g) => g.name === route.name) : undefined
  const rows: Row[] =
    route.kind === "recent"
      ? recent
      : route.kind === "group"
        ? (vg?.ids ?? [])
            .map((id) => view.byId.get(id))
            .filter((r): r is Row => !!r)
            .sort(byDate)
        : route.kind === "dir"
          ? (node?.ss ?? []).map((s) => ({ s, g: node!.g! }))
          : route.kind === "all"
            ? view.rows
            : []
  // Sessions des sous-dossiers : affichées à part, sous la liste du dossier sélectionné.
  const subs = node ? [...node.kids.values()].flatMap(under).sort(byDate) : []

  // Une action : toast de progression puis de résultat, erreurs jamais muettes, liste rechargée.
  const act = async (url: string, body: unknown, busy: string, pick: "first" | "last" = "first") => {
    const id = toast.loading(busy)
    const r = await api.post(url, body)
    const lines = r.out
      .trim()
      .split("\n")
      .filter((l) => l.trim())
    const main = (pick === "last" ? lines[lines.length - 1] : lines[0]) ?? (r.ok ? "Terminé" : "Échec")
    const rest = lines.filter((l) => l !== main).join("\n")
    ;(r.ok ? toast.success : toast.error)(main, {
      id,
      description: rest || undefined,
    })
    await reload()
    return r.ok
  }

  const pickDir = (title: string, start: string) =>
    new Promise<string | null>((resolve) =>
      setPicker({
        title,
        start,
        done: (p) => {
          setPicker(null)
          resolve(p)
        },
      })
    )

  // Groupes virtuels : mise à jour immédiate de l'affichage, puis enregistrement côté serveur.
  const saveGroups = async (next: VGroup[]) => {
    setData((d) => d && { ...d, vgroups: next })
    const r = await api.post("/vgroups", next)
    if (!r.ok) {
      toast.error(r.out)
      await reload()
    }
  }
  const named = (title: string, value: string, confirm: string, run: (v: string) => void) => setPrompt({ title, value, confirm, run })
  const addTo = (ids: string[], name: string) => {
    const exists = vgroups.some((g) => g.name === name)
    saveGroups(exists ? vgroups.map((g) => (g.name === name ? { ...g, ids: [...new Set([...g.ids, ...ids])] } : g)) : [...vgroups, { name, ids }])
    if (ids.length)
      toast.success(`${plural(ids.length)} dans « ${name} »`, {
        action: { label: "Voir", onClick: () => go({ kind: "group", name }) },
      })
  }
  const newGroup = (ids: string[]) => named("Nouveau groupe", "", "Créer", (n) => addTo(ids, n))
  const renameGroup = (name: string) =>
    named(`Renommer le groupe « ${name} »`, name, "Renommer", (n) => {
      if (n === name) return
      if (vgroups.some((g) => g.name === n)) return void toast.error(`Le groupe « ${n} » existe déjà`)
      saveGroups(vgroups.map((g) => (g.name === name ? { ...g, name: n } : g)))
      if (vg?.name === name) go({ kind: "group", name: n })
    })
  const deleteGroup = (name: string) =>
    setAsk({
      title: `Supprimer le groupe « ${name} » ?`,
      body: "Seul le regroupement disparaît : les sessions restent intactes.",
      confirm: "Supprimer le groupe",
      run: () => {
        saveGroups(vgroups.filter((g) => g.name !== name))
        if (vg?.name === name) go({ kind: "recent" })
      },
    })

  // Plusieurs sessions dans des onglets de terminal, l'une après l'autre.
  const openMany = async (rs: Row[]) => {
    const id = toast.loading(`Ouverture de ${plural(rs.length)}…`)
    let n = 0
    for (const r of rs) if ((await api.post("/open", { dir: r.s.dir, id: r.s.id, args })).ok) n++
    ;(n === rs.length ? toast.success : toast.error)(`${n} / ${rs.length} ouverte(s)`, { id })
  }

  const read = (s: Session) =>
    setParams((p) => {
      p.set("s", s.id)
      return p
    })
  const closeReader = () =>
    setParams(
      (p) => {
        p.delete("s")
        return p
      },
      { replace: true }
    )
  const unpick = (items: Item[]) =>
    setPicked((p) => {
      const n = new Set(p)
      for (const it of items) n.delete(it.id)
      return n
    })

  const actions: Actions = {
    home,
    q,
    live,
    hit: (id) => hitMap?.get(id),
    vgroups,
    inGroup: vg?.name ?? null,
    picked,
    selected,
    pick: (id) =>
      setPicked((p) => {
        const n = new Set(p)
        if (n.has(id)) n.delete(id)
        else n.add(id)
        return n
      }),
    read,
    open: (b) => act("/open", { ...b, args }, "Ouverture du terminal…", "last"),
    copy: (s) =>
      navigator.clipboard.writeText(`claude --resume ${s.id}`).then(
        () =>
          toast.success("Commande copiée", {
            description: `claude --resume ${s.id}`,
          }),
        () => toast.error("Copie impossible")
      ),
    rename: (s) =>
      named("Renommer la session", s.named ? s.title : "", "Renommer", (title) => act("/rename", { dir: s.dir, id: s.id, title }, "Renommage…")),
    exportMd: (r) => exportMarkdown(r.s, tilde(r.g.project, home)).catch(() => toast.error("Export impossible")),
    move: async (rs) => {
      const to = await pickDir(rs.length > 1 ? `Déplacer ${plural(rs.length)} vers…` : `Déplacer « ${rs[0].s.title} » vers…`, rs[0].s.cwd)
      const items = rs.filter((r) => r.s.cwd !== to).map((r) => ({ dir: r.s.dir, id: r.s.id }))
      if (to && items.length && (await act("/reassign", { items, to }, "Déplacement…"))) unpick(items)
    },
    moveGroup: async (g: Group, items: Item[]) => {
      const to = await pickDir(`Réassigner ${plural(items.length)} vers…`, g.project)
      if (to) act("/reassign", { items, to }, "Réassignation…")
    },
    group: (ids, name) => (name === null ? newGroup(ids) : addTo(ids, name)),
    ungroup: (ids, name = vg?.name) => {
      if (name) saveGroups(vgroups.map((g) => (g.name === name ? { ...g, ids: g.ids.filter((i) => !ids.includes(i)) } : g)))
    },
    remove: (items, what, after) =>
      setAsk({
        title: "Supprimer définitivement ?",
        body: `${what}\n\nSuppression définitive : transcript, sous-agents et fichiers associés.`,
        confirm: "Supprimer",
        run: () => {
          after?.()
          unpick(items)
          act("/rm", { items }, "Suppression en cours…")
        },
      }),
  }

  const purgeAuto = () => {
    const n = view.autoItems.length
    setAsk({
      title: `Supprimer les ${n} revues de sécurité ?`,
      body: `Environ ${size(view.akb)} : sessions automatiques dont le titre contient « Review this change for security vulnerabilities » ou « You previously flagged these candidate ».\n\nLes sessions vivantes sont épargnées. Suppression définitive.`,
      confirm: `Supprimer ${n} sessions`,
      run: () => act("/rm", { items: view.autoItems }, `Suppression de ${n} sessions…`),
    })
  }

  // Raccourcis clavier, hors champ de saisie et hors fenêtre ou menu ouvert.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (/INPUT|TEXTAREA/.test(t.tagName) || t.isContentEditable || e.ctrlKey || e.metaKey || e.altKey) return
      if (document.querySelector('[role="dialog"],[role="alertdialog"],[role="menu"]')) return
      if (e.key === "/") {
        e.preventDefault()
        search.current?.focus()
        return
      }
      if (e.key === "Escape" && picked.size) return setPicked(new Set())
      const rowsEl = [...document.querySelectorAll<HTMLElement>("main [data-row]")]
      const i = rowsEl.indexOf(document.activeElement as HTMLElement)
      if (e.key === "j" || e.key === "k" || e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        const step = e.key === "j" || e.key === "ArrowDown" ? 1 : -1
        const next = rowsEl[Math.max(0, Math.min(rowsEl.length - 1, i < 0 ? 0 : i + step))]
        next?.focus()
        next?.scrollIntoView({ block: "nearest" })
        return
      }
      const r = i >= 0 ? view.byId.get(rowsEl[i].dataset.id!) : undefined
      if (!r) return
      const one = { dir: r.s.dir, id: r.s.id }
      if (e.key === "t") actions.open(one)
      else if (e.key === "r") actions.rename(r.s)
      else if (e.key === "x") actions.pick(r.s.id)
      else if (e.key === "e") actions.exportMd(r)
      else if (e.key === "Delete") actions.remove([one], `« ${r.s.title} »`)
      else return
      e.preventDefault()
    }
    window.addEventListener("keydown", k)
    return () => window.removeEventListener("keydown", k)
  })

  const g = node?.g ?? null
  const own = g ? node!.ss.map((s) => ({ dir: s.dir, id: s.id })) : []
  const reader = reading ? (everyId.get(reading) ?? null) : null
  const groupCounts = vgroups.map((x) => ({
    name: x.name,
    n: x.ids.filter((id) => view.byId.has(id)).length,
  }))

  // Récentes : regroupées par projet, chaque projet pouvant être repris d'un coup.
  const byProject = new Map<string, Row[]>()
  if (route.kind === "recent") for (const r of recent) byProject.set(r.g.project, [...(byProject.get(r.g.project) ?? []), r])

  const title = {
    recent: "Récentes",
    all: "Toutes les sessions",
    group: route.kind === "group" ? route.name : "",
    snippets: "Snippets",
    memory: "Mémoire",
    tabs: "Sessions sauvegardées",
    dir: node?.name ?? "Dossier",
  }[route.kind]
  const sub =
    route.kind === "recent"
      ? `7 derniers jours · ${plural(recent.length)} dans ${plural(byProject.size, "projet")}`
      : route.kind === "snippets"
        ? (snips?.dir ?? "")
        : route.kind === "memory"
          ? "CLAUDE.md, règles et mémoire de chaque projet"
          : route.kind === "tabs"
            ? "Les sessions ouvertes, pour les rouvrir d'un coup"
            : route.kind === "group"
              ? `Groupe · ${plural(rows.length)}`
              : g
                ? `${tilde(g.project, home)} · ${plural(rows.length)}`
                : plural(rows.length)

  return (
    <ActionsCtx.Provider value={actions}>
      <div className="flex h-dvh flex-col md:flex-row">
        <Sidebar
          loading={data === null}
          total={view.total}
          q={q}
          setQ={setQ}
          searchRef={search}
          searching={searching}
          route={route}
          go={go}
          counts={{ recent: recent.length, all: view.rows.length }}
          groups={groupCounts}
          newGroup={() => newGroup([])}
          renameGroup={renameGroup}
          deleteGroup={deleteGroup}
          tree={tree}
          snippets={snips?.dir ? snips.files.length : null}
          auto={{ n: view.autoItems.length, kb: view.akb, purge: purgeAuto }}
          args={args}
          setArgs={setArgs}
        />

        <main className="relative flex min-h-0 min-w-0 flex-1 flex-col">
          <header className="flex flex-wrap items-center gap-3 border-b px-6 py-4">
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-lg font-semibold tracking-tight">{title}</h2>
              <p className="truncate text-xs text-muted-foreground">
                {sub}
                {qt &&
                  (route.kind === "recent" || route.kind === "all" || route.kind === "group" || route.kind === "dir") &&
                  ` · recherche « ${qt} »`}
              </p>
            </div>
            {g && !g.missing && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => actions.open({ dir: node!.ss[0].dir })}
                title="Un onglet de terminal par session des 7 derniers jours"
              >
                <SquareTerminal /> Reprendre les récentes
              </Button>
            )}
            {vg && rows.length > 0 && (
              <Button variant="outline" size="sm" onClick={() => openMany(rows.filter((r) => !r.g.missing))}>
                <SquareTerminal /> Tout reprendre
              </Button>
            )}
            {(g || vg) && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label={g ? "Actions du projet" : "Actions du groupe"}>
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  {g && (
                    <>
                      <DropdownMenuItem onSelect={() => actions.moveGroup(g, own)}>
                        <FolderInput /> Réassigner les {plural(own.length)}…
                      </DropdownMenuItem>
                      <DropdownMenuItem variant="destructive" onSelect={() => actions.remove(own, `les ${plural(own.length)} de ${g.project}`)}>
                        <Trash2 /> Supprimer les {plural(own.length)} du projet
                      </DropdownMenuItem>
                    </>
                  )}
                  {vg && (
                    <>
                      <DropdownMenuItem onSelect={() => renameGroup(vg.name)}>
                        <Pencil /> Renommer le groupe…
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem variant="destructive" onSelect={() => deleteGroup(vg.name)}>
                        <Trash2 /> Supprimer le groupe
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </header>

          {g?.missing && (
            <div className="flex items-center gap-3 border-b bg-amber-500/10 px-6 py-3 text-sm">
              <TriangleAlert className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
              <p className="flex-1">
                Ce dossier n'existe plus : ses sessions ne peuvent pas être reprises tant qu'elles ne sont pas rattachées à un dossier existant.
              </p>
              <Button size="sm" variant="outline" onClick={() => actions.moveGroup(g, own)}>
                Réassigner…
              </Button>
            </div>
          )}

          {route.kind === "tabs" ? (
            <Tabs args={args} onAsk={setAsk} onPrompt={setPrompt} />
          ) : route.kind === "memory" ? (
            <Memory sel={route.key} home={home} q={qt} onPick={(key) => go({ kind: "memory", key })} onAsk={setAsk} args={args} />
          ) : route.kind === "snippets" ? (
            snips?.dir ? (
              <Snippets files={snips.files} dir={snips.dir} path={route.path} q={qt} onPick={(path) => go({ kind: "snippets", path })} />
            ) : (
              <Empty
                title="Bibliothèque non configurée"
                text="Définis GLD_WEB_SNIPPETS (un dossier de fichiers texte ou Markdown) avant de lancer gld-web."
              />
            )
          ) : (
            <ScrollArea className="min-h-0 flex-1">
              <div className="mx-auto max-w-4xl px-3 py-4 pb-24 md:px-6">
                {data === null && (
                  <div className="space-y-3 px-3">
                    {[0, 1, 2, 3, 4].map((i) => (
                      <Skeleton key={i} className="h-12 w-full" />
                    ))}
                  </div>
                )}

                {data && route.kind === "recent" && (
                  <div className="space-y-6">
                    {[...byProject].map(([project, rs]) => (
                      <section key={project}>
                        <div className="flex items-center gap-2 px-3 py-1.5">
                          <button
                            className="min-w-0 truncate text-sm font-medium hover:underline"
                            onClick={() =>
                              go({
                                kind: "dir",
                                key: segs(project, home).join("/"),
                              })
                            }
                            title="Afficher le dossier"
                          >
                            {tilde(project, home)}
                          </button>
                          <span className="shrink-0 text-xs text-muted-foreground">{plural(rs.length)}</span>
                          {rs.some((r) => live.has(r.s.id)) && <Live className="text-xs" />}
                          {!rs[0].g.missing && (
                            <Button
                              variant="ghost"
                              size="xs"
                              className="ml-auto text-muted-foreground"
                              onClick={() => actions.open({ dir: rs[0].s.dir })}
                              title="Un onglet de terminal par session des 7 derniers jours (hors sessions ouvertes)"
                            >
                              <SquareTerminal /> Reprendre le dossier
                            </Button>
                          )}
                        </div>
                        <SessionList items={rs} showProject={false} flat />
                      </section>
                    ))}
                  </div>
                )}

                {data && route.kind !== "recent" && rows.length > 0 && <SessionList items={rows} showProject={route.kind !== "dir"} />}

                {data && subs.length > 0 && (
                  <details open={rows.length === 0} className="group/subs mt-8 border-t pt-4">
                    <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground">
                      <ChevronRight className="size-4 transition-transform group-open/subs:rotate-90" />
                      Sessions des sous-dossiers
                      <span className="text-xs font-normal tabular-nums">{subs.length}</span>
                    </summary>
                    <div className="mt-2">
                      <SessionList items={subs} showProject />
                    </div>
                  </details>
                )}

                {data && rows.length === 0 && subs.length === 0 && (
                  <Empty
                    title={
                      route.kind === "group" && !vg ? "Groupe introuvable" : route.kind === "dir" && !node ? "Dossier introuvable" : "Aucune session"
                    }
                    text={
                      qt
                        ? "Aucun titre, projet ni message ne correspond à cette recherche."
                        : route.kind === "group"
                          ? "Ajoute des sessions à ce groupe : clic droit sur une session › Groupe."
                          : route.kind === "recent"
                            ? "Aucune session ces 7 derniers jours."
                            : "Rien à afficher ici."
                    }
                  >
                    {qt && (
                      <Button variant="outline" size="sm" className="mt-2" onClick={() => setQ("")}>
                        Effacer la recherche
                      </Button>
                    )}
                  </Empty>
                )}
              </div>
            </ScrollArea>
          )}

          {/* Barre de sélection multiple */}
          {selected.length > 0 && (
            <div className="absolute inset-x-0 bottom-4 z-30 flex justify-center px-4">
              <div className="flex items-center gap-1 rounded-xl border bg-popover p-1.5 pl-4 text-sm shadow-lg">
                <span className="mr-2 font-medium">
                  {plural(selected.length)} sélectionnée
                  {selected.length > 1 ? "s" : ""}
                </span>
                <Button variant="ghost" size="sm" onClick={() => setPicked(new Set([...picked, ...[...rows, ...subs].map((r) => r.s.id)]))}>
                  Tout sélectionner
                </Button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="sm">Actions</Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" side="top" className="w-64">
                    <SessionItems M={DD} row={selected[0]} />
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setPicked(new Set())}
                  aria-label="Vider la sélection"
                  title="Vider la sélection (Échap)"
                >
                  <X />
                </Button>
              </div>
            </div>
          )}
        </main>
      </div>

      <Conversation row={reader} onClose={closeReader} />
      <DirPicker pick={picker} home={home} />
      <Confirm ask={ask} onClose={() => setAsk(null)} />
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
    </ActionsCtx.Provider>
  )
}
