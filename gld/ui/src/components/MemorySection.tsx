import { type LucideIcon, AlertTriangle, MoreHorizontal, ChevronRight, Calculator, ArrowDownWideNarrow, ArrowUpNarrowWide, CheckSquare, EyeOff, Filter, FileStack, FileText, Plus, BotMessageSquare, Trash2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { MemFile } from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useLocal } from "@/hooks/useLocal"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SORTS, type Sort } from "./memoryLib"
import { typeOf } from "./MemoryTypes"

const TOKENS_PER_THOUSAND = 1000

const formatTokenAmount = (tokens: number) => (tokens >= TOKENS_PER_THOUSAND ? `${(tokens / TOKENS_PER_THOUSAND).toFixed(1)} k` : tokens)

function FileTypeIcon({ file }: { file: MemFile }) {
  const { t } = useTranslation()
  const memoryType = typeOf(file.type)
  if (!memoryType) return <FileText className="size-3.5 shrink-0 text-muted-foreground" />
  return <memoryType.icon className={cn("size-3.5 shrink-0", memoryType.color)} aria-label={t("memory.section.typeAria", { key: memoryType.key })} />
}

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
  onNewRule,
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
  onPick: (key: string) => void
  picked: Set<string>
  onToggle: (key: string) => void
  onPickAll: (keys: string[]) => void
  onNew: () => void // nouvelle entrée écrite à la main
  onNewRule?: () => void // nouvelle règle du projet (.claude/rules), sections de projet existantes seulement
  onWrite?: () => void // nouvelle entrée rédigée par un modèle (absent : section sans mémoire de projet)
  onConsolidate?: (file: string) => void // consolider dans le CLAUDE.md (ou l'AGENTS.md, projets seulement) correspondant
  agents?: boolean // propose aussi AGENTS.md
  onRemoveAll: (keys: string[]) => void
  menu: (file: MemFile) => React.ReactNode
  sort: Sort
  onSort: (nextSort: Sort) => void
  budget: number // tokens chargés à chaque session par cette section
  exact: boolean // tous les fichiers comptés l'ont été réellement (sinon estimation)
  onCount: () => void // décompte réel des tokens de la section
  issues: Map<string, number> // problèmes de santé par clé
}) {
  const { t } = useTranslation()
  // Repliée par défaut, sauf si elle contient le fichier ouvert ; le choix de l'utilisateur est gardé par section.
  const [pref, setPref] = useLocal(`gld.mem.open.${title}`, "")
  const open = pref ? pref === "1" : files.some((file) => file.key === sel)
  const sortButtonClassName = "size-5 md:opacity-0 md:group-hover/sec:opacity-100 md:focus-visible:opacity-100"
  return (
    <section className="group/sec mb-2">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div className="flex items-center gap-1.5 px-2 py-1 text-xs font-semibold text-muted-foreground" title={title}>
            <button type="button" className="flex min-w-0 flex-1 items-center gap-1.5 text-left pointer-coarse:min-h-8" aria-expanded={open} onClick={() => setPref(open ? "0" : "1")}>
              <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
              <Icon className="size-3.5 shrink-0" /> <h3 className="truncate">{title}</h3>
              {!open && <span className="shrink-0 text-2xs font-normal">{files.length}</span>}
            </button>
            {budget > 0 && (
              <span
                className="shrink-0 text-2xs font-normal"
                title={exact ? t("memory.section.tokensExact") : t("memory.section.tokensEstimate")}
              >
                {exact ? "" : "≈ "}
                {t("memory.section.tokens", { amount: formatTokenAmount(budget) })}
              </span>
            )}
            {/* Ajout et tri regroupés : un seul bouton, au lieu de trois icônes côte à côte. */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-xs" className={sortButtonClassName} aria-label={t("memory.section.moreAria", { title })}>
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-52">
                <DropdownMenuItem onSelect={onNew}>
                  <Plus /> {t("memory.section.newEntry")}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuLabel>{t("memory.section.sortBy")}</DropdownMenuLabel>
                <DropdownMenuRadioGroup value={sort.by} onValueChange={(by) => onSort({ ...sort, by })}>
                  {SORTS.map(([sortKey, labelKey, SortIcon]) => (
                    <DropdownMenuRadioItem key={sortKey} value={sortKey}>
                      <SortIcon /> {t(labelKey)}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
                <DropdownMenuSeparator />
                <DropdownMenuRadioGroup value={sort.dir} onValueChange={(dir) => onSort({ ...sort, dir })}>
                  <DropdownMenuRadioItem value="asc">
                    <ArrowUpNarrowWide /> {t("memory.section.asc")}
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="desc">
                    <ArrowDownWideNarrow /> {t("memory.section.desc")}
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-60">
          <ContextMenuItem onSelect={onNew}>
            <Plus /> {t("memory.section.menuNew")}
          </ContextMenuItem>
          {onNewRule && (
            <ContextMenuItem onSelect={onNewRule}>
              <Plus /> {t("memory.section.menuNewRule")}
            </ContextMenuItem>
          )}
          {onWrite && (
            <ContextMenuItem onSelect={onWrite}>
              <BotMessageSquare /> {t("memory.section.menuNewHaiku")}
            </ContextMenuItem>
          )}
          {onConsolidate && (
            <ContextMenuItem onSelect={() => onConsolidate("CLAUDE.md")}>
              <FileStack /> {t("memory.section.menuConsolidate", { file: "CLAUDE.md" })}
            </ContextMenuItem>
          )}
          {onConsolidate && agents && (
            <ContextMenuItem onSelect={() => onConsolidate("AGENTS.md")}>
              <FileStack /> {t("memory.section.menuConsolidate", { file: "AGENTS.md" })}
            </ContextMenuItem>
          )}
          <ContextMenuSeparator />
          <ContextMenuItem onSelect={onCount}>
            <Calculator /> {t("memory.section.menuCount")}
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => onPickAll(files.map((file) => file.key))}>
            <CheckSquare /> {t("memory.section.menuSelectAll")}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem variant="destructive" onSelect={() => onRemoveAll(files.map((file) => file.key))}>
            <Trash2 /> {t("memory.section.menuRemoveAll")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {/* Trait de filiation comme dans l'arbre de la nav : les fichiers sont rattachés à leur section. */}
      <ul className={cn("ml-[15px] space-y-px border-l pl-1", !open && "hidden")}>
        {files.map((file) => (
          <ContextMenu key={file.key}>
            <ContextMenuTrigger asChild>
              <li className="group flex items-center gap-1.5 rounded-md data-[state=open]:bg-muted">
                <Checkbox
                  checked={picked.has(file.key)}
                  onCheckedChange={() => onToggle(file.key)}
                  aria-label={t("memory.section.selectAria", { name: file.name })}
                  className={cn(
                    "transition-opacity",
                    picked.size === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
                  )}
                />
                <button
                  onClick={() => onPick(file.key)}
                  className={cn(
                    "flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                    file.key === sel ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  <FileTypeIcon file={file} />
                  <span className={cn("min-w-0 flex-1 truncate", file.off && "text-muted-foreground line-through")}>{file.name}</span>
                  {file.off && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("memory.section.ruleOff")} />}
                  {file.paths && (
                    <Filter className="size-3.5 shrink-0 text-info" aria-label={t("memory.section.ruleScoped")} />
                  )}
                  {(file.warn || issues.has(file.key)) && (
                    <AlertTriangle className="size-3.5 shrink-0 text-warning" aria-label={t("memory.section.toCheck")} />
                  )}
                  <span className="flex shrink-0 flex-col items-end text-xs font-normal leading-tight text-muted-foreground">
                    <span>{ago(file.mtime)}</span>
                    <span className="text-2xs tabular-nums">{file.tokens} tok</span>
                  </span>
                </button>
              </li>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-64">{menu(file)}</ContextMenuContent>
          </ContextMenu>
        ))}
      </ul>
    </section>
  )
}
