import { CircleHelp, Copy, HeartPulse, Merge, Sparkles, Trash2, X } from "lucide-react"
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
  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-1 px-1">
        <Button
          variant={issues ? "outline" : "ghost"}
          size="xs"
          onClick={onHealth}
          title="Index MEMORY.md, imports et liens cassés, front matter, entrées anciennes"
        >
          <HeartPulse className={issues ? "text-amber-500" : ""} /> Santé{issues > 0 && ` (${issues})`}
        </Button>
        {TYPES.map((t) => (
          <Button
            key={t.key}
            variant={typeFilter === t.key ? "secondary" : "ghost"}
            size="xs"
            onClick={() => onType(typeFilter === t.key ? "" : t.key)}
            aria-pressed={typeFilter === t.key}
            title={`Seulement les entrées de type ${t.key} (${t.label})`}
          >
            <t.icon className={t.color} /> {all.filter((x) => x.type === t.key).length}
          </Button>
        ))}
        <Button variant="ghost" size="icon-xs" onClick={onTypeHelp} title="À quoi servent les types de mémoire ?" aria-label="Aide sur les types">
          <CircleHelp />
        </Button>
      </div>
      {orphanProjects > 0 && (
        <Button
          variant="outline"
          size="xs"
          className="mb-2 w-full"
          onClick={onCleanOrphans}
          title="Dossiers de projet disparus : leurs fichiers de mémoire vont à la corbeille"
        >
          <Trash2 /> Nettoyer les projets introuvables ({orphanProjects})
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
  return (
    <div className="flex flex-wrap items-center gap-1 border-b bg-popover px-3 py-2 text-xs md:absolute md:bottom-4 md:left-2 md:w-[19rem] md:rounded-xl md:border md:shadow-lg">
      <span className="mr-auto font-medium">
        {count} fichier{count > 1 ? "s" : ""}
      </span>
      <Button variant="ghost" size="xs" onClick={onPrompt} disabled={count < 2} title="Copier le prompt de fusion">
        <Copy /> Prompt
      </Button>
      <Button variant="ghost" size="xs" onClick={onHaiku} title="Nouvelle entrée rédigée par Haiku à partir de ces fichiers">
        <Sparkles /> Haiku
      </Button>
      <Button variant="ghost" size="xs" onClick={onRemove} title="Mettre ces fichiers à la corbeille">
        <Trash2 /> Supprimer
      </Button>
      <Button size="xs" onClick={onMerge} disabled={count < 2} title="Nouvelle session Claude qui fusionne ces fichiers">
        <Merge /> Fusionner avec Claude
      </Button>
      <Button variant="ghost" size="icon-xs" onClick={onClear} aria-label="Vider la sélection">
        <X />
      </Button>
    </div>
  )
}
