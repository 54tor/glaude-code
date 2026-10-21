import { type LucideIcon, AlertTriangle, Calculator, ArrowDownWideNarrow, ArrowUpNarrowWide, CheckSquare, EyeOff, Filter, FileStack, FileText, Plus, Sparkles, Trash2 } from "lucide-react"
import type { MemFile } from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SORTS, type Sort } from "./memoryLib"
import { typeOf } from "./MemoryTypes"

export function Section({
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
  exact,
  onCount,
  issues,
}: {
  icon: LucideIcon
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
  budget: number // tokens chargés à chaque session par cette section
  exact: boolean // tous les fichiers comptés l'ont été réellement (sinon estimation)
  onCount: () => void // décompte réel des tokens de la section
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
              <span
                className="shrink-0 text-[10px] font-normal"
                title={exact ? "Compte réel (tokenizer du modèle) de ce que cette section charge à chaque session" : "Estimation de ce que cette section charge à chaque session ; clic droit › Compter les tokens"}
              >
                {exact ? "" : "≈ "}
                {budget >= 1000 ? `${(budget / 1000).toFixed(1)} k` : budget} tokens
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
          <ContextMenuItem onSelect={onCount}>
            <Calculator /> Compter les tokens (Haiku)
          </ContextMenuItem>
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
