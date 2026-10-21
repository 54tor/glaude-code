import { Brain, Eye, EyeOff, FolderInput, Globe, Pencil, Save, ScrollText, Sparkles, Trash2, X } from "lucide-react"
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
  return (
    <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b px-6 py-2">
      <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={file?.path}>
        {file ? `${file.name} · ${size(file.kb)}` : sel}
        {file && loads && (loads.sessions ? ` · chargée dans ${loads.sessions} session${loads.sessions > 1 ? "s" : ""}, dernière ${ago(loads.last)}` : " · jamais chargée depuis l'activation du suivi")}
      </code>
      {editing ? (
        <>
          <Button variant="ghost" size="sm" onClick={onCancel}>
            <X /> Annuler
          </Button>
          {!dirty && <span className="text-xs text-muted-foreground">identique au fichier : rien à enregistrer</span>}
          <Button size="sm" onClick={onSave} disabled={!dirty}>
            <Save /> Enregistrer
          </Button>
        </>
      ) : (
        <>
          {movable(sel) && file && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm">
                  <FolderInput /> Déplacer vers…
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                {home(sel) !== "global" && (
                  <>
                    <DropdownMenuItem onSelect={() => onMove("global")}>
                      <Globe /> Règles globales (rules/)
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                  </>
                )}
                {toRules(sel) && <DropdownMenuLabel>Règles du projet</DropdownMenuLabel>}
                {toRules(sel) &&
                  targets.map((t) => (
                    <DropdownMenuItem key={"r" + t.dir} onSelect={() => onMove("r:" + t.dir)}>
                      <ScrollText /> <span className="truncate">{tilde(t.project, homeDir)}</span>
                    </DropdownMenuItem>
                  ))}
                {toRules(sel) && toMemory(sel) && <DropdownMenuSeparator />}
                {toMemory(sel) && <DropdownMenuLabel>Mémoire du projet</DropdownMenuLabel>}
                {toMemory(sel) &&
                  targets.map((t) => (
                    <DropdownMenuItem key={t.dir} onSelect={() => onMove(t.dir)}>
                      <Brain /> <span className="truncate">{tilde(t.project, homeDir)}</span>
                    </DropdownMenuItem>
                  ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {file && (sel.startsWith("m/") || sel.startsWith("g/rules/")) && !sel.endsWith("/MEMORY.md") && (
            <Choose
              value={file.type ?? ""}
              onChange={onType}
              options={[["", "Sans type"], ...TYPES.map((t) => [t.key, `${t.key} (${t.label})`] as const)]}
              label="Type de l'entrée"
              className="w-44"
            />
          )}
          {file && sel.startsWith("g/rules/") && (
            <Button variant="ghost" size="sm" onClick={onToggleRule}>
              {file.off ? <Eye /> : <EyeOff />} {file.off ? "Réactiver" : "Désactiver"}
            </Button>
          )}
          <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onRemove} disabled={!file}>
            <Trash2 /> Supprimer
          </Button>
          {file && (
            <Button variant="ghost" size="sm" onClick={onRewrite}>
              <Sparkles /> Haiku…
            </Button>
          )}
          <Button size="sm" onClick={onEdit} disabled={!textReady}>
            <Pencil /> Modifier
          </Button>
        </>
      )}
    </div>
  )
}
