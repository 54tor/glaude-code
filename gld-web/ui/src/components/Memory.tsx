import { useEffect, useRef, useState } from "react"
import {
  ArchiveRestore,
  BookOpen,
  Brain,
  CheckSquare,
  ClipboardCopy,
  Copy,
  FileText,
  FolderInput,
  Globe,
  Link,
  Merge,
  Pencil,
  Save,
  Trash,
  Trash2,
  X,
} from "lucide-react"
import { toast } from "sonner"
import { api, type MemFile, type MemProject, type MemTarget, type Trashed } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
  menu,
}: {
  icon: typeof Globe
  title: string
  files: MemFile[]
  sel: string
  onPick: (k: string) => void
  picked: Set<string>
  onToggle: (k: string) => void
  onPickAll: (keys: string[]) => void
  menu: (f: MemFile) => React.ReactNode
}) {
  return (
    <section className="mb-3">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <p className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground" title={title}>
            <Icon className="size-3.5 shrink-0" /> <span className="truncate">{title}</span>
          </p>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-60">
          <ContextMenuItem onSelect={() => onPickAll(files.map((f) => f.key))}>
            <CheckSquare /> Tout sélectionner pour une fusion
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
                  <FileText className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{f.name}</span>
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

// Mémoire de Claude : globale (CLAUDE.md, rules/) et par projet (memory/, CLAUDE.md du projet), lecture et édition.
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
  const toggle = (k: string) =>
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  // Fusion par Claude : le serveur rédige le prompt ; on le copie, ou on l'ouvre dans une nouvelle session.
  const merge = async (launch: boolean) => {
    const r = (await api.post("/memory-merge", { keys: [...picked], launch, args })) as { ok: boolean; out: string; prompt?: string }
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
  const refresh = () => Promise.all([api.memory().then(setList), api.trash().then((r) => setTrash(r.items))])
  useEffect(() => {
    let live = true
    api.trash().then((r) => live && setTrash(r.items))
    api.memory().then((l) => live && setList(l))
    return () => {
      live = false
    }
  }, [])
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
  const match = (x: MemFile, scope = "") => !f || x.name.toLowerCase().includes(f) || scope.toLowerCase().includes(f)

  const save = async () => {
    if (!editing) return
    const r = await api.post("/memory-save", { key: sel, text: editing.text })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setLoaded({ key: sel, text: editing.text })
      setDraft(null)
      refresh()
    }
  }
  const edit = (key: string) => {
    if (key === sel && text !== null) return setDraft({ key, text })
    editNext.current = key
    onPick(key)
  }
  const move = async (key: string, to: string) => {
    const r = (await api.post("/memory-move", { key, to })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      refresh()
      if (key === sel) onPick(r.key)
    }
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
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => remove(x)}>
        <Trash2 /> Supprimer
      </ContextMenuItem>
    </>
  )
  const section = (key: string, icon: typeof Globe, title: string, files: MemFile[]) => (
    <Section
      key={key}
      icon={icon}
      title={title}
      files={files}
      sel={sel}
      onPick={onPick}
      picked={picked}
      onToggle={toggle}
      onPickAll={(keys) => setPicked((p) => new Set([...p, ...keys]))}
      menu={menu}
    />
  )

  const trashed = trash.find((t) => t.id === inTrash)
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
        const r = await api.post("/trash-purge", { id: t?.id ?? "" })
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
      <ScrollArea className="max-h-64 shrink-0 border-b md:max-h-none md:w-80 md:border-r md:border-b-0">
        <nav className="p-2">
          {!list && <Skeleton className="m-2 h-24" />}
          {list &&
            list.global.some((x) => match(x, "globale")) &&
            section(
              "global",
              Globe,
              "Globale",
              list.global.filter((x) => match(x, "globale"))
            )}
          {list?.projects.map((p) => {
            const files = p.files.filter((x) => match(x, p.project))
            return files.length ? section(p.dir, Brain, tilde(p.project, homeDir) + (p.missing ? " (introuvable)" : ""), files) : null
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
              <pre className="session-text mx-auto max-w-7xl px-6 py-6 whitespace-pre-wrap">{text ?? ""}</pre>
            </ScrollArea>
          </>
        ) : !sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">
            Choisis un fichier de mémoire à afficher. Clic droit pour ses actions.
          </p>
        ) : (
          <>
            <div className="flex items-center gap-2 border-b px-6 py-2">
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={file?.path}>
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
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => file && remove(file)}
                    disabled={!file}
                  >
                    <Trash2 /> Supprimer
                  </Button>
                  <Button size="sm" onClick={() => edit(sel)} disabled={text === null}>
                    <Pencil /> Modifier
                  </Button>
                </>
              )}
            </div>
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
    </div>
  )
}
