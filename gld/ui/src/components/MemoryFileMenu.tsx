import { BookOpen, BotMessageSquare, Download, Zap, ZapOff, Brain, Copy, Eye, EyeOff, FolderInput, Globe, Merge, Pencil, ScrollText, Tag, TextCursorInput, Trash2, Unlink } from "lucide-react"
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
  rename: (x: MemFile) => void
  copyContent: (x: MemFile) => void
  toggle: (key: string) => void // coche pour une fusion
  move: (key: string, to: string, copy?: boolean) => void
  toggleRule: (key: string) => void
  setType: (key: string, type: string) => void
  remove: (x: MemFile) => void
  exportOne: (x: MemFile) => void
}

const MEMORY_INDEX_SUFFIX = "/MEMORY.md"
const MEMORY_PREFIX = "m/"
const PROJECT_RULES_PREFIX = "r/"
const GLOBAL_RULES_PREFIX = "g/rules/"
const PROJECT_RULES_DESTINATION_PREFIX = "r:"

// Sous-menu « Déplacer vers » / « Copier vers » : les règles globales, puis la mémoire de chaque autre projet.
function Destinations({ file, targets, homeDir, copying, onChoose }: { file: MemFile; targets: MemTarget[]; homeDir: string; copying?: boolean; onChoose: (to: string) => void }) {
  const { t } = useTranslation()
  const Icon = copying ? Copy : FolderInput
  const isGlobal = home(file.key) === "global"
  const canGoToRules = toRules(file.key)
  const canGoToMemory = toMemory(file.key)
  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Icon /> {copying ? t("memory.fileMenu.copyTo") : t("memory.fileMenu.moveTo")}
      </ContextMenuSubTrigger>
      <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
        {!isGlobal && (
          <>
            <ContextMenuItem onSelect={() => onChoose("global")}>
              <Globe /> {t("memory.fileMenu.globalRules")}
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        {canGoToRules && <ContextMenuLabel>{t("memory.fileMenu.projectRules")}</ContextMenuLabel>}
        {canGoToRules &&
          targets.map((target) => (
            <ContextMenuItem key={"r" + target.dir} onSelect={() => onChoose(PROJECT_RULES_DESTINATION_PREFIX + target.dir)}>
              <ScrollText /> <span className="truncate">{tilde(target.project, homeDir)}</span>
            </ContextMenuItem>
          ))}
        {canGoToRules && canGoToMemory && <ContextMenuSeparator />}
        {canGoToMemory && <ContextMenuLabel>{t("memory.fileMenu.projectMemory")}</ContextMenuLabel>}
        {canGoToMemory &&
          targets.map((target) => (
            <ContextMenuItem key={target.dir} onSelect={() => onChoose(target.dir)}>
              <Brain /> <span className="truncate">{tilde(target.project, homeDir)}</span>
            </ContextMenuItem>
          ))}
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}

// Clic droit sur un fichier de mémoire.
export function FileMenu({ x: file, picked, targets, homeDir, a: actions }: { x: MemFile; picked: boolean; targets: MemTarget[]; homeDir: string; a: FileActions }) {
  const { t } = useTranslation()
  const isIndex = file.key.endsWith(MEMORY_INDEX_SUFFIX)
  const isMemory = file.key.startsWith(MEMORY_PREFIX)
  const isProjectRule = file.key.startsWith(PROJECT_RULES_PREFIX)
  const isGlobalRule = file.key.startsWith(GLOBAL_RULES_PREFIX)
  const canRename = !isIndex && (isMemory || isGlobalRule || isProjectRule)
  const canSetType = (isMemory || isGlobalRule) && !isIndex
  const canToggleLoading = (isMemory || isProjectRule) && !isIndex && !file.paths?.length
  const canExport = isMemory && !isIndex
  const toggleLoadingDestination = isMemory ? PROJECT_RULES_DESTINATION_PREFIX + home(file.key) : home(file.key)
  return (
    <>
      <ContextMenuLabel className="truncate">{file.name}</ContextMenuLabel>
      <ContextMenuItem onSelect={() => actions.open(file.key)}>
        <BookOpen /> {t("memory.fileMenu.open")}
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => actions.copyContent(file)}>
        <Copy /> {t("memory.fileMenu.copyContent")}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => copy(file.path, "memory.lib.copiedPath")}>
        <Copy /> {t("memory.fileMenu.copyPath")}
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => actions.edit(file.key)}>
        <Pencil /> {t("memory.fileMenu.edit")}
      </ContextMenuItem>
      {canRename && (
        <ContextMenuItem onSelect={() => actions.rename(file)}>
          <TextCursorInput /> {t("memory.fileMenu.rename")}
        </ContextMenuItem>
      )}
      <ContextMenuItem onSelect={() => actions.rewrite(file)}>
        <BotMessageSquare /> {t("memory.fileMenu.improve")}
      </ContextMenuItem>
      {canSetType && (
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Tag /> {t("memory.fileMenu.type")}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            {TYPES.map((memoryType) => (
              <ContextMenuItem key={memoryType.key} onSelect={() => actions.setType(file.key, memoryType.key)}>
                <memoryType.icon /> {memoryType.key} <span className="text-xs text-muted-foreground">({memoryType.label}){file.type === memoryType.key && " ✓"}</span>
              </ContextMenuItem>
            ))}
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => actions.setType(file.key, "")}>{t("memory.fileMenu.noType")}{!file.type && " ✓"}</ContextMenuItem>
          </ContextMenuSubContent>
        </ContextMenuSub>
      )}
      <ContextMenuItem onSelect={() => actions.toggle(file.key)}>
        {picked ? <Unlink /> : <Merge />} {picked ? t("memory.fileMenu.unmerge") : t("memory.fileMenu.merge")}
      </ContextMenuItem>
      {movable(file.key) && (
        <>
          <Destinations file={file} targets={targets} homeDir={homeDir} onChoose={(destination) => actions.move(file.key, destination)} />
          <Destinations file={file} targets={targets} homeDir={homeDir} copying onChoose={(destination) => actions.move(file.key, destination, true)} />
        </>
      )}
      {canToggleLoading && (
        <ContextMenuItem onSelect={() => actions.move(file.key, toggleLoadingDestination)}>
          {isMemory ? <Zap /> : <ZapOff />} {isMemory ? t("memory.fileMenu.loadAlways") : t("memory.fileMenu.loadOnDemand")}
        </ContextMenuItem>
      )}
      {isGlobalRule && (
        <ContextMenuItem onSelect={() => actions.toggleRule(file.key)}>
          {file.off ? <Eye /> : <EyeOff />} {file.off ? t("memory.fileMenu.reactivateRule") : t("memory.fileMenu.deactivateRule")}
        </ContextMenuItem>
      )}
      {canExport && (
        <ContextMenuItem onSelect={() => actions.exportOne(file)}>
          <Download /> {t("memory.pack.export")}
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => actions.remove(file)}>
        <Trash2 /> {t("memory.fileMenu.remove")}
      </ContextMenuItem>
    </>
  )
}
