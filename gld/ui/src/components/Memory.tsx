import { useEffect, useRef, useState } from "react"
import { ArrowLeft, Brain, Globe } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { useLocal } from "@/hooks/useLocal"
import { api, type Finding, type MemFile, type Trashed } from "@/lib/api"
import { tilde } from "@/lib/format"
import { withChoice } from "@/lib/models"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Ask } from "./Confirm"
import type { ComposeAsk } from "./Compose"
import { Markdown } from "./Message"
import { BLANK_ENTRY, blankRule, cmpFiles, copy, DEFAULT_SORT, home, newKey, split, type Sort } from "./memoryLib"
import { Toolbar, SelectionBar } from "./MemoryBars"
import { FileBar } from "./MemoryFileBar"
import { FileMenu } from "./MemoryFileMenu"
import { Section } from "./MemorySection"
import { HealthDialog } from "./MemoryHealth"
import { ScopeBar } from "./MemoryScope"
import { TypeHelp } from "./MemoryTypes"
import { TrashList, TrashViewer } from "./MemoryTrash"
import { MemoryWriter, type WriteTarget } from "./MemoryWriter"
import { useMemoryData } from "./useMemoryData"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"
import { reportResult } from "@/lib/report"

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
  onPick: (k: string) => void
  onAsk: (a: Ask) => void
  onCompose: (a: ComposeAsk) => void
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
  const toggle = (k: string) =>
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
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
  const inTrash = sel.startsWith("corbeille/") ? sel.slice(10) : ""

  // Liste, corbeille et santé, relues en continu ; le fichier ouvert l'est aussi (sauf une version de la corbeille).
  const { list, trash, issues, loads, refresh } = useMemoryData(() => {
    if (sel && !inTrash)
      api.memoryFile(sel).then(
        (r) => r.text !== undefined && setLoaded((l) => (l?.key === sel && l.text === r.text ? l : { key: sel, text: r.text! })),
        () => {}
      )
  })
  useEffect(() => {
    if (!sel) return
    let live = true
    const get = inTrash ? api.trashFile(inTrash) : api.memoryFile(sel)
    get.then((r) => {
      if (!live) return
      setLoaded(r.text === undefined ? { key: sel, text: "", error: r.error ?? "" } : { key: sel, text: r.text })
      if (editNext.current === sel && r.text !== undefined) setDraft({ key: sel, text: r.text })
      editNext.current = ""
    })
    return () => {
      live = false
    }
  }, [sel, inTrash, setDraft])

  const all = list ? [...list.global, ...list.projects.flatMap((p) => p.files)] : []
  const file = all.find((f) => f.key === sel)

  // Filtre : nom, ou contenu des fichiers (recherche côté serveur, après une courte pause de frappe), et type.
  const [typeFilter, setTypeFilter] = useState("")
  const [hits, setHits] = useState<{ q: string; keys: Set<string> }>({ q: "", keys: new Set() })
  useEffect(() => {
    const t = q.trim()
    if (t.length < 2) return
    let live = true
    const h = setTimeout(() => api.memorySearch(t).then((r) => live && setHits({ q: t, keys: new Set(Object.keys(r.hits)) })), 250)
    return () => {
      live = false
      clearTimeout(h)
    }
  }, [q])
  const hitKeys = hits.q === q.trim() ? hits.keys : null
  const f = q.toLowerCase()
  const match = (x: MemFile, scope = "") =>
    (!typeFilter || x.type === typeFilter) && (!f || x.name.toLowerCase().includes(f) || scope.toLowerCase().includes(f) || !!hitKeys?.has(x.key))
  const issueKeys = new Map<string, number>()
  for (const i of issues) issueKeys.set(i.key, (issueKeys.get(i.key) ?? 0) + 1)
  const [healthOpen, setHealthOpen] = useState(false)
  const [typeHelp, setTypeHelp] = useState(false)

  // --- appels au serveur --------------------------------------------------------------------------------------
  const save = async () => {
    if (!editing) return void toast.error(t("memory.main.noDraft"))
    // Nouvelle entrée de mémoire de projet ou de règle globale : le nom saisi fixe le fichier.
    const m = /^(m\/[^/]+|r\/[^/]+|g\/rules)\/[^/]+$/.exec(sel)
    const scopeOf = (p: string) => (p === "g/rules" ? "global" : p[0] === "r" ? "r:" + p.slice(2) : p.slice(2))
    const key = !file && m ? newKey(all, scopeOf(m[1]), editing.text) : sel
    if (reportResult(await api.post("/memory-save", { key, text: editing.text, index: !file }))) {
      setLoaded({ key, text: editing.text })
      setDraft(null)
      if (key !== sel) onPick(key)
      refresh()
    }
  }
  const [writer, setWriter] = useState<WriteTarget | null>(null)
  // Références proposées : la sélection, plus le fichier affiché.
  const writeRefs = writer?.refs ? all.filter((x) => writer.refs!.includes(x.key)) : all.filter((x) => picked.has(x.key) || x.key === sel)
  // Consolidation dans un CLAUDE.md : les règles globales dans ~/.claude/CLAUDE.md, la mémoire d'un projet dans le sien.
  const consolidate = (scope: string, label: string, target = "CLAUDE.md") => {
    const refs = scope === "global" ? (list?.global ?? []).filter((x) => x.key.startsWith("g/rules/")) : all.filter((x) => x.key.startsWith(`m/${scope}/`))
    setWriter({
      kind: "rewrite",
      key: scope === "global" ? "g/CLAUDE.md" : `p/${scope}/${target}`,
      name: scope === "global" ? "~/.claude/CLAUDE.md" : t("memory.main.consolidateName", { target, label }),
      refs: refs.map((x) => x.key),
      instruction: scope === "global" ? t("memory.main.consolidateGlobal") : t("memory.main.consolidateProject", { target }),
    })
  }
  const onDraft = async (tg: WriteTarget, body: string) => {
    const key = tg.kind === "rewrite" ? tg.key : newKey(all, tg.scope, body)
    if (tg.kind === "rewrite") {
      // Un brouillon identique au fichier n'a rien à enregistrer : on le dit plutôt que d'ouvrir un éditeur sans changement.
      const cur = (await api.memoryFile(key)).text
      if (cur !== undefined && cur.trimEnd() === body.trimEnd()) return void toast.info(t("memory.main.haikuSame"))
    }
    setDraft({ key, text: body })
    onPick(key)
  }
  // Entrée écrite à la main : un modèle prérempli, la clé se fixe à l'enregistrement d'après le nom.
  const blank = (scope: string) => onDraft({ kind: "new", scope, label: "" }, scope === "global" || scope.startsWith("r:") ? blankRule() : BLANK_ENTRY)
  const edit = (key: string) => {
    if (key === sel && text !== null) return setDraft({ key, text })
    editNext.current = key
    onPick(key)
  }
  const move = async (key: string, to: string, copyIt = false) => {
    const r = (await api.post("/memory-move", { key, to, copy: copyIt })) as { ok: boolean; out: string; key?: string }
    if (reportResult(r) && r.key) {
      refresh()
      if (key === sel && !copyIt) onPick(r.key)
    }
  }
  // Règle globale désactivée : fichier renommé en .md.off, Claude ne la charge plus.
  const toggleRule = async (key: string) => {
    const r = (await api.post("/memory-toggle", { key })) as { ok: boolean; out: string; key?: string }
    if (reportResult(r) && r.key) {
      refresh()
      if (key === sel) onPick(r.key)
    }
  }
  const setType = async (key: string, type: string) => {
    if (reportResult(await api.post("/memory-type", { key, type }))) {
      await refresh()
      if (key === sel) setLoaded({ key, text: (await api.memoryFile(key)).text ?? "" })
    }
  }
  const fix = async (keys: string[]) => {
    let ok = 0
    const fails: string[] = []
    for (const key of keys) {
      const r = (await api.post("/memory-fix", { key })) as { ok: boolean; out: string }
      if (r.ok) ok++
      else fails.push(`${key} : ${r.out}`)
    }
    if (fails.length)
      toast.error(t("memory.main.fixPartial", { done: t("memory.main.fixedCount", { count: ok }), failed: t("memory.main.failCount", { count: fails.length }), first: fails[0] }))
    else toast.success(t("memory.main.fixOk", { count: ok }))
    refresh()
  }
  const repair = async (dir: string) => {
    reportResult(await api.post("/memory-repair", { dir }))
    refresh()
  }
  const remove = (x: MemFile) =>
    onAsk({
      title: t("memory.main.deleteTitle", { name: x.name }),
      body: x.key.startsWith("m/") ? t("memory.main.deleteBodyMemory") : t("memory.main.deleteBodyFile"),
      confirm: t("common.delete"),
      run: async () => {
        if (reportResult(await api.post("/memory-rm", { key: x.key }))) {
          if (x.key === sel) onPick("")
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
        let ok = 0
        const fails: string[] = []
        for (const key of keys) {
          const r = await api.post("/memory-rm", { key })
          if (r.ok) ok++
          else fails.push(r.out)
        }
        if (fails.length)
          toast.error(t("memory.main.fixPartial", { done: t("memory.main.removedCount", { count: ok }), failed: t("memory.main.failCount", { count: fails.length }), first: fails[0] }))
        else toast.success(t("memory.main.trashedFiles", { count: ok }))
        setPicked(new Set())
        if (keys.includes(sel)) onPick("")
        refresh()
      },
    })
  const copyContent = async (x: MemFile) => {
    const r = await api.memoryFile(x.key)
    if (r.text === undefined) return void toast.error(r.error ?? t("memory.main.unreadable"))
    copy(r.text, "memory.lib.copiedContent")
  }
  // Fusion par Claude : le serveur rédige le prompt ; on le copie, ou on l'ouvre dans la fenêtre de saisie (relecture, modèle) avant de lancer la session.
  const merge = async (launch: boolean) => {
    const keys = [...picked]
    const draft = async (out?: string) => (await api.post("/memory-merge", { keys, out, launch: false })) as { ok: boolean; out: string; prompt?: string }
    const r = await draft()
    if (!r.ok || !r.prompt) return void toast.error(r.out.trim() || t("memory.main.failed"))
    if (!launch) return void copy(r.prompt, "memory.lib.copiedMergePrompt")
    onCompose({
      id: "merge:" + keys.join(","),
      title: t("memory.main.mergeTitle"),
      subtitle: t("memory.main.mergeSubtitle", { n: keys.length }),
      project: "",
      initial: r.prompt,
      send: t("memory.main.mergeSend"),
      choice: { model: "haiku", effort: "" }, // tâche mécanique
      outputs: [
        ...keys.map((k) => ({ key: k, label: all.find((x) => x.key === k)?.name ?? k })),
        { key: "g/CLAUDE.md", label: "~/.claude/CLAUDE.md" },
        ...[...new Set(keys.map((k) => /^[mpr]\/([^/]+)\//.exec(k)?.[1]).filter((d): d is string => !!d))].flatMap((d) =>
          ["CLAUDE.md", "AGENTS.md"].map((f) => ({ key: `p/${d}/${f}`, label: `${tilde(list?.projects.find((p) => p.dir === d)?.project ?? d, homeDir)}/${f}` }))
        ),
      ].filter((o, i, a) => a.findIndex((x) => x.key === o.key) === i),
      reprompt: async (out) => {
        const d = await draft(out)
        if (!d.ok || !d.prompt) return void toast.error(d.out.trim() || t("memory.main.failed"))
        return d.prompt
      },
      run: async (text, c, _t, out) => {
        const m = (await api.post("/memory-merge", { keys, out, launch: true, prompt: text, args: withChoice(args, c) })) as { ok: boolean; out: string }
        ;(m.ok ? toast.success : toast.error)(m.out.trim() || (m.ok ? t("memory.main.mergeStarted") : t("memory.main.failed")))
        if (m.ok) setPicked(new Set())
        return m.ok
      },
    })
  }
  // Décompte réel des tokens par Haiku (tokens.go), quelques fichiers à la fois ; la liste se rafraîchit à chaque lot.
  const countTokens = async (keys: string[]) => {
    if (!keys.length) return void toast.info(t("memory.main.alreadyCounted"))
    const id = toast.loading(t("memory.main.counting", { count: keys.length }))
    let cost = 0
    let failed = ""
    for (let i = 0; i < keys.length; i += 4) {
      const r = (await api.post("/memory-tokens", { keys: keys.slice(i, i + 4) })) as { out?: string; cost?: number; errors?: Record<string, string> }
      cost += r.cost ?? 0
      failed ||= Object.values(r.errors ?? {})[0] ?? r.out ?? ""
      await refresh()
    }
    ;(failed ? toast.error : toast.success)(failed ? t("memory.main.countFailed", { error: failed }) : t("memory.main.counted", { cost: cost.toFixed(4) }), { id })
  }
  const haikuFromSelection = () => {
    const dir = [...picked].find((k) => k.startsWith("m/"))?.split("/")[1]
    const p = list?.projects.find((x) => x.dir === dir)
    setWriter(dir ? { kind: "new", scope: dir, label: tilde(p?.project ?? dir, homeDir) } : { kind: "new", scope: "global", label: t("memory.main.globalRules") })
  }

  // --- corbeille ----------------------------------------------------------------------------------------------
  const trashed = trash.find((t) => t.id === inTrash)
  const restore = async (t: Trashed) => {
    const r = (await api.post("/trash-restore", { id: t.id })) as { ok: boolean; out: string; key?: string }
    if (reportResult(r)) {
      await refresh()
      if (r.key) onPick(r.key)
    }
  }
  const purge = (item?: Trashed) =>
    onAsk({
      title: item ? t("memory.main.purgeOne") : t("memory.main.purgeAll", { n: trash.length }),
      body: t("memory.main.purgeBody"),
      confirm: item ? t("common.delete") : t("memory.trash.empty"),
      run: async () => {
        if (reportResult(await api.post("/trash-purge", item ? { id: item.id } : { all: true }))) {
          if (!item || item.id === inTrash) onPick("")
          refresh()
        }
      },
    })

  // --- liste de gauche ----------------------------------------------------------------------------------------
  // Projets dont le dossier a disparu : leur mémoire n'est plus utilisée par personne.
  const gone = list?.projects.filter((p) => p.missing) ?? []
  const orphans = gone.flatMap((p) => p.files.map((x) => x.key))
  const targets = (key: string) => (list?.targets ?? []).filter((t) => t.dir !== home(key))
  const menu = (x: MemFile) => (
    <FileMenu
      x={x}
      picked={picked.has(x.key)}
      targets={targets(x.key)}
      homeDir={homeDir}
      a={{ open: onPick, edit, rewrite: (y) => setWriter({ kind: "rewrite", key: y.key, name: y.name }), copyContent, toggle, move, toggleRule, setType, remove }}
    />
  )
  const section = (key: string, icon: typeof Globe, title: string, files: MemFile[], onWrite?: () => void, onConsolidate?: (file: string) => void, onNewRule?: () => void) => (
    <Section
      key={key}
      icon={icon}
      title={title}
      files={[...files].sort(cmpFiles(sortOf(key).by, sortOf(key).dir))}
      sel={sel}
      onPick={onPick}
      picked={picked}
      onToggle={toggle}
      onPickAll={(keys) => setPicked((p) => new Set([...p, ...keys]))}
      onNew={() => blank(key)}
      onNewRule={onNewRule}
      onWrite={onWrite}
      onConsolidate={onConsolidate}
      agents={key !== "global"}
      onRemoveAll={(keys) => removeMany(keys, t("memory.main.sectionOf", { title }))}
      menu={menu}
      sort={sortOf(key)}
      onSort={(s) => setSortsRaw(JSON.stringify({ ...sorts, [key]: s }))}
      budget={files.filter((x) => x.always).reduce((n, x) => n + x.tokens, 0)}
      exact={files.filter((x) => x.always).every((x) => x.exact)}
      onCount={() => countTokens(files.filter((x) => x.always && !x.exact).map((x) => x.key))}
      issues={issueKeys}
    />
  )

  const [fm, body] = split(text ?? "")
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
            />
          )}
          {list &&
            list.global.some((x) => match(x, t("memory.main.global"))) &&
            section(
              "global",
              Globe,
              t("memory.main.global"),
              list.global.filter((x) => match(x, t("memory.main.global"))),
              () => setWriter({ kind: "new", scope: "global", label: t("memory.main.globalRules") }),
              () => consolidate("global", "")
            )}
          {list?.projects.map((p) => {
            const files = p.files.filter((x) => match(x, p.project))
            const label = tilde(p.project, homeDir)
            return files.length
              ? section(
                  p.dir,
                  Brain,
                  p.missing ? t("memory.main.projectMissing", { label }) : label,
                  files,
                  () => setWriter({ kind: "new", scope: p.dir, label }),
                  // un CLAUDE.md de projet s'écrit dans son dossier : impossible s'il a disparu, inutile sans mémoire
                  !p.missing && p.files.some((x) => x.key.startsWith("m/")) ? (target) => consolidate(p.dir, label, target) : undefined,
                  !p.missing ? () => blank("r:" + p.dir) : undefined
                )
              : null
          })}
          {list && !list.global.some((x) => match(x, t("memory.main.global"))) && !list.projects.some((p) => p.files.some((x) => match(x, p.project))) && (
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

        {inTrash ? (
          <TrashViewer
            trashed={trashed}
            text={text}
            homeDir={homeDir}
            exists={!!trashed && all.some((x) => x.key === trashed.key)}
            onPurge={() => trashed && purge(trashed)}
            onRestore={() => trashed && restore(trashed)}
          />
        ) : !sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">{t("memory.main.pickFile")}</p>
        ) : (
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
              onType={(t) => file && setType(file.key, t)}
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
                onSaved={async () => (await refresh(), setLoaded({ key: sel, text: (await api.memoryFile(sel)).text ?? "" }))}
              />
            )}
            {file?.off && sel.startsWith("g/rules/") && (
              <p className="border-b bg-muted/30 px-6 py-2 text-xs text-muted-foreground">
                {t("memory.main.ruleOff")}{" "}
                <button className="underline" onClick={() => toggleRule(file.key)}>
                  {t("memory.main.reactivate")}
                </button>
              </p>
            )}
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(e) => setDraft({ key: sel, text: e.target.value })}
                onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), save())}
                spellCheck={false}
                autoFocus
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
                aria-label={t("memory.main.fileContent")}
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-5xl px-6 py-6 text-sm leading-relaxed">
                  {failed !== undefined ? (
                    <p className="text-destructive">{failed || t("memory.main.unreadable")}</p>
                  ) : text === null ? (
                    <Skeleton className="h-32 w-full" />
                  ) : (
                    <>
                      {fm && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>}
                      <Markdown t={body} onLink={(h) => /^[^/:?#]+\.md$/.test(h) && (onPick(sel.slice(0, sel.lastIndexOf("/") + 1) + h), true)} />
                    </>
                  )}
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
        scopes={[["global", t("memory.main.scopeGlobal")], ...(list?.projects ?? []).filter((p) => p.files.some((x) => x.key.startsWith("m/"))).map((p) => [p.dir, tilde(p.project, homeDir)] as const)]}
        onLint={async (scope) => {
          const r = (await api.post("/memory-lint", { scope })) as { ok: boolean; out?: string; findings?: Finding[]; cost?: number }
          return r.ok ? { findings: r.findings, cost: r.cost } : { error: r.out, cost: r.cost }
        }}
      />
      <MemoryWriter target={writer} refs={writeRefs} onClose={() => setWriter(null)} onDraft={onDraft} />
    </div>
  )
}
