import { useEffect, useRef, useState } from "react"
import { useLocal } from "@/hooks/useLocal"
import {
  ALargeSmall,
  AlertTriangle,
  ArchiveRestore,
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  BookOpen,
  Brain,
  CheckSquare,
  CircleHelp,
  Clock,
  ClipboardCopy,
  Copy,
  Eye,
  EyeOff,
  Filter,
  FileStack,
  FileText,
  FolderInput,
  HardDrive,
  Globe,
  HeartPulse,
  Link,
  Merge,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash,
  Trash2,
  X,
} from "lucide-react"
import { toast } from "sonner"
import { api, type Issue, type MemFile, type MemProject, type MemTarget, type Trashed } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { withChoice } from "@/lib/models"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Choose } from "./Choose"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Ask } from "./Confirm"
import { Markdown } from "./Conversation"
import { MemoryWriter, type WriteTarget } from "./MemoryWriter"
import { DiffView, HealthDialog, ScopeBar, TypeHelp, TYPES, typeOf } from "./MemoryTools"

// Tri des fichiers de mémoire : critère et sens propres à chaque dossier (section), retenus dans le navigateur.
const SORTS = [
  ["date", "date de modification", Clock],
  ["name", "nom", ALargeSmall],
  ["size", "taille", HardDrive],
] as const
const byName = (a: string, b: string) => a.localeCompare(b, "fr", { numeric: true })
// Comparateur de fichiers ; à égalité (la taille est en Ko arrondis), le nom départage.
const cmpFiles = (by: string, dir: string) => (a: MemFile, b: MemFile) => {
  const d = (by === "name" ? byName(a.name, b.name) : by === "size" ? a.kb - b.kb : a.mtime - b.mtime) || byName(a.name, b.name)
  return dir === "asc" ? d : -d
}
type Sort = { by: string; dir: string }
const DEFAULT_SORT: Sort = { by: "date", dir: "desc" }

type List = { global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }

// En-tête YAML (--- … ---) des entrées de mémoire : affiché à part, en petit.
function split(t: string): [string, string] {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(t)
  return m ? [m[1], t.slice(m[0].length)] : ["", t]
}

// Seules les entrées de mémoire de projet et les règles globales changent de place (pas les CLAUDE.md).
const movable = (key: string) => key.startsWith("m/") || key.startsWith("g/rules/")
const home = (key: string) => (key.startsWith("m/") ? key.split("/")[1] : "global")

const copy = (text: string, what: string) =>
  navigator.clipboard.writeText(text).then(
    () => toast.success(`${what} copié`),
    () => toast.error("Copie impossible")
  )

function Section({
  icon: Icon,
  title,
  files,
  sel,
  onPick,
  picked,
  onToggle,
  onPickAll,
  onNew,
  onWrite,
  onConsolidate,
  agents,
  onRemoveAll,
  menu,
  sort,
  onSort,
  budget,
  issues,
}: {
  icon: typeof Globe
  title: string
  files: MemFile[]
  sel: string
  onPick: (k: string) => void
  picked: Set<string>
  onToggle: (k: string) => void
  onPickAll: (keys: string[]) => void
  onNew: () => void // nouvelle entrée écrite à la main
  onWrite?: () => void // nouvelle entrée rédigée par un modèle (absent : section sans mémoire de projet)
  onConsolidate?: (file: string) => void // consolider dans le CLAUDE.md (ou l'AGENTS.md, projets seulement) correspondant
  agents?: boolean // propose aussi AGENTS.md
  onRemoveAll: (keys: string[]) => void
  menu: (f: MemFile) => React.ReactNode
  sort: Sort
  onSort: (s: Sort) => void
  budget: number // tokens estimés chargés à chaque session par cette section
  issues: Map<string, number> // problèmes de santé par clé
}) {
  const [, label, SortIcon] = SORTS.find((s) => s[0] === sort.by) ?? SORTS[0]
  const sortBtn = "size-5 md:opacity-0 md:group-hover/sec:opacity-100 md:focus-visible:opacity-100"
  return (
    <section className="group/sec mb-3">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <p className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground" title={title}>
            <Icon className="size-3.5 shrink-0" /> <span className="flex-1 truncate">{title}</span>
            {budget > 0 && (
              <span className="shrink-0 text-[10px] font-normal" title="Estimation (octets / 4) de ce que cette section charge à chaque session">
                ≈ {budget >= 1000 ? `${(budget / 1000).toFixed(1)} k` : budget} tokens
              </span>
            )}
            <Button variant="ghost" size="icon-xs" className={sortBtn} onClick={onNew} title="Nouvelle entrée" aria-label="Nouvelle entrée">
              <Plus />
            </Button>
            {/* Deux icônes : le critère (un clic passe au suivant) et le sens, pour ce dossier. */}
            <Button
              variant="ghost"
              size="icon-xs"
              className={sortBtn}
              onClick={() => onSort({ ...sort, by: SORTS[(SORTS.findIndex((s) => s[0] === sort.by) + 1) % SORTS.length][0] })}
              title={`Tri par ${label} (cliquer pour changer)`}
              aria-label={`Trier par : ${label}`}
            >
              <SortIcon />
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              className={sortBtn}
              onClick={() => onSort({ ...sort, dir: sort.dir === "asc" ? "desc" : "asc" })}
              title={sort.dir === "asc" ? "Croissant (cliquer pour inverser)" : "Décroissant (cliquer pour inverser)"}
              aria-label={sort.dir === "asc" ? "Ordre croissant" : "Ordre décroissant"}
            >
              {sort.dir === "asc" ? <ArrowUpNarrowWide /> : <ArrowDownWideNarrow />}
            </Button>
          </p>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-60">
          <ContextMenuItem onSelect={onNew}>
            <Plus /> Nouvelle entrée…
          </ContextMenuItem>
          {onWrite && (
            <ContextMenuItem onSelect={onWrite}>
              <Sparkles /> Nouvelle entrée avec Haiku…
            </ContextMenuItem>
          )}
          {onConsolidate && (
            <ContextMenuItem onSelect={() => onConsolidate("CLAUDE.md")}>
              <FileStack /> Consolider dans CLAUDE.md (Haiku)…
            </ContextMenuItem>
          )}
          {onConsolidate && agents && (
            <ContextMenuItem onSelect={() => onConsolidate("AGENTS.md")}>
              <FileStack /> Consolider dans AGENTS.md (Haiku)…
            </ContextMenuItem>
          )}
          <ContextMenuItem onSelect={() => onPickAll(files.map((f) => f.key))}>
            <CheckSquare /> Tout sélectionner pour une fusion
          </ContextMenuItem>
          <ContextMenuItem variant="destructive" onSelect={() => onRemoveAll(files.map((f) => f.key))}>
            <Trash2 /> Supprimer tous les fichiers…
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      <ul className="space-y-px">
        {files.map((f) => (
          <ContextMenu key={f.key}>
            <ContextMenuTrigger asChild>
              <li className="group flex items-center gap-1.5 rounded-md data-[state=open]:bg-muted">
                <Checkbox
                  checked={picked.has(f.key)}
                  onCheckedChange={() => onToggle(f.key)}
                  aria-label={`Sélectionner ${f.name} pour une fusion`}
                  className={cn(
                    "ml-1 transition-opacity",
                    picked.size === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
                  )}
                />
                <button
                  onClick={() => onPick(f.key)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                    f.key === sel ? "bg-accent font-medium" : "hover:bg-muted"
                  )}
                >
                  {typeOf(f.type) ? (
                    (() => {
                      const T = typeOf(f.type)!
                      return <T.icon className={cn("size-3.5 shrink-0", T.color)} aria-label={`type ${T.key}`} />
                    })()
                  ) : (
                    <FileText className="size-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className={cn("min-w-0 flex-1 truncate", f.off && "text-muted-foreground line-through")}>{f.name}</span>
                  {f.off && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label="règle désactivée" />}
                  {f.paths && (
                    <Filter className="size-3.5 shrink-0 text-sky-500" aria-label="règle scopée" />
                  )}
                  {(f.warn || issues.has(f.key)) && (
                    <AlertTriangle className="size-3.5 shrink-0 text-amber-500" aria-label="à vérifier" />
                  )}
                  <span className="shrink-0 text-xs font-normal text-muted-foreground">{ago(f.mtime)}</span>
                </button>
              </li>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-64">{menu(f)}</ContextMenuContent>
          </ContextMenu>
        ))}
      </ul>
    </section>
  )
}

// Mémoire de Claude : globale (CLAUDE.md, rules/) et par projet (memory/, CLAUDE.md et AGENTS.md du projet), lecture et édition.
export function Memory({
  sel,
  home: homeDir,
  q,
  onPick,
  onAsk,
  args,
}: {
  sel: string
  home: string
  q: string
  onPick: (k: string) => void
  onAsk: (a: Ask) => void
  args: string
}) {
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
  // Fusion par Claude : le serveur rédige le prompt ; on le copie, ou on l'ouvre dans une nouvelle session (Haiku : tâche mécanique).
  const merge = async (launch: boolean) => {
    const r = (await api.post("/memory-merge", { keys: [...picked], launch, args: withChoice(args, { model: "haiku", effort: "" }) })) as { ok: boolean; out: string; prompt?: string }
    if (!launch && r.ok && r.prompt) return void copy(r.prompt, "Prompt de fusion")
    ;(r.ok ? toast.success : toast.error)(r.out.trim() || (r.ok ? "Session lancée" : "Échec"))
    if (r.ok) setPicked(new Set())
  }
  const [list, setList] = useState<List | null>(null)
  const [loaded, setLoaded] = useState<{ key: string; text: string } | null>(null)
  const [draft, setDraft] = useState<{ key: string; text: string } | null>(null)
  const editNext = useRef("") // « Modifier » depuis le clic droit : passe en édition dès le chargement
  const text = loaded?.key === sel ? loaded.text : null
  const editing = draft?.key === sel ? draft : null

  // Corbeille : les versions supprimées ou remplacées ; sel vaut « corbeille/<id> » pour en afficher une.
  const [trash, setTrash] = useState<Trashed[]>([])
  const inTrash = sel.startsWith("corbeille/") ? sel.slice(10) : ""
  const [issues, setIssues] = useState<Issue[]>([])
  const [healthOpen, setHealthOpen] = useState(false)
  const [typeFilter, setTypeFilter] = useState("")
  const [typeHelp, setTypeHelp] = useState(false)
  const [hits, setHits] = useState<{ q: string; keys: Set<string> }>({ q: "", keys: new Set() })
  const refresh = () => Promise.all([api.memory().then(setList), api.trash().then((r) => setTrash(r.items)), api.memoryHealth().then((r) => setIssues(r.issues))])
  useEffect(() => {
    let live = true
    api.trash().then((r) => live && setTrash(r.items))
    api.memory().then((l) => live && setList(l))
    api.memoryHealth().then((r) => live && setIssues(r.issues))
    return () => {
      live = false
    }
  }, [])
  // La mémoire change hors de l'interface (une session qui écrit, un éditeur) : on relit la liste et le fichier ouvert
  // toutes les 15 s tant que l'onglet est visible, et au retour sur l'onglet.
  useEffect(() => {
    const tick = () => {
      if (document.visibilityState !== "visible") return
      api.memory().then(setList, () => {})
      api.trash().then((r) => setTrash(r.items), () => {})
      api.memoryHealth().then((r) => setIssues(r.issues), () => {})
      if (sel && !inTrash)
        api.memoryFile(sel).then(
          (r) => r.text !== undefined && setLoaded((l) => (l?.key === sel && l.text === r.text ? l : { key: sel, text: r.text! })),
          () => {}
        )
    }
    const t = setInterval(tick, 15_000)
    document.addEventListener("visibilitychange", tick)
    return () => {
      clearInterval(t)
      document.removeEventListener("visibilitychange", tick)
    }
  }, [sel, inTrash])
  useEffect(() => {
    if (!sel) return
    let live = true
    const get = inTrash ? api.trashFile(inTrash) : api.memoryFile(sel)
    get.then((r) => {
      if (!live) return
      setLoaded({ key: sel, text: r.text ?? r.error ?? "" })
      if (editNext.current === sel && r.text !== undefined) setDraft({ key: sel, text: r.text })
      editNext.current = ""
    })
    return () => {
      live = false
    }
  }, [sel, inTrash])

  const all = list ? [...list.global, ...list.projects.flatMap((p) => p.files)] : []
  const file = all.find((f) => f.key === sel)
  const f = q.toLowerCase()
  // Recherche aussi dans le contenu des fichiers (côté serveur, après une courte pause de frappe).
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
  const match = (x: MemFile, scope = "") =>
    (!typeFilter || x.type === typeFilter) &&
    (!f || x.name.toLowerCase().includes(f) || scope.toLowerCase().includes(f) || !!hitKeys?.has(x.key))
  const issueKeys = new Map<string, number>()
  for (const i of issues) issueKeys.set(i.key, (issueKeys.get(i.key) ?? 0) + 1)

  const save = async () => {
    if (!editing) return
    // Nouvelle entrée de mémoire de projet ou de règle globale : le nom saisi fixe le fichier.
    const m = /^(m\/[^/]+|g\/rules)\/[^/]+$/.exec(sel)
    const key = !file && m ? newKey(m[1] === "g/rules" ? "global" : m[1].slice(2), editing.text) : sel
    const r = await api.post("/memory-save", { key, text: editing.text, index: !file })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
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
  const consolidate = (scope: string, label: string, file = "CLAUDE.md") => {
    const refs =
      scope === "global" ? (list?.global ?? []).filter((x) => x.key.startsWith("g/rules/")) : all.filter((x) => x.key.startsWith(`m/${scope}/`))
    setWriter({
      kind: "rewrite",
      key: scope === "global" ? "g/CLAUDE.md" : `p/${scope}/${file}`,
      name: scope === "global" ? "~/.claude/CLAUDE.md" : `${file} de ${label}`,
      refs: refs.map((x) => x.key),
      instruction:
        scope === "global"
          ? "Consolide ces règles globales dans ce CLAUDE.md : garde les consignes durables, regroupe-les par thème, retire les doublons. Conserve telles quelles les lignes d'import existantes (« @fichier »)."
          : `Consolide ces entrées de mémoire dans le ${file} du projet : garde les règles durables, utiles à chaque session, regroupe-les par thème, retire les doublons et ce qui est périmé.`,
    })
  }
  // Clé d'une nouvelle entrée : le nom du front matter (ou le premier titre), simplifié en nom de fichier, sans écraser un fichier existant.
  const newKey = (scope: string, text: string) => {
    const name = /^name:\s*["']?([^"'\n]+)/m.exec(text)?.[1] ?? /^#\s+(.+)$/m.exec(text)?.[1] ?? "entree"
    const slug =
      name
        .normalize("NFD")
        .replace(/[\u0300-\u036f]/g, "")
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-|-$/g, "") || "entree"
    const base = scope === "global" ? "g/rules/" : `m/${scope}/`
    let key = base + slug + ".md"
    for (let i = 2; all.some((x) => x.key === key); i++) key = `${base}${slug}-${i}.md`
    return key
  }
  const onDraft = (t: WriteTarget, text: string) => {
    const key = t.kind === "rewrite" ? t.key : newKey(t.scope, text)
    setDraft({ key, text })
    onPick(key)
  }
  // Entrée écrite à la main : un modèle prérempli, la clé se fixe à l'enregistrement d'après le nom.
  const blank = (scope: string) => onDraft({ kind: "new", scope, label: "" }, scope === "global" ? "# Nouvelle règle\n\n" : "---\nname: nouvelle-entree\ndescription: \nmetadata:\n  type: project\n---\n\n")
  const edit = (key: string) => {
    if (key === sel && text !== null) return setDraft({ key, text })
    editNext.current = key
    onPick(key)
  }
  const move = async (key: string, to: string, copyIt = false) => {
    const r = (await api.post("/memory-move", { key, to, copy: copyIt })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      refresh()
      if (key === sel && !copyIt) onPick(r.key)
    }
  }
  // Règle globale désactivée : fichier renommé en .md.off, Claude ne la charge plus.
  const toggleRule = async (key: string) => {
    const r = (await api.post("/memory-toggle", { key })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      refresh()
      if (key === sel) onPick(r.key)
    }
  }
  const setType = async (key: string, type: string) => {
    const r = await api.post("/memory-type", { key, type })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      await refresh()
      setLoaded({ key, text: (await api.memoryFile(key)).text ?? "" })
    }
  }
  const repair = async (dir: string) => {
    const r = await api.post("/memory-repair", { dir })
    ;(r.ok ? toast.success : toast.error)(r.out)
    refresh()
  }
  const remove = (x: MemFile) =>
    onAsk({
      title: `Supprimer ${x.name} ?`,
      body: x.key.startsWith("m/") ? "Suppression définitive. Sa ligne dans MEMORY.md est retirée aussi." : "Suppression définitive.",
      confirm: "Supprimer",
      run: async () => {
        const r = await api.post("/memory-rm", { key: x.key })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          if (x.key === sel) onPick("")
          refresh()
        }
      },
    })
  // Suppression d'un groupe : chaque fichier va à la corbeille (restaurable), un à un, avec un bilan.
  const removeMany = (keys: string[], what: string) =>
    onAsk({
      title: `Supprimer ${what} (${keys.length} fichier${keys.length > 1 ? "s" : ""}) ?`,
      body: "Chaque fichier est copié à la corbeille avant d'être supprimé ; les lignes de MEMORY.md sont retirées aussi.",
      confirm: "Tout supprimer",
      run: async () => {
        let ok = 0
        const fails: string[] = []
        for (const key of keys) {
          const r = await api.post("/memory-rm", { key })
          if (r.ok) ok++
          else fails.push(r.out)
        }
        if (fails.length) toast.error(`${ok} supprimé${ok > 1 ? "s" : ""}, ${fails.length} échec${fails.length > 1 ? "s" : ""} : ${fails[0]}`)
        else toast.success(`${ok} fichier${ok > 1 ? "s" : ""} à la corbeille`)
        setPicked(new Set())
        if (keys.includes(sel)) onPick("")
        refresh()
      },
    })
  // Projets dont le dossier a disparu : leur mémoire n'est plus utilisée par personne.
  const gone = list?.projects.filter((p) => p.missing) ?? []
  const orphans = gone.flatMap((p) => p.files.map((x) => x.key))
  const orphanProjects = gone.length
  const copyContent = async (x: MemFile) => {
    const r = await api.memoryFile(x.key)
    if (r.text === undefined) return void toast.error(r.error ?? "Fichier illisible")
    copy(r.text, "Contenu")
  }
  const targets = (key: string) => (list?.targets ?? []).filter((t) => t.dir !== home(key))

  // Clic droit sur un fichier de mémoire.
  const menu = (x: MemFile) => (
    <>
      <ContextMenuLabel className="truncate">{x.name}</ContextMenuLabel>
      <ContextMenuItem onSelect={() => onPick(x.key)}>
        <BookOpen /> Ouvrir
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => edit(x.key)}>
        <Pencil /> Modifier
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => setWriter({ kind: "rewrite", key: x.key, name: x.name })}>
        <Sparkles /> Réécrire avec Haiku…
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => copyContent(x)}>
        <ClipboardCopy /> Copier le contenu
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => copy(x.path, "Chemin")}>
        <Link /> Copier le chemin
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => toggle(x.key)}>
        <Merge /> {picked.has(x.key) ? "Retirer de la fusion" : "Sélectionner pour une fusion"}
      </ContextMenuItem>
      {movable(x.key) && (
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <FolderInput /> Déplacer vers
          </ContextMenuSubTrigger>
          <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
            {home(x.key) !== "global" && (
              <>
                <ContextMenuItem onSelect={() => move(x.key, "global")}>
                  <Globe /> Règles globales (rules/)
                </ContextMenuItem>
                <ContextMenuSeparator />
              </>
            )}
            {targets(x.key).map((t) => (
              <ContextMenuItem key={t.dir} onSelect={() => move(x.key, t.dir)}>
                <Brain /> <span className="truncate">{tilde(t.project, homeDir)}</span>
              </ContextMenuItem>
            ))}
          </ContextMenuSubContent>
        </ContextMenuSub>
      )}
      {movable(x.key) && (
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Copy /> Copier vers
          </ContextMenuSubTrigger>
          <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
            {home(x.key) !== "global" && (
              <>
                <ContextMenuItem onSelect={() => move(x.key, "global", true)}>
                  <Globe /> Règles globales (rules/)
                </ContextMenuItem>
                <ContextMenuSeparator />
              </>
            )}
            {targets(x.key).map((t) => (
              <ContextMenuItem key={t.dir} onSelect={() => move(x.key, t.dir, true)}>
                <Brain /> <span className="truncate">{tilde(t.project, homeDir)}</span>
              </ContextMenuItem>
            ))}
          </ContextMenuSubContent>
        </ContextMenuSub>
      )}
      {x.key.startsWith("g/rules/") && (
        <ContextMenuItem onSelect={() => toggleRule(x.key)}>
          {x.off ? <Eye /> : <EyeOff />} {x.off ? "Réactiver la règle" : "Désactiver la règle"}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => remove(x)}>
        <Trash2 /> Supprimer
      </ContextMenuItem>
    </>
  )
  const section = (key: string, icon: typeof Globe, title: string, files: MemFile[], onWrite?: () => void, onConsolidate?: (file: string) => void) => (
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
      onWrite={onWrite}
      onConsolidate={onConsolidate}
      agents={key !== "global"}
      onRemoveAll={(keys) => removeMany(keys, `la section « ${title} »`)}
      menu={menu}
      sort={sortOf(key)}
      onSort={(s) => setSortsRaw(JSON.stringify({ ...sorts, [key]: s }))}
      budget={files.filter((x) => x.always).reduce((n, x) => n + x.tokens, 0)}
      issues={issueKeys}
    />
  )

  const trashed = trash.find((t) => t.id === inTrash)
  // Comparaison de la version de la corbeille avec le fichier actuel (s'il existe encore).
  const [diff, setDiff] = useState(false)
  const [current, setCurrent] = useState<string | null>(null)
  useEffect(() => {
    if (!diff || !trashed) return
    let live = true
    api.memoryFile(trashed.key).then((r) => live && setCurrent(r.text ?? ""))
    return () => {
      live = false
    }
  }, [diff, trashed])
  const restore = async (t: Trashed) => {
    const r = (await api.post("/trash-restore", { id: t.id })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      await refresh()
      if (r.key) onPick(r.key)
    }
  }
  const purge = (t?: Trashed) =>
    onAsk({
      title: t ? "Supprimer définitivement cette version ?" : `Vider la corbeille (${trash.length}) ?`,
      body: "Cette fois, c'est définitif.",
      confirm: t ? "Supprimer" : "Vider",
      run: async () => {
        const r = await api.post("/trash-purge", t ? { id: t.id } : { all: true })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          if (!t || t.id === inTrash) onPick("")
          refresh()
        }
      },
    })
  const shortPath = (p: string) => (homeDir && p.startsWith(homeDir) ? "~" + p.slice(homeDir.length) : p)

  const [fm, body] = split(text ?? "")
  return (
    <div className="relative flex min-h-0 flex-1 flex-col md:flex-row">
      <ScrollArea className="max-h-64 shrink-0 border-b md:max-h-none md:w-[28rem] md:border-r md:border-b-0">
        <nav className="p-2">
          {!list && <Skeleton className="m-2 h-24" />}
          {list && (
            <div className="mb-2 flex flex-wrap items-center gap-1 px-1">
              <Button
                variant={issues.length ? "outline" : "ghost"}
                size="xs"
                onClick={() => setHealthOpen(true)}
                title="Index MEMORY.md, imports et liens cassés, front matter, entrées anciennes"
              >
                <HeartPulse className={issues.length ? "text-amber-500" : ""} /> Santé{issues.length > 0 && ` (${issues.length})`}
              </Button>
              {TYPES.map((t) => (
                <Button
                  key={t.key}
                  variant={typeFilter === t.key ? "secondary" : "ghost"}
                  size="xs"
                  onClick={() => setTypeFilter(typeFilter === t.key ? "" : t.key)}
                  aria-pressed={typeFilter === t.key}
                  title={`Seulement les entrées de type ${t.key} (${t.label})`}
                >
                  <t.icon className={t.color} /> {all.filter((x) => x.type === t.key).length}
                </Button>
              ))}
              <Button variant="ghost" size="icon-xs" onClick={() => setTypeHelp(true)} title="À quoi servent les types de mémoire ?" aria-label="Aide sur les types">
                <CircleHelp />
              </Button>
            </div>
          )}
          {orphans.length > 0 && (
            <Button
              variant="outline"
              size="xs"
              className="mb-2 w-full"
              onClick={() => removeMany(orphans, `les mémoires de ${orphanProjects} projet${orphanProjects > 1 ? "s" : ""} introuvable${orphanProjects > 1 ? "s" : ""}`)}
              title="Dossiers de projet disparus : leurs fichiers de mémoire vont à la corbeille"
            >
              <Trash2 /> Nettoyer les projets introuvables ({orphanProjects})
            </Button>
          )}
          {list &&
            list.global.some((x) => match(x, "globale")) &&
            section(
              "global",
              Globe,
              "Globale",
              list.global.filter((x) => match(x, "globale")),
              () => setWriter({ kind: "new", scope: "global", label: "règles globales" }),
              () => consolidate("global", "")
            )}
          {list?.projects.map((p) => {
            const files = p.files.filter((x) => match(x, p.project))
            const label = tilde(p.project, homeDir)
            return files.length
              ? section(
                  p.dir,
                  Brain,
                  label + (p.missing ? " (introuvable)" : ""),
                  files,
                  () => setWriter({ kind: "new", scope: p.dir, label }),
                  // un CLAUDE.md de projet s'écrit dans son dossier : impossible s'il a disparu, inutile sans mémoire
                  !p.missing && p.files.some((x) => x.key.startsWith("m/")) ? (file) => consolidate(p.dir, label, file) : undefined
                )
              : null
          })}
          {trash.length > 0 && (
            <section className="mt-4 border-t pt-2">
              <div className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground">
                <Trash className="size-3.5" /> <span className="flex-1">Corbeille</span>
                <Button variant="ghost" size="xs" className="h-5 px-1.5 text-xs" onClick={() => purge()}>
                  Vider
                </Button>
              </div>
              <ul className="space-y-px">
                {trash.map((t) => (
                  <li key={t.id}>
                    <button
                      onClick={() => onPick("corbeille/" + t.id)}
                      title={t.path}
                      className={cn(
                        "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                        t.id === inTrash ? "bg-accent font-medium" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                      )}
                    >
                      <FileText className="size-3.5 shrink-0" />
                      <span className="min-w-0 flex-1 truncate">{t.path.split(/[\\/]/).pop()}</span>
                      <span className="shrink-0 text-xs font-normal">
                        {t.reason} {ago(t.at)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </nav>
      </ScrollArea>
      {picked.size > 0 && (
        <div className="flex flex-wrap items-center gap-1 border-b bg-popover px-3 py-2 text-xs md:absolute md:bottom-4 md:left-2 md:w-[19rem] md:rounded-xl md:border md:shadow-lg">
          <span className="mr-auto font-medium">
            {picked.size} fichier{picked.size > 1 ? "s" : ""}
          </span>
          <Button variant="ghost" size="xs" onClick={() => merge(false)} disabled={picked.size < 2} title="Copier le prompt de fusion">
            <Copy /> Prompt
          </Button>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => {
              const m = [...picked].find((k) => k.startsWith("m/"))
              const dir = m?.split("/")[1]
              const p = list?.projects.find((x) => x.dir === dir)
              setWriter(
                dir
                  ? { kind: "new", scope: dir, label: tilde(p?.project ?? dir, homeDir) }
                  : { kind: "new", scope: "global", label: "règles globales" }
              )
            }}
            title="Nouvelle entrée rédigée par Haiku à partir de ces fichiers"
          >
            <Sparkles /> Haiku
          </Button>
          <Button variant="ghost" size="xs" onClick={() => removeMany([...picked], "la sélection")} title="Mettre ces fichiers à la corbeille">
            <Trash2 /> Supprimer
          </Button>
          <Button size="xs" onClick={() => merge(true)} disabled={picked.size < 2} title="Nouvelle session Claude qui fusionne ces fichiers">
            <Merge /> Fusionner avec Claude
          </Button>
          <Button variant="ghost" size="icon-xs" onClick={() => setPicked(new Set())} aria-label="Vider la sélection">
            <X />
          </Button>
        </div>
      )}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {inTrash ? (
          <>
            <div className="flex items-center gap-2 border-b px-6 py-2">
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={trashed?.path}>
                {trashed ? `${shortPath(trashed.path)} · ${trashed.reason} ${ago(trashed.at)}` : "Introuvable dans la corbeille"}
              </code>
              {trashed && (
                <>
                  {all.some((x) => x.key === trashed.key) && (
                    <Button variant={diff ? "secondary" : "ghost"} size="sm" onClick={() => setDiff(!diff)} aria-pressed={diff}>
                      Comparer à l'actuelle
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => purge(trashed)}>
                    <Trash2 /> Supprimer définitivement
                  </Button>
                  <Button size="sm" onClick={() => restore(trashed)}>
                    <ArchiveRestore /> Restaurer
                  </Button>
                </>
              )}
            </div>
            <ScrollArea className="min-h-0 flex-1">
              {diff && current !== null ? (
                <DiffView old={text ?? ""} cur={current} />
              ) : (
                <pre className="session-text mx-auto max-w-7xl px-6 py-6 whitespace-pre-wrap">{text ?? ""}</pre>
              )}
            </ScrollArea>
          </>
        ) : !sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">
            Choisis un fichier de mémoire à afficher. Clic droit pour ses actions.
          </p>
        ) : (
          <>
            <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b px-6 py-2">
              <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={file?.path}>
                {file ? `${file.name} · ${size(file.kb)}` : sel}
              </code>
              {editing ? (
                <>
                  <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                    <X /> Annuler
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === text}>
                    <Save /> Enregistrer
                  </Button>
                </>
              ) : (
                <>
                  {movable(sel) && list && file && (
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="sm">
                          <FolderInput /> Déplacer vers…
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                        {home(sel) !== "global" && (
                          <>
                            <DropdownMenuItem onSelect={() => move(sel, "global")}>
                              <Globe /> Règles globales (rules/)
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                          </>
                        )}
                        <DropdownMenuLabel>Mémoire du projet</DropdownMenuLabel>
                        {targets(sel).map((t) => (
                          <DropdownMenuItem key={t.dir} onSelect={() => move(sel, t.dir)}>
                            <Brain /> <span className="truncate">{tilde(t.project, homeDir)}</span>
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
                  {file && (sel.startsWith("m/") || sel.startsWith("g/rules/")) && !sel.endsWith("/MEMORY.md") && (
                    <Choose
                      value={file.type ?? ""}
                      onChange={(t) => setType(file.key, t)}
                      options={[["", "Sans type"], ...TYPES.map((t) => [t.key, `${t.key} (${t.label})`] as const)]}
                      label="Type de l'entrée"
                      className="w-44"
                    />
                  )}
                  {file && sel.startsWith("g/rules/") && (
                    <Button variant="ghost" size="sm" onClick={() => toggleRule(file.key)}>
                      {file.off ? <Eye /> : <EyeOff />} {file.off ? "Réactiver" : "Désactiver"}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => file && remove(file)}
                    disabled={!file}
                  >
                    <Trash2 /> Supprimer
                  </Button>
                  {file && (
                    <Button variant="ghost" size="sm" onClick={() => setWriter({ kind: "rewrite", key: file.key, name: file.name })}>
                      <Sparkles /> Haiku…
                    </Button>
                  )}
                  <Button size="sm" onClick={() => edit(sel)} disabled={text === null}>
                    <Pencil /> Modifier
                  </Button>
                </>
              )}
            </div>
            {file && !editing && sel.startsWith("g/rules/") && !file.off && list && (
              <ScopeBar key={file.key + (file.paths ?? []).join("|")} file={file} projects={list.projects} home={homeDir} onSaved={async () => (await refresh(), setLoaded({ key: sel, text: (await api.memoryFile(sel)).text ?? "" }))} />
            )}
            {file?.off && sel.startsWith("g/rules/") && (
              <p className="border-b bg-muted/30 px-6 py-2 text-xs text-muted-foreground">
                Règle désactivée : Claude ne la charge plus.{" "}
                <button className="underline" onClick={() => toggleRule(file.key)}>
                  Réactiver
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
                aria-label="Contenu du fichier"
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-7xl px-6 py-6 text-sm leading-relaxed">
                  {text === null ? (
                    <Skeleton className="h-32 w-full" />
                  ) : (
                    <>
                      {fm && (
                        <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>
                      )}
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
      <HealthDialog open={healthOpen} issues={issues} onClose={() => setHealthOpen(false)} onOpenKey={onPick} onRepair={repair} />
      <MemoryWriter target={writer} refs={writeRefs} onClose={() => setWriter(null)} onDraft={onDraft} />
    </div>
  )
}
