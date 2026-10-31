import { useEffect, useId, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Menubar } from "radix-ui"
import { CheckIcon } from "lucide-react"
import { isEditableTarget } from "@/lib/editable"
import { ariaKeys, commands, showKeys, type CmdCtx, type Command } from "./commands"
import type { ViewPrefValues } from "./useViewPrefs"

const GROUPS: Command["group"][] = ["file", "edit", "find", "view", "goto"]
type Toggle = { [K in keyof ViewPrefValues]: ViewPrefValues[K] extends boolean ? K : never }[keyof ViewPrefValues]
const CHECKS: Record<string, Toggle> = {
  "view.wordWrap": "wordWrap",
  "view.minimap": "minimap",
  "view.toggleTree": "showTree",
  "view.togglePreview": "showPreview",
  "view.toggleStatus": "showStatus",
}
const itemCls =
  "relative flex cursor-default items-center gap-2 rounded-md py-1 pr-2 pl-7 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground aria-disabled:opacity-60"

// aria-disabled plutôt que disabled : l'entrée reste atteignable au clavier pour lire la raison du refus.
// Focus donné à l'éditeur AVANT la commande : celles qui ouvrent un widget de Monaco (recherche, ligne, palette) le reprennent ensuite.
function Entry({ command, ctx, onRan }: { command: Command; ctx: CmdCtx; onRan(): void }) {
  const { t } = useTranslation()
  const id = useId()
  const off = !command.enabled(ctx)
  const check = CHECKS[command.id]
  const props = {
    "data-cmd": command.id,
    "aria-disabled": off || undefined,
    "aria-describedby": off ? id : undefined,
    "aria-keyshortcuts": ariaKeys(command.shortcut),
    className: itemCls,
    onSelect: (event: Event) => {
      if (off) return event.preventDefault()
      if (ctx.editor && command.focus === "editor") {
        onRan()
        ctx.editor.focus()
      }
      command.run(ctx)
    },
  }
  const body = (
    <>
      <span className="flex min-w-0 flex-1 flex-col">
        <span>{t(command.label)}</span>
        {off && (
          <span id={id} aria-hidden className="text-xs text-muted-foreground">
            {t(command.reason?.(ctx) ?? "")}
          </span>
        )}
      </span>
      {command.shortcut && (
        <kbd aria-hidden className="ml-4 text-xs text-muted-foreground">
          {showKeys(command.shortcut, t)}
        </kbd>
      )}
    </>
  )
  if (!check) return <Menubar.Item {...props}>{body}</Menubar.Item>
  return (
    <Menubar.CheckboxItem checked={ctx.prefs[check]} {...props}>
      <Menubar.ItemIndicator className="absolute left-2 flex">
        <CheckIcon className="size-4" />
      </Menubar.ItemIndicator>
      {body}
    </Menubar.CheckboxItem>
  )
}

// Entrées tirées du registre par groupe : un menu ne peut pas proposer une action que les barres ignorent.
// Alt+lettre ouvre un menu, sauf dans une cible de saisie (Monaco, xterm, champs) ou une modale (le menu s'ouvrirait derrière) ; F1 reste à Monaco.
export function MenuBar({ ctx }: { ctx: CmdCtx }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState("")
  // Une commande a rendu le focus à l'éditeur : Radix ne doit pas le remettre sur le menu en se fermant.
  const ran = useRef(false)
  const onRan = () => void (ran.current = true)
  const closeFocus = (event: Event) => {
    if (ran.current) event.preventDefault()
    ran.current = false
  }
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const isAltOnly = event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey
      if (!isAltOnly || event.defaultPrevented || isEditableTarget(event.target)) return
      const insideDialog = event.target instanceof Element && event.target.closest("[role=dialog], [role=alertdialog]")
      if (insideDialog) return
      const group = GROUPS.find((candidate) => t(`ide.menubar.key.${candidate}`) === event.key.toLowerCase())
      if (!group) return
      event.preventDefault()
      setOpen(group)
    }
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [t])
  return (
    <Menubar.Root value={open} onValueChange={setOpen} aria-label={t("ide.menubar.label")} className="flex shrink-0 flex-wrap items-center gap-0.5 border-b px-1 py-0.5">
      {GROUPS.map((group) => (
        <Menubar.Menu key={group} value={group}>
          <Menubar.Trigger
            aria-keyshortcuts={`Alt+${t(`ide.menubar.key.${group}`).toUpperCase()}`}
            className="rounded-md px-2 py-0.5 text-sm outline-hidden select-none focus:bg-accent data-[state=open]:bg-accent"
          >
            {t(`ide.menubar.${group}`)}
          </Menubar.Trigger>
          <Menubar.Portal>
            <Menubar.Content align="start" sideOffset={4} onCloseAutoFocus={closeFocus} className="z-50 min-w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md">
              {commands
                .filter((command) => command.group === group)
                .map((command) => (
                  <Entry key={command.id} command={command} ctx={ctx} onRan={onRan} />
                ))}
            </Menubar.Content>
          </Menubar.Portal>
        </Menubar.Menu>
      ))}
    </Menubar.Root>
  )
}
