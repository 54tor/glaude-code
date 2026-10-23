import { Bot, CircleHelp, Copy, HeartPulse, Merge, Trash2, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { MemFile } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { TYPES } from "./MemoryTypes"

// Haut de la liste : santé, filtre par type (avec aide) et nettoyage des projets introuvables.
export function Toolbar({
  issues,
  all,
  typeFilter,
  orphanProjects,
  onHealth,
  onType,
  onTypeHelp,
  onCleanOrphans,
}: {
  issues: number
  all: MemFile[]
  typeFilter: string
  orphanProjects: number
  onHealth: () => void
  onType: (t: string) => void
  onTypeHelp: () => void
  onCleanOrphans: () => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-1 px-1">
        <Button
          variant={issues ? "outline" : "ghost"}
          size="xs"
          onClick={onHealth}
          title={t("memory.bars.healthTitle")}
        >
          <HeartPulse className={issues ? "text-amber-500" : ""} /> {t("memory.bars.health")}{issues > 0 && ` (${issues})`}
        </Button>
        {TYPES.map((x) => (
          <Button
            key={x.key}
            variant={typeFilter === x.key ? "secondary" : "ghost"}
            size="xs"
            onClick={() => onType(typeFilter === x.key ? "" : x.key)}
            aria-pressed={typeFilter === x.key}
            title={t("memory.bars.typeTitle", { key: x.key, label: x.label })}
          >
            <x.icon className={x.color} /> {all.filter((f) => f.type === x.key).length}
          </Button>
        ))}
        <Button variant="ghost" size="icon-xs" onClick={onTypeHelp} title={t("memory.bars.typeHelpTitle")} aria-label={t("memory.bars.typeHelpAria")}>
          <CircleHelp />
        </Button>
      </div>
      {orphanProjects > 0 && (
        <Button
          variant="outline"
          size="xs"
          className="mb-2 w-full"
          onClick={onCleanOrphans}
          title={t("memory.bars.cleanTitle")}
        >
          <Trash2 /> {t("memory.bars.clean", { n: orphanProjects })}
        </Button>
      )}
    </>
  )
}

// Fichiers cochés : prompt de fusion, entrée rédigée par Haiku, suppression, fusion par Claude.
export function SelectionBar({
  count,
  onPrompt,
  onHaiku,
  onRemove,
  onMerge,
  onClear,
}: {
  count: number
  onPrompt: () => void
  onHaiku: () => void
  onRemove: () => void
  onMerge: () => void
  onClear: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center gap-1 border-b bg-popover px-3 py-2 text-xs md:absolute md:bottom-4 md:left-2 md:w-[19rem] md:rounded-xl md:border md:shadow-lg">
      <span className="mr-auto font-medium">{t("memory.bars.files", { count })}</span>
      <Button variant="ghost" size="xs" onClick={onPrompt} disabled={count < 2} title={t("memory.bars.promptTitle")}>
        <Copy /> {t("memory.bars.prompt")}
      </Button>
      <Button variant="ghost" size="xs" onClick={onHaiku} title={t("memory.bars.writeTitle")}>
        <Bot /> {t("memory.bars.write")}
      </Button>
      <Button variant="ghost" size="xs" onClick={onRemove} title={t("memory.bars.removeTitle")}>
        <Trash2 /> {t("memory.bars.remove")}
      </Button>
      <Button size="xs" onClick={onMerge} disabled={count < 2} title={t("memory.bars.mergeTitle")}>
        <Merge /> {t("memory.bars.merge")}
      </Button>
      <Button variant="ghost" size="icon-xs" onClick={onClear} aria-label={t("memory.bars.clearAria")}>
        <X />
      </Button>
    </div>
  )
}
