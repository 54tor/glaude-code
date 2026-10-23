import { useRef, type Dispatch, type SetStateAction } from "react"
import { toast } from "sonner"
import { watchTrust } from "@/lib/trust"
import { wantConfigFile } from "@/lib/wantConfigFile"
import { api, type Caps, type Data, type Group, type Hit, type Item, type Project, type Session, type Snippet, type VGroup } from "@/lib/api"
import { segs } from "@/lib/paths"
import { exportMarkdown } from "@/lib/export"
import { size, tilde } from "@/lib/format"
import { withChoice, type Choice } from "@/lib/models"
import { byDate, cut, groupKey, HOT, plural, type View } from "@/lib/sessionsView"
import type { Route } from "@/lib/route"
import type { Actions, GroupActions, ProjectActions, Row } from "@/components/actions"
import type { Ask } from "@/components/Confirm"
import type { ComposeAsk, Target } from "@/components/Compose"
import type { Pick } from "@/components/DirPicker"
import type { Ask as PromptAsk } from "@/components/Prompt"

const NO_CAPS: Caps = { hotCache: false, keepWarm: false, config: false, memory: false, tuiPrompts: false, resume: false }

// Tout ce que l'application sait faire d'une session ou d'un groupe (ouvrir, reprendre, renommer, déplacer, grouper,
// étiqueter, supprimer…), regroupé dans l'objet Actions que les composants lisent par contexte.
export function useAppActions(c: {
  data: Data | null
  setData: Dispatch<SetStateAction<Data | null>>
  reload: () => Promise<void>
  home: string
  q: string
  setQ: (v: string) => void
  go: (r: Route) => void
  read: (s: Session) => void
  now: number
  live: Set<string>
  waiting: Set<string>
  hitMap: Map<string, Hit> | null
  tags: Record<string, string[]>
  vgroups: VGroup[]
  favs: Set<string>
  vg: VGroup | undefined // groupe affiché
  picked: Set<string>
  setPicked: Dispatch<SetStateAction<Set<string>>>
  selected: Row[]
  everyId: Map<string, Row>
  view: View
  args: string
  choice: Choice
  snippets: Snippet[]
  setAsk: (a: Ask | null) => void
  setPrompt: (a: PromptAsk | null) => void
  setCompose: (a: ComposeAsk | null) => void
  setPicker: (p: Pick | null) => void
}) {
  const { data, setData, reload, home, q, setQ, go, read, now, live, waiting, hitMap, tags, vgroups, favs, vg, picked, setPicked, selected, everyId, view, args, choice, snippets, setAsk, setPrompt, setCompose, setPicker } = c
  const lastPick = useRef<string | null>(null) // dernière case cochée, début d'une sélection de plage
  const gkey = (x: VGroup) => groupKey(x, home)
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
    if (r.ok && url === "/new-session") void watchTrust(home)
    await reload()
    // Une session lancée met un instant à créer son transcript : on relit jusqu'à ce qu'elle apparaisse, sans retenir l'appelant
    // (la fenêtre de saisie se ferme dès le lancement).
    if (r.ok && (url === "/new-session" || url === "/open")) {
      void (async () => {
        for (let i = 0; i < 8; i++) {
          await new Promise((res) => setTimeout(res, 700))
          await reload().catch(() => {})
        }
      })()
    }
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

  // Groupes virtuels et favoris : mise à jour immédiate de l'affichage, puis enregistrement côté serveur.
  const saveStore = async (groups: VGroup[], favList: string[], tagMap = tags) => {
    setData((d) => d && { ...d, vgroups: groups, favs: favList, tags: tagMap })
    const r = await api.post("/vgroups", {
      groups,
      favs: favList,
      tags: tagMap,
    })
    if (!r.ok) {
      toast.error(r.out)
      await reload()
    }
  }
  const saveGroups = (groups: VGroup[]) => saveStore(groups, [...favs])
  // Projets : ensembles nommés de dossiers de travail (un dossier peut appartenir à plusieurs).
  const projects = data?.projects ?? []
  const saveProjects = async (next: Project[]) => {
    setData((d) => d && { ...d, projects: next })
    const r = await api.post("/vgroups", { groups: vgroups, favs: [...favs], tags, projects: next })
    if (!r.ok) {
      toast.error(r.out)
      await reload()
    }
  }
  const projectActs: ProjectActions = {
    add: (dir, name) => {
      const put = (n: string) => {
        const has = projects.some((p) => p.name === n)
        saveProjects(has ? projects.map((p) => (p.name === n && !p.dirs.includes(dir) ? { ...p, dirs: [...p.dirs, dir] } : p)) : [...projects, { name: n, dirs: [dir] }])
        toast.success(`${tilde(dir, home)} dans « ${n} »`)
      }
      if (name !== null) put(name)
      else
        named("Nouveau projet", "", "Créer", (n) => {
          const t = n.trim()
          if (projects.some((p) => p.name === t)) toast.error(`Le projet « ${t} » existe déjà`)
          else put(t)
        })
    },
    remove: (dir, name) => saveProjects(projects.map((p) => (p.name === name ? { ...p, dirs: p.dirs.filter((d) => d !== dir) } : p))),
    rename: (p) =>
      named(`Renommer le projet « ${p.name} »`, p.name, "Renommer", (n) => {
        const t = n.trim()
        if (t === p.name) return
        if (projects.some((x) => x.name === t)) return void toast.error(`Le projet « ${t} » existe déjà`)
        saveProjects(projects.map((x) => (x.name === p.name ? { ...x, name: t } : x)))
        void api.post("/notes", { key: "p/" + p.name, to: "p/" + t }) // la note suit le projet
        go({ kind: "project", name: t })
      }),
    delete: (p) =>
      setAsk({
        title: `Supprimer le projet « ${p.name} » ?`,
        body: "Seul le regroupement disparaît : les dossiers et leurs sessions restent.",
        confirm: "Supprimer",
        run: async () => {
          await saveProjects(projects.filter((x) => x.name !== p.name))
          go({ kind: "recent" })
        },
      }),
  }
  const same = (x: VGroup, y: VGroup) => x.name === y.name && x.project === y.project
  const named = (title: string, value: string, confirm: string, run: PromptAsk["run"], auto?: PromptAsk["auto"], row?: string) => setPrompt({ title, value, confirm, run, auto, row })
  // Un groupe est un sous-dossier virtuel : il ne contient que des sessions de son dossier de travail.
  const addTo = (ids: string[], project: string, name: string) => {
    const vg = { name, project, ids }
    const exists = vgroups.some((g) => same(g, vg))
    saveGroups(exists ? vgroups.map((g) => (same(g, vg) ? { ...g, ids: [...new Set([...g.ids, ...ids])] } : g)) : [...vgroups, vg])
    if (ids.length)
      toast.success(`${plural(ids.length)} dans « ${name} »`, {
        action: {
          label: "Voir",
          onClick: () => go({ kind: "group", name, key: gkey(vg) }),
        },
      })
  }
  const newGroup = (ids: string[], project: string) =>
    named(`Nouveau groupe dans ${tilde(project, home)}`, "", "Créer", (n) => addTo(ids, project, n))
  // Dossiers où lancer une nouvelle session : ceux qui existent encore, le plus récemment utilisé d'abord.
  // Un dossier enregistré sans session se lance par son chemin (cwd), en dernier.
  const targets = (): Target[] =>
    (data?.groups ?? [])
      .filter((x) => !x.missing)
      .map((x) => ({
        x,
        s: [...x.sessions].sort((p, q) => q.mtime - p.mtime)[0],
      }))
      .sort((p, q) => (q.s?.mtime ?? 0) - (p.s?.mtime ?? 0))
      .map(({ x, s }) => ({
        label: tilde(x.project, home),
        project: x.project,
        dir: s?.dir ?? "",
        id: s?.id ?? "",
        ...(s ? {} : { cwd: x.project }),
      }))
  const newSession = (title: string, subtitle: string, ts: Target[], initial = "") =>
    ts.length &&
    setCompose({
      id: "new:" + ts[0].project + title,
      title,
      subtitle,
      project: ts[0].project,
      initial,
      send: "Lancer la session",
      choice,
      targets: ts,
      run: (prompt, c, t = ts[0]) => act("/new-session", { ...(t.cwd ? { cwd: t.cwd } : { dir: t.dir, id: t.id }), prompt, args: withChoice(args, c) }, "Nouvelle session…", "last"),
    })
  const groupRows = (x: VGroup) =>
    x.ids
      .map((id) => everyId.get(id))
      .filter((r): r is Row => !!r)
      .sort(byDate)
  // Références des sessions d'un groupe, à coller dans un nouveau prompt.
  const refs = (x: VGroup) =>
    `Sessions Claude du groupe « ${x.name} » (dossier ${tilde(x.project, home)}) :\n` +
    groupRows(x)
      .map((r) => `- ${r.s.title} — claude --resume ${r.s.id} — transcript : ${data?.root ?? "~/.claude/projects"}/${r.s.dir}/${r.s.id}.jsonl`)
      .join("\n")
  const groupActs: GroupActions = {
    rows: groupRows,
    show: (x) => go({ kind: "group", name: x.name, key: gkey(x) }),
    open: (x) => openMany(groupRows(x).filter((r) => !r.g.missing && !live.has(r.s.id))),
    refs: (x) =>
      navigator.clipboard.writeText(refs(x)).then(
        () =>
          toast.success("Références copiées", {
            description: `${plural(groupRows(x).length)} du groupe « ${x.name} »`,
          }),
        () => toast.error("Copie impossible")
      ),
    prompt: (x) => {
      const first = groupRows(x)[0]
      if (!first) return
      setCompose({
        id: "g:" + x.project + x.name,
        title: "Nouvelle session avec ces références",
        subtitle: `Groupe « ${x.name} » · ${tilde(x.project, home)}`,
        project: x.project,
        initial: refs(x) + "\n\n",
        send: "Lancer la session",
        choice,
        run: (prompt, c) =>
          act(
            "/new-session",
            {
              dir: first.s.dir,
              id: first.s.id,
              prompt,
              args: withChoice(args, c),
            },
            "Nouvelle session…",
            "last"
          ),
      })
    },
    export: (x) =>
      exportMarkdown(
        groupRows(x).map((r) => ({
          s: r.s,
          project: tilde(r.g.project, home),
        })),
        `Groupe ${x.name}`
      ).catch(() => toast.error("Export impossible")),
    select: (x) => setPicked((p) => new Set([...p, ...groupRows(x).map((r) => r.s.id)])),
    fav: (x) => saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, fav: !g.fav } : g))),
    rename: (x) =>
      named(`Renommer le groupe « ${x.name} »`, x.name, "Renommer", (n) => {
        if (n === x.name) return
        if (vgroups.some((g) => g.project === x.project && g.name === n)) return void toast.error(`Le groupe « ${n} » existe déjà dans ce dossier`)
        saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, name: n } : g)))
        if (vg && same(vg, x)) go({ kind: "group", name: n, key: gkey(x) })
      }),
    remove: (x) =>
      setAsk({
        title: `Supprimer le groupe « ${x.name} » ?`,
        body: "Seul le regroupement disparaît : les sessions restent intactes.",
        confirm: "Supprimer le groupe",
        run: () => {
          saveGroups(vgroups.filter((g) => !same(g, x)))
          if (vg && same(vg, x)) go({ kind: "dir", key: gkey(x) })
        },
      }),
  }

  // Plusieurs sessions dans des onglets de terminal, l'une après l'autre.
  const openMany = async (rs: Row[]) => {
    const id = toast.loading(`Ouverture de ${plural(rs.length)}…`)
    let n = 0
    for (const r of rs) if ((await api.post("/open", { dir: r.s.dir, id: r.s.id, args })).ok) n++
    ;(n === rs.length ? toast.success : toast.error)(`${n} / ${rs.length} ouverte(s)`, { id })
  }


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
    waiting,
    asking: new Set(data?.asking),
    hit: (id) => hitMap?.get(id),
    registeredDirs: data?.dirs ?? [],
    addDir: () =>
      void pickDir("Ajouter un dossier à la liste", home).then(async (p) => {
        if (!p) return
        const r = await api.post("/dirs", { op: "add", dir: p })
        if (!r.ok) return void toast.error(r.out)
        await reload()
        go({ kind: "dir", key: segs(p, home).join("/") })
      }),
    removeDir: async (project) => {
      const r = await api.post("/dirs", { op: "remove", dir: project })
      if (!r.ok) return void toast.error(r.out)
      await reload()
    },
    openPerms: (project) => {
      const dir = data?.groups.find((g) => g.project === project)?.sessions[0]?.dir
      if (dir) wantConfigFile.id = "p/" + dir
      wantConfigFile.raw = true
      go({ kind: "config", tab: "permissions" })
    },
    folder: (project) => data?.groups.find((g) => g.project === project),
    vgroups,
    projects,
    projectActs,
    favs,
    tags,
    filterTag: (t) => setQ("#" + t),
    editTags: (s) =>
      named(`Étiquettes de « ${cut(s.title, 40)} » (séparées par des virgules)`, (tags[s.id] ?? []).join(", "), "Enregistrer", (v) => {
        const list = [
          ...new Set(
            v
              .split(",")
              .map((t) => t.trim())
              .filter(Boolean)
          ),
        ]
        const next = { ...tags }
        if (list.length) next[s.id] = list
        else delete next[s.id]
        saveStore(vgroups, [...favs], next)
        },
        undefined,
        s.id
      ),
    toggleFav: (ids) => {
      const add = ids.some((id) => !favs.has(id))
      saveStore(vgroups, add ? [...new Set([...favs, ...ids])] : [...favs].filter((id) => !ids.includes(id)))
    },
    inGroup: vg ?? null,
    groupActs,
    picked,
    selected,
    pick: (id, range) => {
      const from = lastPick.current
      lastPick.current = id
      setPicked((p) => {
        const n = new Set(p)
        const ids = [...document.querySelectorAll<HTMLElement>("main [data-row]")].map((e) => e.dataset.id ?? "")
        const a = from ? ids.indexOf(from) : -1
        const b = ids.indexOf(id)
        if (range && a >= 0 && b >= 0) for (const x of ids.slice(Math.min(a, b), Math.max(a, b) + 1)) n.add(x)
        else if (n.has(id)) n.delete(id)
        else n.add(id)
        return n
      })
    },
    hotLeft: (s) => (now && now - s.mtime < HOT ? Math.min(HOT / 60, Math.max(1, Math.ceil((HOT - (now - s.mtime)) / 60))) : null),
    touch: (id) => {
      const t = Date.now() / 1000
      setData((d) => d && { ...d, groups: d.groups.map((g) => ({ ...g, sessions: g.sessions.map((s) => (s.id === id ? { ...s, mtime: t } : s)) })) })
      setTimeout(() => reload().catch(() => {}), 2000) // le transcript a enregistré le prompt : on prend l'heure réelle
    },
    read,
    open: (b) => act("/open", { ...b, args }, "Ouverture du terminal…", "last"),
    // Un fournisseur inconnu ou une réponse ancienne : aucune fonction optionnelle (l'interface reste sobre plutôt que fausse).
    caps: (s) => data?.providers?.find((p) => p.id === (s.provider ?? "claude"))?.caps ?? NO_CAPS,
    defaultChoice: choice,
    snippets,
    startSession: async (project, prompt, c) => {
      const t = targets().find((x) => x.project === project)
      if (!t) return false
      await act("/new-session", { ...(t.cwd ? { cwd: t.cwd } : { dir: t.dir, id: t.id }), prompt, args: withChoice(args, c) }, "Nouvelle session…", "last")
      return true
    },
    newSession: (project, initial) => {
      const t = targets().filter((x) => x.project === project)
      if (t.length) newSession("Nouvelle session", t[0].label, t, initial)
    },
    resumeWith: (r) =>
      live.has(r.s.id)
        ? void toast.info("Session déjà ouverte : écris le message dans son onglet")
        : setCompose({
            id: r.s.id,
            title: "Reprendre avec un message",
            subtitle: r.s.title,
            project: r.g.project,
            send: "Reprendre et envoyer",
            choice,
            run: (text, c) =>
              act(
                "/open",
                {
                  dir: r.s.dir,
                  id: r.s.id,
                  args: withChoice(args, c),
                  prompt: text,
                },
                "Reprise avec le message…",
                "last"
              ),
          }),
    copy: (s) =>
      navigator.clipboard.writeText(`claude --resume ${s.id}`).then(
        () =>
          toast.success("Commande copiée", {
            description: `claude --resume ${s.id}`,
          }),
        () => toast.error("Copie impossible")
      ),
    rename: (s) =>
      named(
        "Renommer la session",
        s.named ? s.title : "",
        "Renommer",
        (title) => act("/rename", { dir: s.dir, id: s.id, title }, "Renommage…"),
        { label: "Titre automatique", hint: "Un modèle rédige le titre d'après la conversation", run: () => act("/rename", { dir: s.dir, id: s.id, auto: true }, "Titre automatique…") },
        s.id
      ),
    exportMd: (r) => exportMarkdown([{ s: r.s, project: tilde(r.g.project, home) }]).catch(() => toast.error("Export impossible")),
    move: async (rs) => {
      const to = await pickDir(rs.length > 1 ? `Déplacer ${plural(rs.length)} vers…` : `Déplacer « ${rs[0].s.title} » vers…`, rs[0].s.cwd)
      const items = rs.filter((r) => r.s.cwd !== to).map((r) => ({ dir: r.s.dir, id: r.s.id }))
      if (to && items.length && (await act("/reassign", { items, to }, "Déplacement…"))) unpick(items)
    },
    moveGroup: async (g: Group, items: Item[]) => {
      const to = await pickDir(`Réassigner ${plural(items.length)} vers…`, g.project)
      if (to) act("/reassign", { items, to }, "Réassignation…")
    },
    group: (ids, project, name) => (name === null ? newGroup(ids, project) : addTo(ids, project, name)),
    ungroup: (ids, x = vg) => {
      if (x) saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, ids: g.ids.filter((i) => !ids.includes(i)) } : g)))
    },
    pickDir,
    stop: (ids) =>
      setAsk({
        title: ids.length > 1 ? `Fermer ${ids.length} sessions ouvertes ?` : "Fermer la session ouverte ?",
        rows: ids,
        body: "Le processus claude reçoit SIGTERM ; la conversation reste reprenable.",
        confirm: "Fermer",
        run: async () => {
          const rs = await Promise.all(ids.map((id) => api.post("/session-close", { id })))
          const bad = rs.find((r) => !r.ok)
          ;(bad ? toast.error : toast.success)(bad ? bad.out : ids.length > 1 ? "Sessions fermées" : "Session fermée")
          // Le processus met un instant à finir : on retire les sessions tout de suite, puis on relit jusqu'à confirmation.
          const done = new Set(ids.filter((_, i) => rs[i].ok))
          setData((d) => d && { ...d, live: d.live.filter((x) => !done.has(x)), waiting: d.waiting.filter((x) => !done.has(x)) })
          for (let i = 0; i < 4; i++) {
            await new Promise((r) => setTimeout(r, 1000))
            await reload()
          }
        },
      }),
    remove: (items, what, after) =>
      setAsk({
        title: "Mettre à la corbeille ?",
        rows: items.map((x) => x.id),
        removes: true,
        body: `${what}${items.some((x) => live.has(x.id)) ? "\n\nLes sessions ouvertes seront d'abord fermées." : ""}\n\nLes sessions restent restaurables 30 jours (menu de la barre latérale : Corbeille).`,
        confirm: "Mettre à la corbeille",
        run: async () => {
          after?.()
          unpick(items)
          const r = await api.post("/session-trash-add?close=1", { items })
          await reload()
          if (!r.ok) return void toast.error(r.out)
          toast.success(r.out.split("\n")[0], {
            action: {
              label: "Annuler",
              onClick: async () => {
                const b = await api.post("/session-trash-restore", { ids: items.map((x) => x.id) })
                ;(b.ok ? toast.success : toast.error)(b.out.split("\n")[0])
                await reload()
              },
            },
            duration: 8000,
          })
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
  return { actions, groupActs, groupRows, targets, newSession, purgeAuto }
}
