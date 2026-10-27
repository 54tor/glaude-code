import { type LucideIcon, AlertTriangle, ChevronRight, Calculator, ArrowDownWideNarrow, ArrowUpNarrowWide, CheckSquare, EyeOff, Filter, FileStack, FileText, Plus, ScrollText, Sparkles, Trash2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { MemFile } from "@/lib/api"
import { ago } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useLocal } from "@/hooks/useLocal"
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
  onPick: (k: string) => void
  picked: Set<string>
  onToggle: (k: string) => void
  onPickAll: (keys: string[]) => void
  onNew: () => void // nouvelle entrée écrite à la main
  onNewRule?: () => void // nouvelle règle du projet (.claude/rules), sections de projet existantes seulement
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
  const { t } = useTranslation()
  const [, labelKey, SortIcon] = SORTS.find((s) => s[0] === sort.by) ?? SORTS[0]
  const label = t(labelKey)
  // Repliée par défaut, sauf si elle contient le fichier ouvert ; le choix de l'utilisateur est gardé par section.
  const [pref, setPref] = useLocal(`gld.mem.open.${title}`, "")
  const open = pref ? pref === "1" : files.some((f) => f.key === sel)
  const sortBtn = "size-5 md:opacity-0 md:group-hover/sec:opacity-100 md:focus-visible:opacity-100"
  return (
    <section className="group/sec mb-2">
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div className="flex items-center gap-1.5 px-2 py-1 text-xs font-semibold text-muted-foreground" title={title}>
            <button type="button" className="flex min-w-0 flex-1 items-center gap-1.5 text-left" aria-expanded={open} onClick={() => setPref(open ? "0" : "1")}>
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
                {t("memory.section.tokens", { amount: budget >= 1000 ? `${(budget / 1000).toFixed(1)} k` : budget })}
              </span>
            )}
            <Button variant="ghost" size="icon-xs" className={sortBtn} onClick={onNew} title={t("memory.section.newEntry")} aria-label={t("memory.section.newEntryAria", { title })}>
              <Plus />
            </Button>
            {/* Deux icônes : le critère (un clic passe au suivant) et le sens, pour ce dossier. */}
            <Button
              variant="ghost"
              size="icon-xs"
              className={sortBtn}
              onClick={() => onSort({ ...sort, by: SORTS[(SORTS.findIndex((s) => s[0] === sort.by) + 1) % SORTS.length][0] })}
              title={t("memory.section.sortTitle", { label })}
              aria-label={t("memory.section.sortAria", { title, label })}
            >
              <SortIcon />
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              className={sortBtn}
              onClick={() => onSort({ ...sort, dir: sort.dir === "asc" ? "desc" : "asc" })}
              title={sort.dir === "asc" ? t("memory.section.ascTitle") : t("memory.section.descTitle")}
              aria-label={sort.dir === "asc" ? t("memory.section.ascAria", { title }) : t("memory.section.descAria", { title })}
            >
              {sort.dir === "asc" ? <ArrowUpNarrowWide /> : <ArrowDownWideNarrow />}
            </Button>
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-60">
          <ContextMenuItem onSelect={onNew}>
            <Plus /> {t("memory.section.menuNew")}
          </ContextMenuItem>
          {onNewRule && (
            <ContextMenuItem onSelect={onNewRule}>
              <ScrollText /> {t("memory.section.menuNewRule")}
            </ContextMenuItem>
          )}
          {onWrite && (
            <ContextMenuItem onSelect={onWrite}>
              <Sparkles /> {t("memory.section.menuNewHaiku")}
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
          <ContextMenuItem onSelect={onCount}>
            <Calculator /> {t("memory.section.menuCount")}
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => onPickAll(files.map((f) => f.key))}>
            <CheckSquare /> {t("memory.section.menuSelectAll")}
          </ContextMenuItem>
          <ContextMenuItem variant="destructive" onSelect={() => onRemoveAll(files.map((f) => f.key))}>
            <Trash2 /> {t("memory.section.menuRemoveAll")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {/* Trait de filiation comme dans l'arbre de la nav : les fichiers sont rattachés à leur section. */}
      <ul className={cn("ml-[15px] space-y-px border-l pl-1", !open && "hidden")}>
        {files.map((f) => (
          <ContextMenu key={f.key}>
            <ContextMenuTrigger asChild>
              <li className="group flex items-center gap-1.5 rounded-md data-[state=open]:bg-muted">
                <Checkbox
                  checked={picked.has(f.key)}
                  onCheckedChange={() => onToggle(f.key)}
                  aria-label={t("memory.section.selectAria", { name: f.name })}
                  className={cn(
                    "transition-opacity",
                    picked.size === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100"
                  )}
                />
                <button
                  onClick={() => onPick(f.key)}
                  className={cn(
                    "flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                    f.key === sel ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  {typeOf(f.type) ? (
                    (() => {
                      const T = typeOf(f.type)!
                      return <T.icon className={cn("size-3.5 shrink-0", T.color)} aria-label={t("memory.section.typeAria", { key: T.key })} />
                    })()
                  ) : (
                    <FileText className="size-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className={cn("min-w-0 flex-1 truncate", f.off && "text-muted-foreground line-through")}>{f.name}</span>
                  {f.off && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("memory.section.ruleOff")} />}
                  {f.paths && (
                    <Filter className="size-3.5 shrink-0 text-info" aria-label={t("memory.section.ruleScoped")} />
                  )}
                  {(f.warn || issues.has(f.key)) && (
                    <AlertTriangle className="size-3.5 shrink-0 text-warning" aria-label={t("memory.section.toCheck")} />
                  )}
                  <span className="flex shrink-0 flex-col items-end text-xs font-normal leading-tight text-muted-foreground">
                    <span>{ago(f.mtime)}</span>
                    <span className="text-2xs tabular-nums">{f.tokens} tok</span>
                  </span>
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
