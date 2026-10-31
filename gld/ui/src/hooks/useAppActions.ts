import { useRef, type Dispatch, type SetStateAction } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { watchTrust } from "@/lib/trust"
import { api, type Caps, type Data, type Group, type Hit, type Item, type Project, type Snippet, type VGroup } from "@/lib/api"
import { segs } from "@/lib/paths"
import { exportMarkdown } from "@/lib/export"
import { size, tilde } from "@/lib/format"
import { withChoice, type Choice } from "@/lib/models"
import { asked, byDate, groupKey, HOT, type View } from "@/lib/sessionsView"
import type { Route } from "@/lib/route"
import type { Actions, GroupActions, ProjectActions, Row } from "@/components/actions"
import type { Ask } from "@/components/Confirm"
import type { ComposeAsk, Target } from "@/components/Compose"
import type { Pick } from "@/components/DirPicker"
import type { Ask as PromptAsk } from "@/components/Prompt"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { copyText } from "@/lib/utils"
import { markClosing } from "./useAppData"

const sessionsCount = (count: number) => i18n.t("shell.count.sessions", { count })
const NO_CAPS: Caps = { hotCache: false, keepWarm: false, config: false, memory: false, tuiPrompts: false, resume: false }
const DEFAULT_PROJECTS_ROOT = "~/.claude/projects"
const LAUNCH_RELOAD_ATTEMPTS = 8
const LAUNCH_RELOAD_DELAY_MS = 700
const CLOSE_RELOAD_ATTEMPTS = 4
const CLOSE_RELOAD_DELAY_MS = 1000
const TOUCH_RELOAD_DELAY_MS = 2000
const TRASH_UNDO_TOAST_DURATION_MS = 8000
const ROW_SELECTOR = "main [data-row]"

const wait = (delayMs: number) => new Promise((resolve) => setTimeout(resolve, delayMs))
const firstLine = (message: string) => message.split("\n")[0]

// Tout ce que l'application sait faire d'une session ou d'un groupe (ouvrir, reprendre, renommer, déplacer, grouper,
// étiqueter, supprimer…), regroupé dans l'objet Actions que les composants lisent par contexte.
export function useAppActions(context: {
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
  const {
    data,
    setData,
    reload,
    home,
    q: searchQuery,
    go,
    read,
    now,
    live,
    waiting,
    hitMap,
    vgroups,
    favs,
    vg: shownGroup,
    picked,
    setPicked,
    selected,
    everyId,
    view,
    args,
    choice,
    snippets,
    setAsk,
    setPrompt,
    setCompose,
    setInfo,
    setShare,
    setPicker,
  } = context
  const lastPick = useRef<string | null>(null) // dernière case cochée, début d'une sélection de plage
  const projectsRoot = data?.root ?? DEFAULT_PROJECTS_ROOT
  const groupKeyOf = (group: VGroup) => groupKey(group, home)
  const reloadQuietly = () => reload().catch(() => {})
  // Une action : toast de progression puis de résultat, erreurs jamais muettes, liste rechargée.
  const act = async (run: () => Promise<{ message?: string } | void>, busy: string, pick: "first" | "last" = "first", launch?: "new" | "open") => {
    const toastId = toast.loading(busy)
    const succeeded = await reportAction(run, { id: toastId, pick })
    if (succeeded && launch === "new") void watchTrust(home)
    await reloadQuietly() // le résultat de l'action est déjà affiché ; un rechargement raté ne doit pas le masquer
    // Une session lancée met un instant à créer son transcript : on relit jusqu'à ce qu'elle apparaisse, sans retenir l'appelant
    // (la fenêtre de saisie se ferme dès le lancement).
    if (succeeded && launch) {
      void (async () => {
        for (let attempt = 0; attempt < LAUNCH_RELOAD_ATTEMPTS; attempt++) {
          await wait(LAUNCH_RELOAD_DELAY_MS)
          await reloadQuietly()
        }
      })()
    }
    return succeeded
  }
  const startSessionIn = (target: Target, prompt: string, chosen: Choice) => {
    const location = target.cwd ? { cwd: target.cwd } : { dir: target.dir, id: target.id }
    return act(() => api.newSession({ ...location, prompt, args: withChoice(args, chosen) }), i18n.t("shell.act.starting"), "last", "new")
  }

  // children : la modale propose aussi « ajouter tous les sous-dossiers » (alors children vaut true dans la réponse).
  const pickWith = (title: string, start: string, children = false) =>
    new Promise<{ path: string; children: boolean } | null>((resolve) =>
      setPicker({
        title,
        start,
        children,
        done: (pickedPath, includeChildren) => {
          setPicker(null)
          resolve(pickedPath ? { path: pickedPath, children: !!includeChildren } : null)
        },
      })
    )
  const pickDir = (title: string, start: string) => pickWith(title, start).then((picked) => picked?.path ?? null)

  // Groupes virtuels et favoris : mise à jour immédiate de l'affichage, puis enregistrement côté serveur.
  const saveStore = async (groups: VGroup[], favList: string[]) => {
    setData((currentData) => currentData && { ...currentData, vgroups: groups, favs: favList })
    try {
      await api.vgroups({ groups, favs: favList })
    } catch (error) {
      toast.error(errMsg(error))
      await reload()
    }
  }
  const saveGroups = (groups: VGroup[]) => saveStore(groups, [...favs])
  // Projets : ensembles nommés de dossiers de travail (un dossier peut appartenir à plusieurs).
  const projects = data?.projects ?? []
  const saveProjects = async (nextProjects: Project[]) => {
    setData((currentData) => currentData && { ...currentData, projects: nextProjects })
    try {
      await api.vgroups({ groups: vgroups, favs: [...favs], projects: nextProjects })
    } catch (error) {
      toast.error(errMsg(error))
      await reload()
    }
  }
  const projectExists = (projectName: string) => projects.some((project) => project.name === projectName)
  const projectActs: ProjectActions = {
    add: (dir, name) => {
      const addDirToProject = (projectName: string) => {
        if (projectExists(projectName)) {
          saveProjects(
            projects.map((project) => {
              const shouldAddDir = project.name === projectName && !project.dirs.includes(dir)
              return shouldAddDir ? { ...project, dirs: [...project.dirs, dir] } : project
            })
          )
        } else {
          saveProjects([...projects, { name: projectName, dirs: [dir] }])
        }
        toast.success(i18n.t("shell.act.addedToProject", { dir: tilde(dir, home), name: projectName }))
      }
      if (name !== null) return addDirToProject(name)
      askForName(i18n.t("shell.act.newProject"), "", i18n.t("shell.act.create"), (typedName) => {
        const trimmedName = typedName.trim()
        if (projectExists(trimmedName)) toast.error(i18n.t("shell.act.projectExists", { name: trimmedName }))
        else addDirToProject(trimmedName)
      })
    },
    remove: (dir, name) =>
      saveProjects(projects.map((project) => (project.name === name ? { ...project, dirs: project.dirs.filter((projectDir) => projectDir !== dir) } : project))),
    promptAll: (project) => {
      const projectTargets = targets().filter((target) => project.dirs.includes(target.project))
      if (!projectTargets.length) return void toast.error(i18n.t("shell.act.promptAllNone"))
      const count = projectTargets.length
      setCompose({
        id: "all:" + project.name,
        title: i18n.t("shell.act.promptAllTitle", { name: project.name }),
        subtitle: i18n.t("shell.act.promptAllSub", { count }),
        project: projectTargets[0].project,
        warning: i18n.t("shell.act.promptAllWarning", { count }),
        send: i18n.t("shell.act.promptAllSend", { count }),
        choice,
        run: async (prompt, chosen) => {
          let started = 0
          for (const target of projectTargets) {
            const succeeded = await startSessionIn(target, prompt, chosen)
            if (succeeded) started++
          }
          return started > 0
        },
      })
    },
    rename: (project) =>
      askForName(i18n.t("shell.act.renameProject", { name: project.name }), project.name, i18n.t("shell.act.rename"), (typedName) => {
        const trimmedName = typedName.trim()
        if (trimmedName === project.name) return
        if (projectExists(trimmedName)) return void toast.error(i18n.t("shell.act.projectExists", { name: trimmedName }))
        saveProjects(projects.map((candidate) => (candidate.name === project.name ? { ...candidate, name: trimmedName } : candidate)))
        void api.notesSave({ key: "p/" + project.name, to: "p/" + trimmedName }).catch(() => {}) // la note suit le projet
        go({ kind: "project", name: trimmedName })
      }),
    delete: (project) =>
      setAsk({
        title: i18n.t("shell.act.deleteProjectTitle", { name: project.name }),
        body: i18n.t("shell.act.deleteProjectBody"),
        confirm: i18n.t("common.delete"),
        run: async () => {
          await saveProjects(projects.filter((candidate) => candidate.name !== project.name))
          go({ kind: "recent" })
        },
      }),
  }
  const isSameGroup = (first: VGroup, second: VGroup) => first.name === second.name && first.project === second.project
  const askForName = (title: string, value: string, confirm: string, run: PromptAsk["run"], auto?: PromptAsk["auto"], row?: string) =>
    setPrompt({ title, value, confirm, run, auto, row })
  // Un groupe est un sous-dossier virtuel : il ne contient que des sessions de son dossier de travail.
  const addTo = (ids: string[], project: string, name: string) => {
    const targetGroup = { name, project, ids }
    const groupExists = vgroups.some((group) => isSameGroup(group, targetGroup))
    if (groupExists) {
      saveGroups(vgroups.map((group) => (isSameGroup(group, targetGroup) ? { ...group, ids: [...new Set([...group.ids, ...ids])] } : group)))
    } else {
      saveGroups([...vgroups, targetGroup])
    }
    if (!ids.length) return
    toast.success(i18n.t("shell.act.addedToGroup", { sessions: sessionsCount(ids.length), name }), {
      action: {
        label: i18n.t("shell.act.view"),
        onClick: () => go({ kind: "group", name, key: groupKeyOf(targetGroup) }),
      },
    })
  }
  const newGroup = (ids: string[], project: string) =>
    askForName(i18n.t("shell.act.newGroup", { dir: tilde(project, home) }), "", i18n.t("shell.act.create"), (typedName) => addTo(ids, project, typedName))
  // Dossiers où lancer une nouvelle session : ceux qui existent encore, triés par nom.
  // Un dossier enregistré sans session se lance par son chemin (cwd).
  const targets = (): Target[] =>
    (data?.groups ?? [])
      .filter((folder) => !folder.missing)
      .map((folder) => ({
        folder,
        latestSession: [...folder.sessions].sort((first, second) => asked(second) - asked(first))[0],
      }))
      .sort((first, second) => tilde(first.folder.project, home).localeCompare(tilde(second.folder.project, home)))
      .map(({ folder, latestSession }) => ({
        label: tilde(folder.project, home),
        project: folder.project,
        dir: latestSession?.dir ?? "",
        id: latestSession?.id ?? "",
        ...(latestSession ? {} : { cwd: folder.project }),
      }))
  const newSession = (title: string, subtitle: string, sessionTargets: Target[], initial = "", onSent?: () => void) =>
    sessionTargets.length &&
    setCompose({
      id: "new:" + sessionTargets[0].project + title,
      title,
      subtitle,
      project: sessionTargets[0].project,
      initial,
      send: i18n.t("shell.head.start"),
      choice,
      targets: sessionTargets,
      run: (prompt, chosen, target = sessionTargets[0]) =>
        startSessionIn(target, prompt, chosen).then((succeeded) => {
          if (succeeded) onSent?.()
          return succeeded
        }),
    })
  const groupRows = (group: VGroup) =>
    group.ids
      .map((sessionId) => everyId.get(sessionId))
      .filter((row): row is Row => !!row)
      .sort(byDate)
  // Références des sessions d'un groupe, à coller dans un nouveau prompt.
  const refs = (group: VGroup) => {
    const header = i18n.t("shell.act.refsHeader", { name: group.name, dir: tilde(group.project, home) })
    const lines = groupRows(group).map((row) =>
      i18n.t("shell.act.refsLine", { title: row.s.title, id: row.s.id, path: `${projectsRoot}/${row.s.dir}/${row.s.id}.jsonl` })
    )
    return header + "\n" + lines.join("\n")
  }
  const groupActs: GroupActions = {
    rows: groupRows,
    show: (group) => go({ kind: "group", name: group.name, key: groupKeyOf(group) }),
    open: (group) => openMany(groupRows(group).filter((row) => !row.g.missing && !live.has(row.s.id))),
    refs: (group) =>
      copyText(refs(group)).then(
        () =>
          toast.success(i18n.t("shell.act.refsCopied"), {
            description: i18n.t("shell.act.refsCopiedDesc", { sessions: sessionsCount(groupRows(group).length), name: group.name }),
          }),
        () => toast.error(i18n.t("shell.act.copyFailed"))
      ),
    prompt: (group) => {
      const firstRow = groupRows(group)[0]
      if (!firstRow) return
      setCompose({
        id: "g:" + group.project + group.name,
        title: i18n.t("shell.act.newWithRefs"),
        subtitle: i18n.t("shell.act.groupSubtitle", { name: group.name, dir: tilde(group.project, home) }),
        project: group.project,
        initial: refs(group) + "\n\n",
        send: i18n.t("shell.head.start"),
        choice,
        run: (prompt, chosen) =>
          act(
            () =>
              api.newSession({
                dir: firstRow.s.dir,
                id: firstRow.s.id,
                prompt,
                args: withChoice(args, chosen),
              }),
            i18n.t("shell.act.starting"),
            "last",
            "new"
          ),
      })
    },
    export: (group) =>
      exportMarkdown(
        groupRows(group).map((row) => ({
          s: row.s,
          project: tilde(row.g.project, home),
        })),
        i18n.t("shell.act.groupExportTitle", { name: group.name })
      ).catch(() => toast.error(i18n.t("shell.act.exportFailed"))),
    select: (group) => setPicked((previous) => new Set([...previous, ...groupRows(group).map((row) => row.s.id)])),
    fav: (group) => saveGroups(vgroups.map((candidate) => (isSameGroup(candidate, group) ? { ...candidate, fav: !candidate.fav } : candidate))),
    rename: (group) =>
      askForName(i18n.t("shell.act.renameGroup", { name: group.name }), group.name, i18n.t("shell.act.rename"), (typedName) => {
        if (typedName === group.name) return
        const nameTaken = vgroups.some((candidate) => candidate.project === group.project && candidate.name === typedName)
        if (nameTaken) return void toast.error(i18n.t("shell.act.groupExists", { name: typedName }))
        saveGroups(vgroups.map((candidate) => (isSameGroup(candidate, group) ? { ...candidate, name: typedName } : candidate)))
        if (shownGroup && isSameGroup(shownGroup, group)) go({ kind: "group", name: typedName, key: groupKeyOf(group) })
      }),
    remove: (group) =>
      setAsk({
        title: i18n.t("shell.act.deleteGroupTitle", { name: group.name }),
        body: i18n.t("shell.act.deleteGroupBody"),
        confirm: i18n.t("shell.menu.deleteGroup"),
        run: () => {
          saveGroups(vgroups.filter((candidate) => !isSameGroup(candidate, group)))
          if (shownGroup && isSameGroup(shownGroup, group)) go({ kind: "dir", key: groupKeyOf(group) })
        },
      }),
  }

  // Plusieurs sessions dans des onglets de terminal, l'une après l'autre.
  const openMany = async (rows: Row[]) => {
    const toastId = toast.loading(i18n.t("shell.tabs.opening", { sessions: sessionsCount(rows.length) }))
    let openedCount = 0
    for (const row of rows) {
      const opened = await api.open({ dir: row.s.dir, id: row.s.id, args }).then(
        () => true,
        () => false
      )
      if (opened) openedCount++
    }
    const showResult = openedCount === rows.length ? toast.success : toast.error
    showResult(i18n.t("shell.act.opened", { n: openedCount, total: rows.length }), { id: toastId })
  }

  const unpick = (items: Item[]) =>
    setPicked((previous) => {
      const nextPicked = new Set(previous)
      for (const item of items) nextPicked.delete(item.id)
      return nextPicked
    })

  const addChildDirs = async (path: string) => {
    let addedCount: number
    try {
      addedCount = (await api.dirsAdd(path, true)).n
    } catch (error) {
      return void toast.error(errMsg(error))
    }
    await reload()
    toast.success(i18n.t("shell.act.childDirsAdded", { count: addedCount }))
  }

  const addDirPath = async (path: string) => {
    try {
      await api.dirsAdd(path)
    } catch (error) {
      return void toast.error(errMsg(error))
    }
    await reload()
    go({ kind: "dir", key: segs(path, home).join("/") })
  }

  const hotMinutesLeft = (session: { mtime: number }) => {
    const isHot = now && now - session.mtime < HOT
    if (!isHot) return null
    const secondsLeft = HOT - (now - session.mtime)
    return Math.min(HOT / 60, Math.max(1, Math.ceil(secondsLeft / 60)))
  }

  const togglePick = (id: string, range?: boolean) => {
    const rangeStart = lastPick.current
    lastPick.current = id
    setPicked((previous) => {
      const nextPicked = new Set(previous)
      const visibleIds = [...document.querySelectorAll<HTMLElement>(ROW_SELECTOR)].map((rowElement) => rowElement.dataset.id ?? "")
      const startIndex = rangeStart ? visibleIds.indexOf(rangeStart) : -1
      const endIndex = visibleIds.indexOf(id)
      const isRangeSelection = range && startIndex >= 0 && endIndex >= 0
      if (isRangeSelection) {
        const rangeIds = visibleIds.slice(Math.min(startIndex, endIndex), Math.max(startIndex, endIndex) + 1)
        for (const rangeId of rangeIds) nextPicked.add(rangeId)
      } else if (nextPicked.has(id)) {
        nextPicked.delete(id)
      } else {
        nextPicked.add(id)
      }
      return nextPicked
    })
  }

  const actions: Actions = {
    home,
    now,
    root: projectsRoot,
    rows: [...everyId.values()].sort((first, second) => asked(second.s) - asked(first.s)),
    q: searchQuery,
    live,
    background: new Set(data?.background),
    rc: new Set(data?.rc),
    waiting,
    asking: new Set(data?.asking),
    hit: (id) => hitMap?.get(id),
    registeredDirs: data?.dirs ?? [],
    addDir: () =>
      void pickWith(i18n.t("shell.act.addDirTitle"), home, true).then((picked) => {
        if (!picked) return
        if (picked.children) void addChildDirs(picked.path)
        else void addDirPath(picked.path)
      }),
    addDirPath,
    removeDir: async (project) => {
      try {
        await api.dirsRemove(project)
      } catch (error) {
        return void toast.error(errMsg(error))
      }
      await reload()
    },
    openPerms: (project) => {
      const dir = data?.groups.find((folder) => folder.project === project)?.sessions[0]?.dir
      if (dir) go({ kind: "projectConfig", dir })
      else go({ kind: "config", tab: "permissions" })
    },
    folder: (project) => data?.groups.find((folder) => folder.project === project),
    vgroups,
    projects,
    projectActs,
    favs,
    toggleFav: (ids) => {
      const addingFavs = ids.some((id) => !favs.has(id))
      const nextFavs = addingFavs ? [...new Set([...favs, ...ids])] : [...favs].filter((id) => !ids.includes(id))
      saveStore(vgroups, nextFavs)
    },
    inGroup: shownGroup ?? null,
    groupActs,
    picked,
    selected,
    pick: togglePick,
    hotLeft: hotMinutesLeft,
    touch: (id) => {
      const nowSeconds = Date.now() / 1000
      // Optimiste : la session travaille dès l'envoi (icône de la barre latérale), la relecture corrige si le prompt n'est pas parti.
      setData(
        (currentData) =>
          currentData && {
            ...currentData,
            live: currentData.live.includes(id) ? currentData.live : [...currentData.live, id],
            waiting: currentData.waiting.filter((waitingId) => waitingId !== id),
            groups: currentData.groups.map((folder) => ({
              ...folder,
              sessions: folder.sessions.map((session) => (session.id === id ? { ...session, mtime: nowSeconds, ask: nowSeconds } : session)),
            })),
          }
      )
      setTimeout(reloadQuietly, TOUCH_RELOAD_DELAY_MS) // le transcript a enregistré le prompt : on prend l'heure réelle
    },
    read,
    open: (item) => act(() => api.open({ ...item, args }), i18n.t("shell.act.openingTerminal"), "last", "open"),
    // Un fournisseur inconnu ou une réponse ancienne : aucune fonction optionnelle (l'interface reste sobre plutôt que fausse).
    caps: (session) => data?.providers?.find((provider) => provider.id === (session.provider ?? "claude"))?.caps ?? NO_CAPS,
    defaultChoice: choice,
    snippets,
    startSession: async (project, prompt, chosen) => {
      const target = targets().find((candidate) => candidate.project === project)
      if (!target) return false
      await startSessionIn(target, prompt, chosen)
      return true
    },
    newSession: (project, initial, onSent) => {
      const everyTarget = targets()
      const projectTargets = everyTarget.filter((target) => target.project === project)
      const otherTargets = everyTarget.filter((target) => target.project !== project)
      if (projectTargets.length) newSession(i18n.t("shell.header.newSession"), projectTargets[0].label, [...projectTargets, ...otherTargets], initial, onSent)
    },
    resumeWith: (row) => {
      if (live.has(row.s.id)) return void toast.info(i18n.t("shell.act.alreadyOpen"))
      return setCompose({
        id: row.s.id,
        title: i18n.t("shell.act.resumeWith"),
        subtitle: row.s.title,
        project: row.g.project,
        send: i18n.t("shell.act.resumeSend"),
        choice,
        run: (text, chosen) =>
          act(
            () =>
              api.open({
                dir: row.s.dir,
                id: row.s.id,
                args: withChoice(args, chosen),
                prompt: text,
              }),
            i18n.t("shell.act.resuming"),
            "last",
            "open"
          ),
      })
    },
    resumeSend: (row, text) =>
      act(() => api.open({ dir: row.s.dir, id: row.s.id, args: withChoice(args, choice), prompt: text }), i18n.t("shell.act.resuming"), "last", "open"),
    copy: (session) =>
      copyText(session.id).then(
        () =>
          toast.success(i18n.t("shell.act.idCopied"), {
            description: session.id,
          }),
        () => toast.error(i18n.t("shell.act.copyFailed"))
      ),
    rename: (session) =>
      askForName(
        i18n.t("shell.act.renameSession"),
        session.named ? session.title : "",
        i18n.t("shell.act.rename"),
        (title) => act(() => api.rename(session, { title }), i18n.t("shell.act.renaming")),
        {
          label: i18n.t("shell.act.autoTitle"),
          hint: i18n.t("shell.act.autoTitleHint"),
          run: () => act(() => api.rename(session, { auto: true }), i18n.t("shell.act.autoTitling")),
        },
        session.id
      ),
    info: setInfo,
    share: setShare,
    exportMd: (row) => exportMarkdown([{ s: row.s, project: tilde(row.g.project, home) }]).catch(() => toast.error(i18n.t("shell.act.exportFailed"))),
    move: async (rows) => {
      const pickerTitle = rows.length > 1 ? i18n.t("shell.act.moveMany", { sessions: sessionsCount(rows.length) }) : i18n.t("shell.act.moveOne", { title: rows[0].s.title })
      const destination = await pickDir(pickerTitle, rows[0].s.cwd)
      const items = rows.filter((row) => row.s.cwd !== destination).map((row) => ({ dir: row.s.dir, id: row.s.id }))
      if (!destination || !items.length) return
      const moved = await act(() => api.reassign(items, destination), i18n.t("shell.act.moving"))
      if (moved) unpick(items)
    },
    moveGroup: async (folder: Group, items: Item[]) => {
      const destination = await pickDir(i18n.t("shell.act.reassignTo", { sessions: sessionsCount(items.length) }), folder.project)
      if (destination) act(() => api.reassign(items, destination), i18n.t("shell.act.reassigning"))
    },
    group: (ids, project, name) => (name === null ? newGroup(ids, project) : addTo(ids, project, name)),
    ungroup: (ids, group = shownGroup) => {
      if (!group) return
      saveGroups(vgroups.map((candidate) => (isSameGroup(candidate, group) ? { ...candidate, ids: candidate.ids.filter((sessionId) => !ids.includes(sessionId)) } : candidate)))
    },
    pickDir,
    stop: (ids, onDone) =>
      setAsk({
        title: ids.length > 1 ? i18n.t("shell.act.closeMany", { n: ids.length }) : i18n.t("shell.act.closeOne"),
        rows: ids,
        body: i18n.t("shell.act.closeBody"),
        confirm: i18n.t("common.close"),
        run: async () => {
          const results = await Promise.allSettled(ids.map((id) => api.sessionClose(id)))
          const firstFailure = results.find((result): result is PromiseRejectedResult => result.status === "rejected")
          if (firstFailure) toast.error(errMsg(firstFailure.reason))
          else toast.success(ids.length > 1 ? i18n.t("shell.act.closedMany") : i18n.t("shell.act.closedOne"))
          // Le processus met un instant à finir : on retire les sessions tout de suite, puis on relit jusqu'à confirmation.
          const closedIds = new Set(ids.filter((_, index) => results[index].status === "fulfilled"))
          markClosing([...closedIds])
          if (!firstFailure) onDone?.()
          setData(
            (currentData) =>
              currentData && {
                ...currentData,
                live: currentData.live.filter((sessionId) => !closedIds.has(sessionId)),
                waiting: currentData.waiting.filter((sessionId) => !closedIds.has(sessionId)),
              }
          )
          for (let attempt = 0; attempt < CLOSE_RELOAD_ATTEMPTS; attempt++) {
            await wait(CLOSE_RELOAD_DELAY_MS)
            await reload()
          }
        },
      }),
    remove: (items, what, after) => {
      const someAreOpen = items.some((item) => live.has(item.id))
      const closingNotice = someAreOpen ? "\n\n" + i18n.t("shell.act.trashClosing") : ""
      return setAsk({
        title: i18n.t("shell.act.trashTitle"),
        rows: items.map((item) => item.id),
        removes: true,
        body: `${what}${closingNotice}\n\n${i18n.t("shell.act.trashRestorable")}`,
        confirm: i18n.t("shell.act.trashConfirm"),
        run: async () => {
          after?.()
          unpick(items)
          let trashResult: { message: string }
          try {
            trashResult = await api.trashSessionsAdd(items)
          } catch (error) {
            await reload()
            return void toast.error(errMsg(error))
          }
          await reload()
          toast.success(firstLine(trashResult.message), {
            action: {
              label: i18n.t("common.cancel"),
              onClick: async () => {
                const restore = () => api.trashSessionsRestore(items.map((item) => item.id)).then((restored) => ({ message: firstLine(restored.message) }))
                await reportAction(restore)
                await reload()
              },
            },
            duration: TRASH_UNDO_TOAST_DURATION_MS,
          })
        },
      })
    },
  }

  const purgeAuto = () => {
    const autoCount = view.autoItems.length
    setAsk({
      title: i18n.t("shell.act.purgeTitle", { n: autoCount }),
      body: i18n.t("shell.act.purgeBody", { size: size(view.akb) }),
      confirm: i18n.t("shell.act.purgeConfirm", { n: autoCount }),
      run: () => act(() => api.rmSessions(view.autoItems), i18n.t("shell.act.purging", { n: autoCount })),
    })
  }
  return { actions, groupActs, groupRows, targets, newSession, purgeAuto }
}
