import { useId, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Toolbar } from "radix-ui"
import { cn } from "cn"
import { buttonVariants } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { ariaKeys, commands, showKeys, type CmdCtx } from "./commands"

type Props = { label: string; shortcut?: string; reason?: string; pressed?: boolean; onClick(): void; children: ReactNode }

// aria-disabled plutôt que disabled : le bouton reste atteignable au clavier et au survol, pour lire la raison du refus.
export function ToolButton({ label, shortcut, reason, pressed, onClick, children }: Props) {
  const { t } = useTranslation()
  const id = useId()
  const off = !!reason
  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <Toolbar.Button
            aria-label={label}
            aria-disabled={off || undefined}
            aria-pressed={pressed}
            aria-keyshortcuts={ariaKeys(shortcut)}
            aria-describedby={off ? id : undefined}
            className={cn(buttonVariants({ variant: "ghost", size: "icon-sm" }), off && "opacity-50", pressed && "bg-muted")}
            onClick={() => !off && onClick()}
          >
            {children}
          </Toolbar.Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          <span>{label}</span>
          {shortcut && <kbd data-slot="kbd" className="text-[10px] opacity-70">{showKeys(shortcut, t)}</kbd>}
          {off && <span className="opacity-80">({reason})</span>}
        </TooltipContent>
      </Tooltip>
      {off && (
        <span id={id} hidden>
          {reason}
        </span>
      )}
    </>
  )
}

// Même ordre que le menu : focus d'abord, la commande ensuite ; sans éditeur monté (ctx.editor null), aucun focus.
const run = (command: (typeof commands)[number], ctx: CmdCtx) => {
  if (command.focus === "editor") ctx.editor?.focus()
  command.run(ctx)
}

// Bouton d'une commande du registre : libellé, raccourci, raison et action viennent tous du registre.
export function CmdButton({ id, ctx, pressed, children }: { id: string; ctx: CmdCtx; pressed?: boolean; children: ReactNode }) {
  const { t } = useTranslation()
  const command = commands.find((candidate) => candidate.id === id)
  if (!command) return null
  const reason = command.enabled(ctx) ? "" : t(command.reason?.(ctx) ?? "")
  return (
    <ToolButton label={t(command.label)} shortcut={command.shortcut} reason={reason} pressed={pressed} onClick={() => run(command, ctx)}>
      {children}
    </ToolButton>
  )
}
