import { BookOpen, Bot, Brain, ClipboardCopy, Copy, Eye, EyeOff, FolderInput, Globe, Link, Merge, Pencil, ScrollText, Tag, Trash2 } from "lucide-react"
import { useTranslation } from "react-i18next"
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
import { TYPES } from "./MemoryTypes"

export type FileActions = {
  open: (key: string) => void
  edit: (key: string) => void
  rewrite: (x: MemFile) => void // réécriture par Haiku
  copyContent: (x: MemFile) => void
  toggle: (key: string) => void // coche pour une fusion
  move: (key: string, to: string, copy?: boolean) => void
  toggleRule: (key: string) => void
  setType: (key: string, type: string) => void
  remove: (x: MemFile) => void
}

// Sous-menu « Déplacer vers » / « Copier vers » : les règles globales, puis la mémoire de chaque autre projet.
function Destinations({ x, targets, homeDir, copying, onChoose }: { x: MemFile; targets: MemTarget[]; homeDir: string; copying?: boolean; onChoose: (to: string) => void }) {
  const { t } = useTranslation()
  const Icon = copying ? Copy : FolderInput
  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Icon /> {copying ? t("memory.fileMenu.copyTo") : t("memory.fileMenu.moveTo")}
      </ContextMenuSubTrigger>
      <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
        {home(x.key) !== "global" && (
          <>
            <ContextMenuItem onSelect={() => onChoose("global")}>
              <Globe /> {t("memory.fileMenu.globalRules")}
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        {toRules(x.key) && <ContextMenuLabel>{t("memory.fileMenu.projectRules")}</ContextMenuLabel>}
        {toRules(x.key) &&
          targets.map((t) => (
            <ContextMenuItem key={"r" + t.dir} onSelect={() => onChoose("r:" + t.dir)}>
              <ScrollText /> <span className="truncate">{tilde(t.project, homeDir)}</span>
            </ContextMenuItem>
          ))}
        {toRules(x.key) && toMemory(x.key) && <ContextMenuSeparator />}
        {toMemory(x.key) && <ContextMenuLabel>{t("memory.fileMenu.projectMemory")}</ContextMenuLabel>}
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
  const { t } = useTranslation()
  return (
    <>
      <ContextMenuLabel className="truncate">{x.name}</ContextMenuLabel>
      <ContextMenuItem onSelect={() => a.open(x.key)}>
        <BookOpen /> {t("memory.fileMenu.open")}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => a.edit(x.key)}>
        <Pencil /> {t("memory.fileMenu.edit")}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => a.rewrite(x)}>
        <Bot /> {t("memory.fileMenu.improve")}
      </ContextMenuItem>
      {(x.key.startsWith("m/") || x.key.startsWith("g/rules/")) && !x.key.endsWith("/MEMORY.md") && (
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Tag /> {t("memory.fileMenu.type")}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            {TYPES.map((ty) => (
              <ContextMenuItem key={ty.key} onSelect={() => a.setType(x.key, ty.key)}>
                <ty.icon /> {ty.key} <span className="text-xs text-muted-foreground">({ty.label}){x.type === ty.key && " ✓"}</span>
              </ContextMenuItem>
            ))}
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => a.setType(x.key, "")}>{t("memory.fileMenu.noType")}{!x.type && " ✓"}</ContextMenuItem>
          </ContextMenuSubContent>
        </ContextMenuSub>
      )}
      <ContextMenuItem onSelect={() => a.copyContent(x)}>
        <ClipboardCopy /> {t("memory.fileMenu.copyContent")}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => copy(x.path, "memory.lib.copiedPath")}>
        <Link /> {t("memory.fileMenu.copyPath")}
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => a.toggle(x.key)}>
        <Merge /> {picked ? t("memory.fileMenu.unmerge") : t("memory.fileMenu.merge")}
      </ContextMenuItem>
      {movable(x.key) && (
        <>
          <Destinations x={x} targets={targets} homeDir={homeDir} onChoose={(to) => a.move(x.key, to)} />
          <Destinations x={x} targets={targets} homeDir={homeDir} copying onChoose={(to) => a.move(x.key, to, true)} />
        </>
      )}
      {x.key.startsWith("g/rules/") && (
        <ContextMenuItem onSelect={() => a.toggleRule(x.key)}>
          {x.off ? <Eye /> : <EyeOff />} {x.off ? t("memory.fileMenu.reactivateRule") : t("memory.fileMenu.deactivateRule")}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => a.remove(x)}>
        <Trash2 /> {t("memory.fileMenu.remove")}
      </ContextMenuItem>
    </>
  )
}
