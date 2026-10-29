import { useRef, type Dispatch, type SetStateAction } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { watchTrust } from "@/lib/trust"
import { api, type Caps, type Data, type Group, type Hit, type Item, type Project, type Snippet, type VGroup } from "@/lib/api"
import { segs } from "@/lib/paths"
import { exportMarkdown } from "@/lib/export"
import { size, tilde } from "@/lib/format"
import { withChoice, type Choice } from "@/lib/models"
import { byDate, groupKey, HOT, type View } from "@/lib/sessionsView"
import type { Route } from "@/lib/route"
import type { Actions, GroupActions, ProjectActions, Row } from "@/components/actions"
import type { Ask } from "@/components/Confirm"
import type { ComposeAsk, Target } from "@/components/Compose"
import type { Pick } from "@/components/DirPicker"
import type { Ask as PromptAsk } from "@/components/Prompt"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { copyText } from "@/lib/utils"

const ns = (count: number) => i18n.t("shell.count.sessions", { count })
const NO_CAPS: Caps = { hotCache: false, keepWarm: false, config: false, memory: false, tuiPrompts: false, resume: false }

// Tout ce que l'application sait faire d'une session ou d'un groupe (ouvrir, reprendre, renommer, déplacer, grouper,
// étiqueter, supprimer…), regroupé dans l'objet Actions que les composants lisent par contexte.
export function useAppActions(c: {
  data: Data | null
  setData: Dispatch<SetStateAction<Data | null>>
  reload: () => Promise<void>
  home: string
  q: string
  go: (r: Route) => void
  read: (s: { id: string }) => void
  now: number
  live: Set<string>
  waiting: Set<string>
  hitMap: Map<string, Hit> | null
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
  setInfo: (r: Row | null) => void
  setShare: (r: Row | null) => void
  setPicker: (p: Pick | null) => void
}) {
  const { data, setData, reload, home, q, go, read, now, live, waiting, hitMap, vgroups, favs, vg, picked, setPicked, selected, everyId, view, args, choice, snippets, setAsk, setPrompt, setCompose, setInfo, setShare, setPicker } = c
  const lastPick = useRef<string | null>(null) // dernière case cochée, début d'une sélection de plage
  const gkey = (x: VGroup) => groupKey(x, home)
  // Une action : toast de progression puis de résultat, erreurs jamais muettes, liste rechargée.
  const act = async (run: () => Promise<{ message?: string } | void>, busy: string, pick: "first" | "last" = "first", launch?: "new" | "open") => {
    const id = toast.loading(busy)
    const ok = await reportAction(run, { id, pick })
    if (ok && launch === "new") void watchTrust(home)
    await reload().catch(() => {}) // le résultat de l'action est déjà affiché ; un rechargement raté ne doit pas le masquer
    // Une session lancée met un instant à créer son transcript : on relit jusqu'à ce qu'elle apparaisse, sans retenir l'appelant
    // (la fenêtre de saisie se ferme dès le lancement).
    if (ok && launch) {
      void (async () => {
        for (let i = 0; i < 8; i++) {
          await new Promise((res) => setTimeout(res, 700))
          await reload().catch(() => {})
        }
      })()
    }
    return ok
  }

  // children : la modale propose aussi « ajouter tous les sous-dossiers » (alors children vaut true dans la réponse).
  const pickWith = (title: string, start: string, children = false) =>
    new Promise<{ path: string; children: boolean } | null>((resolve) =>
      setPicker({
        title,
        start,
        children,
        done: (p, c) => {
          setPicker(null)
          resolve(p ? { path: p, children: !!c } : null)
        },
      })
    )
  const pickDir = (title: string, start: string) => pickWith(title, start).then((r) => r?.path ?? null)

  // Groupes virtuels et favoris : mise à jour immédiate de l'affichage, puis enregistrement côté serveur.
  const saveStore = async (groups: VGroup[], favList: string[]) => {
    setData((d) => d && { ...d, vgroups: groups, favs: favList })
    try {
      await api.vgroups({ groups, favs: favList })
    } catch (e) {
      toast.error(errMsg(e))
      await reload()
    }
  }
  const saveGroups = (groups: VGroup[]) => saveStore(groups, [...favs])
  // Projets : ensembles nommés de dossiers de travail (un dossier peut appartenir à plusieurs).
  const projects = data?.projects ?? []
  const saveProjects = async (next: Project[]) => {
    setData((d) => d && { ...d, projects: next })
    try {
      await api.vgroups({ groups: vgroups, favs: [...favs], projects: next })
    } catch (e) {
      toast.error(errMsg(e))
      await reload()
    }
  }
  const projectActs: ProjectActions = {
    add: (dir, name) => {
      const put = (n: string) => {
        const has = projects.some((p) => p.name === n)
        saveProjects(has ? projects.map((p) => (p.name === n && !p.dirs.includes(dir) ? { ...p, dirs: [...p.dirs, dir] } : p)) : [...projects, { name: n, dirs: [dir] }])
        toast.success(i18n.t("shell.act.addedToProject", { dir: tilde(dir, home), name: n }))
      }
      if (name !== null) put(name)
      else
        named(i18n.t("shell.act.newProject"), "", i18n.t("shell.act.create"), (n) => {
          const t = n.trim()
          if (projects.some((p) => p.name === t)) toast.error(i18n.t("shell.act.projectExists", { name: t }))
          else put(t)
        })
    },
    remove: (dir, name) => saveProjects(projects.map((p) => (p.name === name ? { ...p, dirs: p.dirs.filter((d) => d !== dir) } : p))),
    promptAll: (p) => {
      const ts = targets().filter((x) => p.dirs.includes(x.project))
      if (!ts.length) return void toast.error(i18n.t("shell.act.promptAllNone"))
      const count = ts.length
      setCompose({
        id: "all:" + p.name,
        title: i18n.t("shell.act.promptAllTitle", { name: p.name }),
        subtitle: i18n.t("shell.act.promptAllSub", { count }),
        project: ts[0].project,
        warning: i18n.t("shell.act.promptAllWarning", { count }),
        send: i18n.t("shell.act.promptAllSend", { count }),
        choice,
        run: async (prompt, c) => {
          let started = 0
          for (const t of ts) {
            const ok = await act(() => api.newSession({ ...(t.cwd ? { cwd: t.cwd } : { dir: t.dir, id: t.id }), prompt, args: withChoice(args, c) }), i18n.t("shell.act.starting"), "last", "new")
            if (ok) started++
          }
          return started > 0
        },
      })
    },
    rename: (p) =>
      named(i18n.t("shell.act.renameProject", { name: p.name }), p.name, i18n.t("shell.act.rename"), (n) => {
        const t = n.trim()
        if (t === p.name) return
        if (projects.some((x) => x.name === t)) return void toast.error(i18n.t("shell.act.projectExists", { name: t }))
        saveProjects(projects.map((x) => (x.name === p.name ? { ...x, name: t } : x)))
        void api.notesSave({ key: "p/" + p.name, to: "p/" + t }).catch(() => {}) // la note suit le projet
        go({ kind: "project", name: t })
      }),
    delete: (p) =>
      setAsk({
        title: i18n.t("shell.act.deleteProjectTitle", { name: p.name }),
        body: i18n.t("shell.act.deleteProjectBody"),
        confirm: i18n.t("common.delete"),
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
      toast.success(i18n.t("shell.act.addedToGroup", { sessions: ns(ids.length), name }), {
        action: {
          label: i18n.t("shell.act.view"),
          onClick: () => go({ kind: "group", name, key: gkey(vg) }),
        },
      })
  }
  const newGroup = (ids: string[], project: string) =>
    named(i18n.t("shell.act.newGroup", { dir: tilde(project, home) }), "", i18n.t("shell.act.create"), (n) => addTo(ids, project, n))
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
  const newSession = (title: string, subtitle: string, ts: Target[], initial = "", onSent?: () => void) =>
    ts.length &&
    setCompose({
      id: "new:" + ts[0].project + title,
      title,
      subtitle,
      project: ts[0].project,
      initial,
      send: i18n.t("shell.head.start"),
      choice,
      targets: ts,
      run: (prompt, c, t = ts[0]) =>
        act(() => api.newSession({ ...(t.cwd ? { cwd: t.cwd } : { dir: t.dir, id: t.id }), prompt, args: withChoice(args, c) }), i18n.t("shell.act.starting"), "last", "new").then((ok) => {
          if (ok) onSent?.()
          return ok
        }),
    })
  const groupRows = (x: VGroup) =>
    x.ids
      .map((id) => everyId.get(id))
      .filter((r): r is Row => !!r)
      .sort(byDate)
  // Références des sessions d'un groupe, à coller dans un nouveau prompt.
  const refs = (x: VGroup) =>
    i18n.t("shell.act.refsHeader", { name: x.name, dir: tilde(x.project, home) }) +
    "\n" +
    groupRows(x)
      .map((r) => i18n.t("shell.act.refsLine", { title: r.s.title, id: r.s.id, path: `${data?.root ?? "~/.claude/projects"}/${r.s.dir}/${r.s.id}.jsonl` }))
      .join("\n")
  const groupActs: GroupActions = {
    rows: groupRows,
    show: (x) => go({ kind: "group", name: x.name, key: gkey(x) }),
    open: (x) => openMany(groupRows(x).filter((r) => !r.g.missing && !live.has(r.s.id))),
    refs: (x) =>
      copyText(refs(x)).then(
        () =>
          toast.success(i18n.t("shell.act.refsCopied"), {
            description: i18n.t("shell.act.refsCopiedDesc", { sessions: ns(groupRows(x).length), name: x.name }),
          }),
        () => toast.error(i18n.t("shell.act.copyFailed"))
      ),
    prompt: (x) => {
      const first = groupRows(x)[0]
      if (!first) return
      setCompose({
        id: "g:" + x.project + x.name,
        title: i18n.t("shell.act.newWithRefs"),
        subtitle: i18n.t("shell.act.groupSubtitle", { name: x.name, dir: tilde(x.project, home) }),
        project: x.project,
        initial: refs(x) + "\n\n",
        send: i18n.t("shell.head.start"),
        choice,
        run: (prompt, c) =>
          act(
            () =>
              api.newSession({
                dir: first.s.dir,
                id: first.s.id,
                prompt,
                args: withChoice(args, c),
              }),
            i18n.t("shell.act.starting"),
            "last",
            "new"
          ),
      })
    },
    export: (x) =>
      exportMarkdown(
        groupRows(x).map((r) => ({
          s: r.s,
          project: tilde(r.g.project, home),
        })),
        i18n.t("shell.act.groupExportTitle", { name: x.name })
      ).catch(() => toast.error(i18n.t("shell.act.exportFailed"))),
    select: (x) => setPicked((p) => new Set([...p, ...groupRows(x).map((r) => r.s.id)])),
    fav: (x) => saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, fav: !g.fav } : g))),
    rename: (x) =>
      named(i18n.t("shell.act.renameGroup", { name: x.name }), x.name, i18n.t("shell.act.rename"), (n) => {
        if (n === x.name) return
        if (vgroups.some((g) => g.project === x.project && g.name === n)) return void toast.error(i18n.t("shell.act.groupExists", { name: n }))
        saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, name: n } : g)))
        if (vg && same(vg, x)) go({ kind: "group", name: n, key: gkey(x) })
      }),
    remove: (x) =>
      setAsk({
        title: i18n.t("shell.act.deleteGroupTitle", { name: x.name }),
        body: i18n.t("shell.act.deleteGroupBody"),
        confirm: i18n.t("shell.menu.deleteGroup"),
        run: () => {
          saveGroups(vgroups.filter((g) => !same(g, x)))
          if (vg && same(vg, x)) go({ kind: "dir", key: gkey(x) })
        },
      }),
  }

  // Plusieurs sessions dans des onglets de terminal, l'une après l'autre.
  const openMany = async (rs: Row[]) => {
    const id = toast.loading(i18n.t("shell.tabs.opening", { sessions: ns(rs.length) }))
    let n = 0
    for (const r of rs) if (await api.open({ dir: r.s.dir, id: r.s.id, args }).then(() => true, () => false)) n++
    ;(n === rs.length ? toast.success : toast.error)(i18n.t("shell.act.opened", { n, total: rs.length }), { id })
  }


  const unpick = (items: Item[]) =>
    setPicked((p) => {
      const n = new Set(p)
      for (const it of items) n.delete(it.id)
      return n
    })

  const addChildDirs = async (p: string) => {
    let n: number
    try {
      n = (await api.dirsAdd(p, true)).n
    } catch (e) {
      return void toast.error(errMsg(e))
    }
    await reload()
    toast.success(i18n.t("shell.act.childDirsAdded", { count: n }))
  }

  const addDirPath = async (p: string) => {
    try {
      await api.dirsAdd(p)
    } catch (e) {
      return void toast.error(errMsg(e))
    }
    await reload()
    go({ kind: "dir", key: segs(p, home).join("/") })
  }

  const actions: Actions = {
    home,
    now,
    root: data?.root ?? "~/.claude/projects",
    rows: [...everyId.values()].sort((x, y) => y.s.mtime - x.s.mtime),
    q,
    live,
    background: new Set(data?.background),
    waiting,
    asking: new Set(data?.asking),
    hit: (id) => hitMap?.get(id),
    registeredDirs: data?.dirs ?? [],
    addDir: () =>
      void pickWith(i18n.t("shell.act.addDirTitle"), home, true).then((r) => {
        if (r) void (r.children ? addChildDirs(r.path) : addDirPath(r.path))
      }),
    addDirPath,
    removeDir: async (project) => {
      try {
        await api.dirsRemove(project)
      } catch (e) {
        return void toast.error(errMsg(e))
      }
      await reload()
    },
    openPerms: (project) => {
      const dir = data?.groups.find((g) => g.project === project)?.sessions[0]?.dir
      if (dir) go({ kind: "projectConfig", dir })
      else go({ kind: "config", tab: "permissions" })
    },
    folder: (project) => data?.groups.find((g) => g.project === project),
    vgroups,
    projects,
    projectActs,
    favs,
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
    open: (b) => act(() => api.open({ ...b, args }), i18n.t("shell.act.openingTerminal"), "last", "open"),
    // Un fournisseur inconnu ou une réponse ancienne : aucune fonction optionnelle (l'interface reste sobre plutôt que fausse).
    caps: (s) => data?.providers?.find((p) => p.id === (s.provider ?? "claude"))?.caps ?? NO_CAPS,
    defaultChoice: choice,
    snippets,
    startSession: async (project, prompt, c) => {
      const t = targets().find((x) => x.project === project)
      if (!t) return false
      await act(() => api.newSession({ ...(t.cwd ? { cwd: t.cwd } : { dir: t.dir, id: t.id }), prompt, args: withChoice(args, c) }), i18n.t("shell.act.starting"), "last", "new")
      return true
    },
    newSession: (project, initial, onSent) => {
      const t = targets().filter((x) => x.project === project)
      if (t.length) newSession(i18n.t("shell.header.newSession"), t[0].label, t, initial, onSent)
    },
    resumeWith: (r) =>
      live.has(r.s.id)
        ? void toast.info(i18n.t("shell.act.alreadyOpen"))
        : setCompose({
            id: r.s.id,
            title: i18n.t("shell.act.resumeWith"),
            subtitle: r.s.title,
            project: r.g.project,
            send: i18n.t("shell.act.resumeSend"),
            choice,
            run: (text, c) =>
              act(
                () =>
                  api.open({
                    dir: r.s.dir,
                    id: r.s.id,
                    args: withChoice(args, c),
                    prompt: text,
                  }),
                i18n.t("shell.act.resuming"),
                "last",
                "open"
              ),
          }),
    resumeSend: (r, text) => act(() => api.open({ dir: r.s.dir, id: r.s.id, args: withChoice(args, choice), prompt: text }), i18n.t("shell.act.resuming"), "last", "open"),
    copy: (s) =>
      copyText(s.id).then(
        () =>
          toast.success(i18n.t("shell.act.idCopied"), {
            description: s.id,
          }),
        () => toast.error(i18n.t("shell.act.copyFailed"))
      ),
    rename: (s) =>
      named(
        i18n.t("shell.act.renameSession"),
        s.named ? s.title : "",
        i18n.t("shell.act.rename"),
        (title) => act(() => api.rename(s, { title }), i18n.t("shell.act.renaming")),
        { label: i18n.t("shell.act.autoTitle"), hint: i18n.t("shell.act.autoTitleHint"), run: () => act(() => api.rename(s, { auto: true }), i18n.t("shell.act.autoTitling")) },
        s.id
      ),
    info: setInfo,
    share: setShare,
    exportMd: (r) => exportMarkdown([{ s: r.s, project: tilde(r.g.project, home) }]).catch(() => toast.error(i18n.t("shell.act.exportFailed"))),
    move: async (rs) => {
      const to = await pickDir(rs.length > 1 ? i18n.t("shell.act.moveMany", { sessions: ns(rs.length) }) : i18n.t("shell.act.moveOne", { title: rs[0].s.title }), rs[0].s.cwd)
      const items = rs.filter((r) => r.s.cwd !== to).map((r) => ({ dir: r.s.dir, id: r.s.id }))
      if (to && items.length && (await act(() => api.reassign(items, to), i18n.t("shell.act.moving")))) unpick(items)
    },
    moveGroup: async (g: Group, items: Item[]) => {
      const to = await pickDir(i18n.t("shell.act.reassignTo", { sessions: ns(items.length) }), g.project)
      if (to) act(() => api.reassign(items, to), i18n.t("shell.act.reassigning"))
    },
    group: (ids, project, name) => (name === null ? newGroup(ids, project) : addTo(ids, project, name)),
    ungroup: (ids, x = vg) => {
      if (x) saveGroups(vgroups.map((g) => (same(g, x) ? { ...g, ids: g.ids.filter((i) => !ids.includes(i)) } : g)))
    },
    pickDir,
    stop: (ids, onDone) =>
      setAsk({
        title: ids.length > 1 ? i18n.t("shell.act.closeMany", { n: ids.length }) : i18n.t("shell.act.closeOne"),
        rows: ids,
        body: i18n.t("shell.act.closeBody"),
        confirm: i18n.t("common.close"),
        run: async () => {
          const rs = await Promise.allSettled(ids.map((id) => api.sessionClose(id)))
          const bad = rs.find((r): r is PromiseRejectedResult => r.status === "rejected")
          ;(bad ? toast.error : toast.success)(bad ? errMsg(bad.reason) : ids.length > 1 ? i18n.t("shell.act.closedMany") : i18n.t("shell.act.closedOne"))
          // Le processus met un instant à finir : on retire les sessions tout de suite, puis on relit jusqu'à confirmation.
          const done = new Set(ids.filter((_, i) => rs[i].status === "fulfilled"))
          if (!bad) onDone?.()
          setData((d) => d && { ...d, live: d.live.filter((x) => !done.has(x)), waiting: d.waiting.filter((x) => !done.has(x)) })
          for (let i = 0; i < 4; i++) {
            await new Promise((r) => setTimeout(r, 1000))
            await reload()
          }
        },
      }),
    remove: (items, what, after) =>
      setAsk({
        title: i18n.t("shell.act.trashTitle"),
        rows: items.map((x) => x.id),
        removes: true,
        body: `${what}${items.some((x) => live.has(x.id)) ? "\n\n" + i18n.t("shell.act.trashClosing") : ""}\n\n${i18n.t("shell.act.trashRestorable")}`,
        confirm: i18n.t("shell.act.trashConfirm"),
        run: async () => {
          after?.()
          unpick(items)
          let r: { message: string }
          try {
            r = await api.trashSessionsAdd(items)
          } catch (e) {
            await reload()
            return void toast.error(errMsg(e))
          }
          await reload()
          toast.success(r.message.split("\n")[0], {
            action: {
              label: i18n.t("common.cancel"),
              onClick: async () => {
                await reportAction(() => api.trashSessionsRestore(items.map((x) => x.id)).then((b) => ({ message: b.message.split("\n")[0] })))
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
      title: i18n.t("shell.act.purgeTitle", { n }),
      body: i18n.t("shell.act.purgeBody", { size: size(view.akb) }),
      confirm: i18n.t("shell.act.purgeConfirm", { n }),
      run: () => act(() => api.rmSessions(view.autoItems), i18n.t("shell.act.purging", { n })),
    })
  }
  return { actions, groupActs, groupRows, targets, newSession, purgeAuto }
}
