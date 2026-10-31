import { useEffect, useRef, useState } from "react"
import { ArrowLeft, Brain, Globe } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { useLocal } from "@/hooks/useLocal"
import { api, type MemFile, type Trashed } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { watchTrust } from "@/lib/trust"
import { tilde } from "@/lib/format"
import { withChoice } from "@/lib/models"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Ask } from "./Confirm"
import type { ComposeAsk } from "./Compose"
import { Markdown } from "./Message"
import { BLANK_ENTRY, blankRule, cmpFiles, copy, DEFAULT_SORT, home, newKey, split, type Sort } from "./memoryLib"
import { downloadPack, packDir, projectLabel } from "@/lib/memoryPack"
import { MemoryImport } from "./MemoryImport"
import { Toolbar, SelectionBar } from "./MemoryBars"
import { FileBar } from "./MemoryFileBar"
import { FileMenu } from "./MemoryFileMenu"
import { Section } from "./MemorySection"
import { HealthDialog } from "./MemoryHealth"
import { ScopeBar } from "./MemoryScope"
import { TypeHelp } from "./MemoryTypes"
import { TrashList, TrashViewer } from "./MemoryTrash"
import { Prompt, type Ask as PromptAsk } from "./Prompt"
import { MemoryWriter, type WriteTarget } from "./MemoryWriter"
import { useMemoryData } from "./useMemoryData"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"
import { reportAction } from "@/lib/report"

const TRASH_PREFIX = "corbeille/"
const MIN_SEARCH_LENGTH = 2
const SEARCH_DEBOUNCE_MS = 250
const TOKEN_COUNT_BATCH_SIZE = 4

// Mémoire de Claude : globale (CLAUDE.md, rules/) et par projet (memory/, CLAUDE.md et AGENTS.md du projet), lecture et édition.
export function Memory({
  sel,
  home: homeDir,
  q,
  onPick,
  onAsk,
  onCompose,
  args,
}: {
  sel: string
  home: string
  q: string
  onPick: (key: string) => void
  onAsk: (ask: Ask) => void
  onCompose: (composeAsk: ComposeAsk) => void
  args: string
}) {
  const { t } = useTranslation()
  const [picked, setPicked] = useState(() => new Set<string>())
  const [sortsRaw, setSortsRaw] = useLocal("ccMemSorts", "{}") // { <dossier>: { by, dir } }
  const sorts: Record<string, Sort> = (() => {
    try {
      return JSON.parse(sortsRaw) ?? {}
    } catch {
      return {}
    }
  })()
  const sortOf = (key: string): Sort => ({ ...DEFAULT_SORT, ...sorts[key] })
  const toggle = (key: string) =>
    setPicked((previousPicked) => {
      const nextPicked = new Set(previousPicked)
      if (nextPicked.has(key)) nextPicked.delete(key)
      else nextPicked.add(key)
      return nextPicked
    })

  const [loaded, setLoaded] = useState<{ key: string; text: string; error?: string } | null>(null)
  const [draft, setDraft] = useEditorDraft("memory")
  const editNext = useRef("") // « Modifier » depuis le clic droit : passe en édition dès le chargement
  // Une erreur de lecture reste à part : elle ne devient jamais le texte du fichier, donc jamais modifiable ni enregistrable.
  const failed = loaded?.key === sel ? loaded.error : undefined
  const text = loaded?.key === sel && failed === undefined ? loaded.text : null
  const editing = draft?.key === sel ? draft : null
  useLeaveGuard(!!draft && !(draft.key === sel && draft.text === text))
  // Corbeille : sel vaut « corbeille/<id> » pour afficher une version.
  const inTrash = sel.startsWith(TRASH_PREFIX) ? sel.slice(TRASH_PREFIX.length) : ""

  // Liste, corbeille et santé, relues en continu ; le fichier ouvert l'est aussi (sauf une version de la corbeille).
  const { list, trash, issues, loads, refresh } = useMemoryData(() => {
    if (sel && !inTrash)
      api.memoryFile(sel).then(
        // l'utilisateur a pu ouvrir un autre fichier pendant la lecture : on ne rafraîchit que celui qui est encore affiché
        (fileResponse) =>
          setLoaded((previousLoaded) => {
            const isOtherFileOrUnchanged = previousLoaded?.key !== sel || previousLoaded.text === fileResponse.text
            return isOtherFileOrUnchanged ? previousLoaded : { key: sel, text: fileResponse.text }
          }),
        () => {}
      )
  })
  useEffect(() => {
    if (!sel) return
    let isStillMounted = true
    const fileRequest: Promise<{ text?: string; error?: string }> = inTrash ? api.trashFile(inTrash) : api.memoryFile(sel)
    fileRequest.then(
      (fileResponse) => {
        if (!isStillMounted) return
        if (fileResponse.text === undefined) setLoaded({ key: sel, text: "", error: fileResponse.error ?? "" })
        else setLoaded({ key: sel, text: fileResponse.text })
        if (editNext.current === sel && fileResponse.text !== undefined) setDraft({ key: sel, text: fileResponse.text })
        editNext.current = ""
      },
      (error) => isStillMounted && setLoaded({ key: sel, text: "", error: errMsg(error) }) // sinon la vue reste sur « chargement »
    )
    return () => {
      isStillMounted = false
    }
  }, [sel, inTrash, setDraft])

  const all = list ? [...list.global, ...list.projects.flatMap((project) => project.files)] : []
  const file = all.find((memFile) => memFile.key === sel)

  // Filtre : nom, ou contenu des fichiers (recherche côté serveur, après une courte pause de frappe), et type.
  const [typeFilter, setTypeFilter] = useState("")
  const [hits, setHits] = useState<{ q: string; keys: Set<string> }>({ q: "", keys: new Set() })
  useEffect(() => {
    const trimmedQuery = q.trim()
    if (trimmedQuery.length < MIN_SEARCH_LENGTH) return
    let isStillMounted = true
    const searchTimer = setTimeout(
      () =>
        api.memorySearch(trimmedQuery).then(
          (searchResponse) => isStillMounted && setHits({ q: trimmedQuery, keys: new Set(Object.keys(searchResponse.hits)) }),
          () => {}
        ),
      SEARCH_DEBOUNCE_MS
    )
    return () => {
      isStillMounted = false
      clearTimeout(searchTimer)
    }
  }, [q])
  const hitKeys = hits.q === q.trim() ? hits.keys : null
  const lowerQuery = q.toLowerCase()
  const match = (memFile: MemFile, scope = "") => {
    const matchesType = !typeFilter || memFile.type === typeFilter
    if (!matchesType) return false
    if (!lowerQuery) return true
    const matchesName = memFile.name.toLowerCase().includes(lowerQuery)
    const matchesScope = scope.toLowerCase().includes(lowerQuery)
    const matchesContent = !!hitKeys?.has(memFile.key)
    return matchesName || matchesScope || matchesContent
  }
  const issueKeys = new Map<string, number>()
  for (const issue of issues) issueKeys.set(issue.key, (issueKeys.get(issue.key) ?? 0) + 1)
  const [healthOpen, setHealthOpen] = useState(false)
  const [typeHelp, setTypeHelp] = useState(false)

  // --- appels au serveur --------------------------------------------------------------------------------------
  const save = async () => {
    if (!editing) return void toast.error(t("memory.main.noDraft"))
    // Nouvelle entrée de mémoire de projet ou de règle globale : le nom saisi fixe le fichier.
    const newEntryMatch = /^(m\/[^/]+|r\/[^/]+|g\/rules)\/[^/]+$/.exec(sel)
    const scopeOf = (parentPath: string) => {
      if (parentPath === "g/rules") return "global"
      if (parentPath[0] === "r") return "r:" + parentPath.slice(2)
      return parentPath.slice(2)
    }
    const key = !file && newEntryMatch ? newKey(all, scopeOf(newEntryMatch[1]), editing.text) : sel
    if (await reportAction(() => api.memorySave(key, editing.text, !file))) {
      setLoaded({ key, text: editing.text })
      setDraft(null)
      if (key !== sel) onPick(key)
      refresh()
    }
  }
  const [writer, setWriter] = useState<WriteTarget | null>(null)
  // Références proposées : la sélection, plus le fichier affiché.
  const writeRefs = writer?.refs
    ? all.filter((memFile) => writer.refs!.includes(memFile.key))
    : all.filter((memFile) => picked.has(memFile.key) || memFile.key === sel)
  // Consolidation dans un CLAUDE.md : les règles globales dans ~/.claude/CLAUDE.md, la mémoire d'un projet dans le sien.
  const consolidate = (scope: string, label: string, target = "CLAUDE.md") => {
    const isGlobal = scope === "global"
    const refs = isGlobal
      ? (list?.global ?? []).filter((memFile) => memFile.key.startsWith("g/rules/"))
      : all.filter((memFile) => memFile.key.startsWith(`m/${scope}/`))
    setWriter({
      kind: "rewrite",
      key: isGlobal ? "g/CLAUDE.md" : `p/${scope}/${target}`,
      name: isGlobal ? "~/.claude/CLAUDE.md" : t("memory.main.consolidateName", { target, label }),
      refs: refs.map((memFile) => memFile.key),
      instruction: isGlobal ? t("memory.main.consolidateGlobal") : t("memory.main.consolidateProject", { target }),
    })
  }
  const onDraft = async (writeTarget: WriteTarget, body: string) => {
    const key = writeTarget.kind === "rewrite" ? writeTarget.key : newKey(all, writeTarget.scope, body)
    if (writeTarget.kind === "rewrite") {
      // Un brouillon identique au fichier n'a rien à enregistrer : on le dit plutôt que d'ouvrir un éditeur sans changement.
      const currentText = await api.memoryFile(key).then((fileResponse) => fileResponse.text, () => undefined)
      const isSameAsFile = currentText !== undefined && currentText.trimEnd() === body.trimEnd()
      if (isSameAsFile) return void toast.info(t("memory.main.haikuSame"))
    }
    setDraft({ key, text: body })
    onPick(key)
  }
  // Entrée écrite à la main : un modèle prérempli, la clé se fixe à l'enregistrement d'après le nom.
  const blank = (scope: string) => {
    const isRuleScope = scope === "global" || scope.startsWith("r:")
    return onDraft({ kind: "new", scope, label: "" }, isRuleScope ? blankRule() : BLANK_ENTRY)
  }
  const edit = (key: string) => {
    if (key === sel && text !== null) return setDraft({ key, text })
    editNext.current = key
    onPick(key)
  }
  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
  const rename = (memFile: MemFile) =>
    setPrompt({
      title: t("memory.fileMenu.renameTitle", { name: memFile.name }),
      value: memFile.name,
      confirm: t("memory.fileMenu.rename"),
      run: async (name) => {
        if (name === memFile.name) return true
        let newFileKey = ""
        const ok = await reportAction(async () => {
          const renameResponse = await api.memoryRename(memFile.key, name)
          newFileKey = renameResponse.key
          return renameResponse
        })
        if (ok) {
          refresh()
          if (memFile.key === sel) onPick(newFileKey)
        }
        return ok
      },
    })
  const move = async (key: string, to: string, copyIt = false) => {
    let newFileKey = ""
    const ok = await reportAction(async () => {
      const moveResponse = await api.memoryMove(key, to, copyIt)
      newFileKey = moveResponse.key
      return moveResponse
    })
    if (ok) {
      refresh()
      if (key === sel && !copyIt) onPick(newFileKey)
    }
  }
  // Règle globale désactivée : fichier renommé en .md.off, Claude ne la charge plus.
  const toggleRule = async (key: string) => {
    let newFileKey = ""
    const ok = await reportAction(async () => {
      const toggleResponse = await api.memoryToggle(key, key.endsWith(".off"))
      newFileKey = toggleResponse.key
      return toggleResponse
    })
    if (ok) {
      refresh()
      if (key === sel) onPick(newFileKey)
    }
  }
  const setType = async (key: string, type: string) => {
    if (await reportAction(() => api.memoryType(key, type))) {
      await refresh()
      if (key === sel) setLoaded({ key, text: await api.memoryFile(key).then((fileResponse) => fileResponse.text, () => "") })
    }
  }
  const fix = async (keys: string[]) => {
    let fixedCount = 0
    const failures: string[] = []
    for (const key of keys) {
      try {
        await api.memoryFix(key)
        fixedCount++
      } catch (error) {
        failures.push(`${key} : ${errMsg(error)}`)
      }
    }
    if (failures.length)
      toast.error(t("memory.main.fixPartial", { done: t("memory.main.fixedCount", { count: fixedCount }), failed: t("memory.main.failCount", { count: failures.length }), first: failures[0] }))
    else toast.success(t("memory.main.fixOk", { count: fixedCount }))
    refresh()
  }
  const repair = async (dir: string) => {
    await reportAction(() => api.memoryRepair(dir))
    refresh()
  }
  const remove = (memFile: MemFile) =>
    onAsk({
      title: t("memory.main.deleteTitle", { name: memFile.name }),
      body: memFile.key.startsWith("m/") ? t("memory.main.deleteBodyMemory") : t("memory.main.deleteBodyFile"),
      confirm: t("common.delete"),
      run: async () => {
        if (await reportAction(() => api.memoryRemove([memFile.key]))) {
          if (memFile.key === sel) onPick("")
          refresh()
        }
      },
    })
  // Suppression d'un groupe : chaque fichier va à la corbeille (restaurable), un à un, avec un bilan.
  const removeMany = (keys: string[], what: string) =>
    onAsk({
      title: t("memory.main.removeManyTitle", { what, count: keys.length }),
      body: t("memory.main.removeManyBody"),
      confirm: t("memory.main.removeAll"),
      run: async () => {
        let removedCount = 0
        const failures: string[] = []
        for (const key of keys) {
          try {
            await api.memoryRemove([key])
            removedCount++
          } catch (error) {
            failures.push(errMsg(error))
          }
        }
        if (failures.length)
          toast.error(t("memory.main.fixPartial", { done: t("memory.main.removedCount", { count: removedCount }), failed: t("memory.main.failCount", { count: failures.length }), first: failures[0] }))
        else toast.success(t("memory.main.trashedFiles", { count: removedCount }))
        setPicked(new Set())
        if (keys.includes(sel)) onPick("")
        refresh()
      },
    })
  const copyContent = async (memFile: MemFile) => {
    try {
      copy((await api.memoryFile(memFile.key)).text, "memory.lib.copiedContent")
    } catch (error) {
      toast.error(errMsg(error))
    }
  }
  // Fusion par Claude : le serveur rédige le prompt ; on le copie, ou on l'ouvre dans la fenêtre de saisie (relecture, modèle) avant de lancer la session.
  // Export : les fichiers cochés (mémoire, règles, CLAUDE.md), hors index, en zip ; un avertissement précède le téléchargement.
  const [importing, setImporting] = useState(false)
  const exportKeys = (keys: string[]) => {
    const files = all.filter((memFile) => keys.includes(memFile.key) && memFile.name !== "MEMORY.md" && /\.md$/.test(memFile.name))
    if (!files.length) return void toast.info(t("memory.pack.none"))
    const projectNameOf = (fileKey: string) => list?.projects.find((project) => project.dir === home(fileKey))?.project ?? ""
    onAsk({
      title: t("memory.pack.exportTitle", { count: files.length }),
      body: t("memory.pack.warn"),
      confirm: t("memory.pack.export"),
      run: async () => {
        try {
          const entries = await Promise.all(
            files.map(async (memFile) => ({
              project: packDir(memFile.key, projectLabel(projectNameOf(memFile.key))),
              name: memFile.name.replace(/^rules\//, ""),
              text: (await api.memoryFile(memFile.key)).text,
            }))
          )
          downloadPack(entries)
          setPicked(new Set())
        } catch (error) {
          toast.error(errMsg(error).trim() || t("memory.main.failed"))
        }
      },
    })
  }
  const merge = async (launch: boolean) => {
    const keys = [...picked]
    const draftMergePrompt = (out?: string) => api.memoryMerge({ keys, out, launch: false })
    let mergeDraft
    try {
      mergeDraft = await draftMergePrompt()
    } catch (error) {
      return void toast.error(errMsg(error).trim() || t("memory.main.failed"))
    }
    if (!launch) return void copy(mergeDraft.prompt, "memory.lib.copiedMergePrompt")
    const projectDirs = [...new Set(keys.map((key) => /^[mpr]\/([^/]+)\//.exec(key)?.[1]).filter((projectDir): projectDir is string => !!projectDir))]
    const projectLabelOf = (projectDir: string) => tilde(list?.projects.find((project) => project.dir === projectDir)?.project ?? projectDir, homeDir)
    const outputs = [
      ...keys.map((key) => ({ key, label: all.find((memFile) => memFile.key === key)?.name ?? key })),
      { key: "g/CLAUDE.md", label: "~/.claude/CLAUDE.md" },
      ...projectDirs.flatMap((projectDir) =>
        ["CLAUDE.md", "AGENTS.md"].map((fileName) => ({ key: `p/${projectDir}/${fileName}`, label: `${projectLabelOf(projectDir)}/${fileName}` }))
      ),
    ]
    const uniqueOutputs = outputs.filter(
      (output, outputIndex) => outputs.findIndex((otherOutput) => otherOutput.key === output.key) === outputIndex
    )
    onCompose({
      id: "merge:" + keys.join(","),
      title: t("memory.main.mergeTitle"),
      subtitle: t("memory.main.mergeSubtitle", { n: keys.length }),
      project: "",
      initial: mergeDraft.prompt,
      send: t("memory.main.mergeSend"),
      choice: { model: "haiku", effort: "" }, // tâche mécanique
      outputs: uniqueOutputs,
      reprompt: async (out) => {
        try {
          return (await draftMergePrompt(out)).prompt
        } catch (error) {
          return void toast.error(errMsg(error).trim() || t("memory.main.failed"))
        }
      },
      run: async (text, modelChoice, _unused, out) => {
        try {
          const mergeResponse = await api.memoryMerge({ keys, out, launch: true, prompt: text, args: withChoice(args, modelChoice) })
          toast.success(mergeResponse.message.trim() || t("memory.main.mergeStarted"))
          void watchTrust(homeDir)
          setPicked(new Set())
          return true
        } catch (error) {
          toast.error(errMsg(error).trim() || t("memory.main.failed"))
          return false
        }
      },
    })
  }
  // Décompte réel des tokens par Haiku (tokens.go), quelques fichiers à la fois ; la liste se rafraîchit à chaque lot.
  const countTokens = async (keys: string[]) => {
    if (!keys.length) return void toast.info(t("memory.main.alreadyCounted"))
    const toastId = toast.loading(t("memory.main.counting", { count: keys.length }))
    let cost = 0
    let firstFailure = ""
    for (let batchStart = 0; batchStart < keys.length; batchStart += TOKEN_COUNT_BATCH_SIZE) {
      try {
        const tokensResponse = await api.memoryTokens(keys.slice(batchStart, batchStart + TOKEN_COUNT_BATCH_SIZE))
        cost += tokensResponse.cost
        firstFailure ||= Object.values(tokensResponse.errors)[0] ?? ""
      } catch (error) {
        firstFailure ||= errMsg(error)
      }
      await refresh()
    }
    if (firstFailure) toast.error(t("memory.main.countFailed", { error: firstFailure }), { id: toastId })
    else toast.success(t("memory.main.counted", { cost: cost.toFixed(4) }), { id: toastId })
  }
  const haikuFromSelection = () => {
    const projectDir = [...picked].find((key) => key.startsWith("m/"))?.split("/")[1]
    if (!projectDir) {
      setWriter({ kind: "new", scope: "global", label: t("memory.main.globalRules") })
      return
    }
    const project = list?.projects.find((candidate) => candidate.dir === projectDir)
    setWriter({ kind: "new", scope: projectDir, label: tilde(project?.project ?? projectDir, homeDir) })
  }

  // --- corbeille ----------------------------------------------------------------------------------------------
  const trashed = trash.find((trashItem) => trashItem.id === inTrash)
  const restore = async (trashItem: Trashed) => {
    let key = ""
    const ok = await reportAction(async () => {
      const restoreResponse = await api.trashRestore(trashItem.id)
      key = restoreResponse.key
      return restoreResponse
    })
    if (ok) {
      await refresh()
      if (key) onPick(key)
    }
  }
  const purge = (item?: Trashed) =>
    onAsk({
      title: item ? t("memory.main.purgeOne") : t("memory.main.purgeAll", { n: trash.length }),
      body: t("memory.main.purgeBody"),
      confirm: item ? t("common.delete") : t("memory.trash.empty"),
      run: async () => {
        if (await reportAction(() => api.trashPurge(item?.id))) {
          if (!item || item.id === inTrash) onPick("")
          refresh()
        }
      },
    })

  // --- liste de gauche ----------------------------------------------------------------------------------------
  // Projets dont le dossier a disparu : leur mémoire n'est plus utilisée par personne.
  const gone = list?.projects.filter((project) => project.missing) ?? []
  const orphans = gone.flatMap((project) => project.files.map((memFile) => memFile.key))
  const targets = (key: string) => (list?.targets ?? []).filter((target) => target.dir !== home(key))
  const menu = (memFile: MemFile) => (
    <FileMenu
      x={memFile}
      picked={picked.has(memFile.key)}
      targets={targets(memFile.key)}
      homeDir={homeDir}
      a={{
        open: onPick,
        edit,
        rewrite: (fileToRewrite) => setWriter({ kind: "rewrite", key: fileToRewrite.key, name: fileToRewrite.name }),
        rename,
        copyContent,
        toggle,
        move,
        toggleRule,
        setType,
        remove,
        exportOne: (fileToExport) => exportKeys([fileToExport.key]),
      }}
    />
  )
  const section = (key: string, icon: typeof Globe, title: string, files: MemFile[], onWrite?: () => void, onConsolidate?: (file: string) => void, onNewRule?: () => void) => {
    const alwaysLoadedFiles = files.filter((memFile) => memFile.always)
    return (
      <Section
        key={key}
        icon={icon}
        title={title}
        files={[...files].sort(cmpFiles(sortOf(key).by, sortOf(key).dir))}
        sel={sel}
        onPick={onPick}
        picked={picked}
        onToggle={toggle}
        onPickAll={(keys) => setPicked((previousPicked) => new Set([...previousPicked, ...keys]))}
        onNew={() => blank(key)}
        onNewRule={onNewRule}
        onWrite={onWrite}
        onConsolidate={onConsolidate}
        agents={key !== "global"}
        onRemoveAll={(keys) => removeMany(keys, t("memory.main.sectionOf", { title }))}
        menu={menu}
        sort={sortOf(key)}
        onSort={(sort) => setSortsRaw(JSON.stringify({ ...sorts, [key]: sort }))}
        budget={alwaysLoadedFiles.reduce((tokenTotal, memFile) => tokenTotal + memFile.tokens, 0)}
        exact={alwaysLoadedFiles.every((memFile) => memFile.exact)}
        onCount={() => countTokens(alwaysLoadedFiles.filter((memFile) => !memFile.exact).map((memFile) => memFile.key))}
        issues={issueKeys}
      />
    )
  }

  const [frontMatter, body] = split(text ?? "")
  const globalLabel = t("memory.main.global")
  const gldRulesLabel = t("memory.main.gldRules")
  const isGlobalFile = (memFile: MemFile) => !memFile.gld && match(memFile, globalLabel)
  const isGldRuleFile = (memFile: MemFile) => memFile.gld && match(memFile, gldRulesLabel)
  const hasNoMatch =
    !!list &&
    !list.global.some((memFile) => match(memFile, globalLabel)) &&
    !list.projects.some((project) => project.files.some((memFile) => match(memFile, project.project)))
  const renderProjectSection = (project: NonNullable<typeof list>["projects"][number]) => {
    const files = project.files.filter((memFile) => match(memFile, project.project))
    if (!files.length) return null
    const label = tilde(project.project, homeDir)
    // un CLAUDE.md de projet s'écrit dans son dossier : impossible s'il a disparu, inutile sans mémoire
    const canConsolidate = !project.missing && project.files.some((memFile) => memFile.key.startsWith("m/"))
    return section(
      project.dir,
      Brain,
      project.missing ? t("memory.main.projectMissing", { label }) : label,
      files,
      () => setWriter({ kind: "new", scope: project.dir, label }),
      canConsolidate ? (target) => consolidate(project.dir, label, target) : undefined,
      !project.missing ? () => blank("r:" + project.dir) : undefined
    )
  }
  const renderReader = () => {
    if (failed !== undefined) return <p className="text-destructive">{failed || t("memory.main.unreadable")}</p>
    if (text === null) return <Skeleton className="h-32 w-full" />
    return (
      <>
        {frontMatter && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{frontMatter}</pre>}
        <Markdown t={body} onLink={openSiblingMarkdown} />
      </>
    )
  }
  const openSiblingMarkdown = (href: string) => {
    if (!/^[^/:?#]+\.md$/.test(href)) return false
    onPick(sel.slice(0, sel.lastIndexOf("/") + 1) + href)
    return true
  }
  const saveScope = async () => {
    await refresh()
    setLoaded({ key: sel, text: (await api.memoryFile(sel)).text ?? "" })
  }
  const memoryScopes = (list?.projects ?? [])
    .filter((project) => project.files.some((memFile) => memFile.key.startsWith("m/")))
    .map((project) => [project.dir, tilde(project.project, homeDir)] as const)
  return (
    <div className="relative flex min-h-0 flex-1 flex-col lg:flex-row">
      <ScrollArea className={cn("shrink-0 border-b bg-muted/30 lg:block lg:max-h-none lg:w-[28rem] lg:flex-none lg:border-r lg:border-b-0", sel ? "hidden" : "min-h-0 flex-1")}>
        <nav className="p-2">
          {!list && <Skeleton className="m-2 h-24" />}
          {list && (
            <Toolbar
              issues={issues.length}
              all={all}
              typeFilter={typeFilter}
              orphanProjects={gone.length}
              onHealth={() => setHealthOpen(true)}
              onType={setTypeFilter}
              onTypeHelp={() => setTypeHelp(true)}
              onCleanOrphans={() => removeMany(orphans, t("memory.main.orphansOf", { count: gone.length }))}
              onExportAll={() => exportKeys(all.filter((memFile) => memFile.key.startsWith("m/")).map((memFile) => memFile.key))}
              onImport={() => setImporting(true)}
            />
          )}
          {list &&
            list.global.some(isGlobalFile) &&
            section(
              "global",
              Globe,
              globalLabel,
              list.global.filter(isGlobalFile),
              () => setWriter({ kind: "new", scope: "global", label: t("memory.main.globalRules") }),
              () => consolidate("global", "")
            )}
          {list && list.global.some(isGldRuleFile) && section("gld", Globe, gldRulesLabel, list.global.filter(isGldRuleFile))}
          {list?.projects.map(renderProjectSection)}
          {hasNoMatch && (
            <p className="px-2 py-6 text-center text-sm text-muted-foreground">{all.length ? t("memory.main.noMatch") : t("memory.main.empty")}</p>
          )}
          {trash.length > 0 && <TrashList trash={trash} current={inTrash} onPick={onPick} onPurgeAll={() => purge()} />}
        </nav>
      </ScrollArea>
      {picked.size > 0 && (
        <SelectionBar
          count={picked.size}
          onPrompt={() => merge(false)}
          onHaiku={haikuFromSelection}
          onRemove={() => removeMany([...picked], t("memory.main.selection"))}
          onMerge={() => merge(true)}
          onExport={() => exportKeys([...picked])}
          onClear={() => setPicked(new Set())}
        />
      )}
      <div className={cn("min-h-0 min-w-0 flex-1 flex-col lg:flex", sel ? "flex" : "hidden")}>
        {sel && (
          <div className="border-b px-2 py-1.5 lg:hidden">
            <Button variant="ghost" size="sm" onClick={() => onPick("")}>
              <ArrowLeft /> {t("memory.main.list")}
            </Button>
          </div>
        )}

        {!!inTrash && (
          <TrashViewer
            trashed={trashed}
            text={text}
            homeDir={homeDir}
            exists={!!trashed && all.some((memFile) => memFile.key === trashed.key)}
            onPurge={() => trashed && purge(trashed)}
            onRestore={() => trashed && restore(trashed)}
          />
        )}
        {!inTrash && !sel && <p className="px-6 py-20 text-center text-sm text-muted-foreground">{t("memory.main.pickFile")}</p>}
        {!inTrash && !!sel && (
          <>
            <FileBar
              sel={sel}
              file={file}
              editing={!!editing}
              dirty={!!editing && editing.text !== text}
              textReady={text !== null}
              targets={targets(sel)}
              homeDir={homeDir}
              onCancel={() => setDraft(null)}
              onSave={save}
              onEdit={() => edit(sel)}
              onMove={(to) => move(sel, to)}
              onType={(type) => file && setType(file.key, type)}
              onToggleRule={() => file && toggleRule(file.key)}
              onRemove={() => file && remove(file)}
              loads={loads.hook ? (loads.files[file?.path ?? ""] ?? { sessions: 0, last: 0 }) : undefined}
              onRewrite={() => file && setWriter({ kind: "rewrite", key: file.key, name: file.name })}
            />
            {file && !editing && sel.startsWith("g/rules/") && !file.off && list && (
              <ScopeBar
                key={file.key + (file.paths ?? []).join("|")}
                file={file}
                projects={list.projects}
                home={homeDir}
                onSaved={saveScope}
              />
            )}
            {file?.off && sel.startsWith("g/rules/") && (
              <p className="border-b bg-muted/30 px-6 py-2 text-xs text-muted-foreground">
                {t("memory.main.ruleOff")}{" "}
                <button className="text-primary underline underline-offset-2 hover:decoration-2" onClick={() => toggleRule(file.key)}>
                  {t("memory.main.reactivate")}
                </button>
              </p>
            )}
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(changeEvent) => setDraft({ key: sel, text: changeEvent.target.value })}
                onKeyDown={(keyEvent) => {
                  const isSaveShortcut = (keyEvent.ctrlKey || keyEvent.metaKey) && keyEvent.key === "s"
                  if (!isSaveShortcut) return
                  keyEvent.preventDefault()
                  save()
                }}
                spellCheck={false}
                autoFocus
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
                aria-label={t("memory.main.fileContent")}
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-page px-6 py-6 text-sm leading-relaxed">
                  {renderReader()}
                </div>
              </ScrollArea>
            )}
          </>
        )}
      </div>
      <TypeHelp open={typeHelp} onClose={() => setTypeHelp(false)} />
      <HealthDialog
        open={healthOpen}
        issues={issues}
        onClose={() => setHealthOpen(false)}
        onOpenKey={onPick}
        onRepair={repair}
        onFix={fix}
        scopes={[["global", t("memory.main.scopeGlobal")], ...memoryScopes]}
        onLint={async (scope) => {
          try {
            return await api.memoryLint(scope)
          } catch (error) {
            return { error: errMsg(error) }
          }
        }}
      />
      {list && <MemoryImport open={importing} list={list} homeDir={homeDir} onClose={() => setImporting(false)} onDone={refresh} />}
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
      <MemoryWriter target={writer} refs={writeRefs} onClose={() => setWriter(null)} onDraft={onDraft} />
    </div>
  )
}
