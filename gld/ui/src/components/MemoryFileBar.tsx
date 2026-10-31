import { BotMessageSquare, Brain, Eye, EyeOff, FolderInput, Globe, Pencil, Save, ScrollText, Trash2, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { MemFile, MemTarget } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Choose } from "./Choose"
import { home, movable, toMemory, toRules } from "./memoryLib"
import { TYPES } from "./MemoryTypes"

// Barre d'actions au-dessus du fichier affiché : édition (annuler, enregistrer) ou déplacer, type, désactiver, supprimer, Haiku, modifier.
export function FileBar({
  sel,
  file,
  editing,
  dirty,
  textReady,
  targets,
  homeDir,
  onCancel,
  onSave,
  onEdit,
  onMove,
  onType,
  onToggleRule,
  onRemove,
  onRewrite,
  loads,
}: {
  sel: string
  file: MemFile | undefined
  editing: boolean
  dirty: boolean // le brouillon diffère du fichier
  textReady: boolean // le texte du fichier est chargé
  targets: MemTarget[] // projets de destination (hors celui du fichier)
  homeDir: string
  onCancel: () => void
  onSave: () => void
  onEdit: () => void
  onMove: (to: string) => void
  onType: (type: string) => void
  onToggleRule: () => void
  onRemove: () => void
  onRewrite: () => void
  loads?: { sessions: number; last: number } // chargements réels, si le suivi est installé
}) {
  const { t } = useTranslation()
  const isGldReadOnly = sel.startsWith("g/gldrules/")
  const hasTypeChoice = (sel.startsWith("m/") || sel.startsWith("g/rules/")) && !sel.endsWith("/MEMORY.md")
  const loadsLabel = loads?.sessions
    ? t("memory.fileBar.loadedIn", { count: loads.sessions, ago: ago(loads.last) })
    : t("memory.fileBar.neverLoaded")
  return (
    <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b px-6 py-2">
      <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={file?.path}>
        {file ? `${file.name} · ${size(file.kb)}` : sel}
        {file && loads && loadsLabel}
      </code>
      {editing && (
        <>
          <Button variant="ghost" size="sm" onClick={onCancel}>
            <X /> {t("memory.fileBar.cancel")}
          </Button>
          {!dirty && <span className="text-xs text-muted-foreground">{t("memory.fileBar.identical")}</span>}
          <Button size="sm" onClick={onSave} disabled={!dirty}>
            <Save /> {t("memory.fileBar.save")}
          </Button>
        </>
      )}
      {!editing && isGldReadOnly && <span className="text-xs text-muted-foreground">{t("memory.fileBar.gldReadOnly")}</span>}
      {!editing && !isGldReadOnly && (
        <>
          {movable(sel) && file && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm">
                  <FolderInput /> {t("memory.fileBar.moveTo")}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                {home(sel) !== "global" && (
                  <>
                    <DropdownMenuItem onSelect={() => onMove("global")}>
                      <Globe /> {t("memory.fileBar.globalRules")}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                  </>
                )}
                {toRules(sel) && <DropdownMenuLabel>{t("memory.fileBar.projectRules")}</DropdownMenuLabel>}
                {toRules(sel) &&
                  targets.map((target) => (
                    <DropdownMenuItem key={"r" + target.dir} onSelect={() => onMove("r:" + target.dir)}>
                      <ScrollText /> <span className="truncate">{tilde(target.project, homeDir)}</span>
                    </DropdownMenuItem>
                  ))}
                {toRules(sel) && toMemory(sel) && <DropdownMenuSeparator />}
                {toMemory(sel) && <DropdownMenuLabel>{t("memory.fileBar.projectMemory")}</DropdownMenuLabel>}
                {toMemory(sel) &&
                  targets.map((target) => (
                    <DropdownMenuItem key={target.dir} onSelect={() => onMove(target.dir)}>
                      <Brain /> <span className="truncate">{tilde(target.project, homeDir)}</span>
                    </DropdownMenuItem>
                  ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {file && hasTypeChoice && (
            <Choose
              value={file.type ?? ""}
              onChange={onType}
              options={[["", t("memory.fileBar.noType")], ...TYPES.map((memoryType) => [memoryType.key, `${memoryType.key} (${memoryType.label})`] as const)]}
              label={t("memory.fileBar.typeAria")}
              size="sm"
              className="w-44"
            />
          )}
          {file && sel.startsWith("g/rules/") && (
            <Button variant="ghost" size="sm" onClick={onToggleRule}>
              {file.off ? <Eye /> : <EyeOff />} {file.off ? t("memory.fileBar.reactivate") : t("memory.fileBar.deactivate")}
            </Button>
          )}
          <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onRemove} disabled={!file}>
            <Trash2 /> {t("memory.fileBar.remove")}
          </Button>
          {file && (
            <Button variant="ghost" size="sm" onClick={onRewrite}>
              <BotMessageSquare /> {t("memory.fileBar.improve")}
            </Button>
          )}
          <Button size="sm" onClick={onEdit} disabled={!textReady}>
            <Pencil /> {t("memory.fileBar.edit")}
          </Button>
        </>
      )}
    </div>
  )
}
