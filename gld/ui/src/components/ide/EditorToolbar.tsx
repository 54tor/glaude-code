import { useTranslation } from "react-i18next"
import { AArrowDown, AArrowUp, Command, CopyX, ListOrdered, PanelLeft, Redo2, Replace, Save, SaveAll, Search, SquareTerminal, Terminal, Undo2, WrapText } from "lucide-react"
import { Toolbar } from "radix-ui"
import { TooltipProvider } from "@/components/ui/tooltip"
import { CmdButton } from "./ToolButton"
import type { CmdCtx } from "./commands"

// Tout passe par le contexte des commandes : la poignée de l'éditeur actif (ctx.editor), l'onglet et les droits.
export function EditorToolbar({ ctx }: { ctx: CmdCtx }) {
  const { t } = useTranslation()
  const sep = <Toolbar.Separator className="mx-1 h-4 w-px bg-border" />
  return (
    <TooltipProvider>
      <Toolbar.Root aria-label={t("ide.toolbar.editor")} className="flex shrink-0 items-center gap-0.5 overflow-x-auto border-b px-1 py-0.5">
        <CmdButton id="file.save" ctx={ctx}>
          <Save />
        </CmdButton>
        <CmdButton id="file.saveAll" ctx={ctx}>
          <SaveAll />
        </CmdButton>
        {sep}
        <CmdButton id="edit.undo" ctx={ctx}>
          <Undo2 />
        </CmdButton>
        <CmdButton id="edit.redo" ctx={ctx}>
          <Redo2 />
        </CmdButton>
        {sep}
        <CmdButton id="find.find" ctx={ctx}>
          <Search />
        </CmdButton>
        <CmdButton id="find.replace" ctx={ctx}>
          <Replace />
        </CmdButton>
        <CmdButton id="goto.line" ctx={ctx}>
          <ListOrdered />
        </CmdButton>
        <CmdButton id="goto.palette" ctx={ctx}>
          <Command />
        </CmdButton>
        {sep}
        <CmdButton id="view.wordWrap" ctx={ctx} pressed={ctx.prefs.wordWrap}>
          <WrapText />
        </CmdButton>
        <CmdButton id="view.fontMinus" ctx={ctx}>
          <AArrowDown />
        </CmdButton>
        <CmdButton id="view.fontPlus" ctx={ctx}>
          <AArrowUp />
        </CmdButton>
        <CmdButton id="view.toggleTree" ctx={ctx} pressed={ctx.prefs.showTree}>
          <PanelLeft />
        </CmdButton>
        <span className="flex-1" />
        <CmdButton id="terminal.focusSession" ctx={ctx}>
          <Terminal />
        </CmdButton>
        <CmdButton id="terminal.toggle" ctx={ctx} pressed={ctx.prefs.showTerminal}>
          <SquareTerminal />
        </CmdButton>
        <CmdButton id="file.closeAll" ctx={ctx}>
          <CopyX />
        </CmdButton>
      </Toolbar.Root>
    </TooltipProvider>
  )
}
