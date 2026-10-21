import { BookOpen, Brain, ClipboardCopy, ScrollText, Copy, Eye, EyeOff, FolderInput, Globe, Link, Merge, Pencil, Sparkles, Trash2 } from "lucide-react"
import type { MemFile, MemTarget } from "@/lib/api"
import { tilde } from "@/lib/format"
import {
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@/components/ui/context-menu"
import { copy, home, movable, toMemory, toRules } from "./memoryLib"

export type FileActions = {
  open: (key: string) => void
  edit: (key: string) => void
  rewrite: (x: MemFile) => void // réécriture par Haiku
  copyContent: (x: MemFile) => void
  toggle: (key: string) => void // coche pour une fusion
  move: (key: string, to: string, copy?: boolean) => void
  toggleRule: (key: string) => void
  remove: (x: MemFile) => void
}

// Sous-menu « Déplacer vers » / « Copier vers » : les règles globales, puis la mémoire de chaque autre projet.
function Destinations({ x, targets, homeDir, label, onChoose }: { x: MemFile; targets: MemTarget[]; homeDir: string; label: string; onChoose: (to: string) => void }) {
  const Icon = label === "Copier vers" ? Copy : FolderInput
  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Icon /> {label}
      </ContextMenuSubTrigger>
      <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
        {home(x.key) !== "global" && (
          <>
            <ContextMenuItem onSelect={() => onChoose("global")}>
              <Globe /> Règles globales (rules/)
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        {toRules(x.key) && <ContextMenuLabel>Règles du projet</ContextMenuLabel>}
        {toRules(x.key) &&
          targets.map((t) => (
            <ContextMenuItem key={"r" + t.dir} onSelect={() => onChoose("r:" + t.dir)}>
              <ScrollText /> <span className="truncate">{tilde(t.project, homeDir)}</span>
            </ContextMenuItem>
          ))}
        {toRules(x.key) && toMemory(x.key) && <ContextMenuSeparator />}
        {toMemory(x.key) && <ContextMenuLabel>Mémoire du projet</ContextMenuLabel>}
        {toMemory(x.key) &&
          targets.map((t) => (
            <ContextMenuItem key={t.dir} onSelect={() => onChoose(t.dir)}>
              <Brain /> <span className="truncate">{tilde(t.project, homeDir)}</span>
            </ContextMenuItem>
          ))}
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}

// Clic droit sur un fichier de mémoire.
export function FileMenu({ x, picked, targets, homeDir, a }: { x: MemFile; picked: boolean; targets: MemTarget[]; homeDir: string; a: FileActions }) {
  return (
    <>
      <ContextMenuLabel className="truncate">{x.name}</ContextMenuLabel>
      <ContextMenuItem onSelect={() => a.open(x.key)}>
        <BookOpen /> Ouvrir
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => a.edit(x.key)}>
        <Pencil /> Modifier
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => a.rewrite(x)}>
        <Sparkles /> Réécrire avec Haiku…
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => a.copyContent(x)}>
        <ClipboardCopy /> Copier le contenu
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => copy(x.path, "Chemin")}>
        <Link /> Copier le chemin
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => a.toggle(x.key)}>
        <Merge /> {picked ? "Retirer de la fusion" : "Sélectionner pour une fusion"}
      </ContextMenuItem>
      {movable(x.key) && (
        <>
          <Destinations x={x} targets={targets} homeDir={homeDir} label="Déplacer vers" onChoose={(to) => a.move(x.key, to)} />
          <Destinations x={x} targets={targets} homeDir={homeDir} label="Copier vers" onChoose={(to) => a.move(x.key, to, true)} />
        </>
      )}
      {x.key.startsWith("g/rules/") && (
        <ContextMenuItem onSelect={() => a.toggleRule(x.key)}>
          {x.off ? <Eye /> : <EyeOff />} {x.off ? "Réactiver la règle" : "Désactiver la règle"}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => a.remove(x)}>
        <Trash2 /> Supprimer
      </ContextMenuItem>
    </>
  )
}
