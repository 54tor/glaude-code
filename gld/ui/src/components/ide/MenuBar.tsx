import { useEffect, useId, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Menubar } from "radix-ui"
import { CheckIcon } from "lucide-react"
import { isEditableTarget } from "@/lib/editable"
import { ariaKeys, commands, showKeys, type CmdCtx, type Command } from "./commands"
import type { ViewPrefValues } from "./useViewPrefs"

const GROUPS: Command["group"][] = ["file", "edit", "find", "view", "goto", "terminal"]
type Toggle = { [K in keyof ViewPrefValues]: ViewPrefValues[K] extends boolean ? K : never }[keyof ViewPrefValues]
const CHECKS: Record<string, Toggle> = {
  "view.wordWrap": "wordWrap",
  "view.minimap": "minimap",
  "view.toggleTree": "showTree",
  "view.toggleTerminal": "showTerminal",
  "view.toggleStatus": "showStatus",
}
const itemCls =
  "relative flex cursor-default items-center gap-2 rounded-md py-1 pr-2 pl-7 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground aria-disabled:opacity-60"

// aria-disabled plutôt que disabled : l'entrée reste atteignable au clavier pour lire la raison du refus.
// Focus donné à l'éditeur AVANT la commande : celles qui ouvrent un widget de Monaco (recherche, ligne, palette) le reprennent ensuite.
function Entry({ c, ctx, onRan }: { c: Command; ctx: CmdCtx; onRan(): void }) {
  const { t } = useTranslation()
  const id = useId()
  const off = !c.enabled(ctx)
  const check = CHECKS[c.id]
  const props = {
    "data-cmd": c.id,
    "aria-disabled": off || undefined,
    "aria-describedby": off ? id : undefined,
    "aria-keyshortcuts": ariaKeys(c.shortcut),
    className: itemCls,
    onSelect: (e: Event) => {
      if (off) return e.preventDefault()
      if (ctx.editor && c.focus === "editor") {
        onRan()
        ctx.editor.focus()
      }
      c.run(ctx)
    },
  }
  const body = (
    <>
      <span className="flex min-w-0 flex-1 flex-col">
        <span>{t(c.label)}</span>
        {off && (
          <span id={id} aria-hidden className="text-xs text-muted-foreground">
            {t(c.reason?.(ctx) ?? "")}
          </span>
        )}
      </span>
      {c.shortcut && (
        <kbd aria-hidden className="ml-4 text-xs text-muted-foreground">
          {showKeys(c.shortcut, t)}
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
// Alt+lettre ouvre un menu, sauf dans une cible de saisie (Monaco, xterm, champs) ; F1 reste à Monaco.
export function MenuBar({ ctx }: { ctx: CmdCtx }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState("")
  // Une commande a rendu le focus à l'éditeur : Radix ne doit pas le remettre sur le menu en se fermant.
  const ran = useRef(false)
  const onRan = () => void (ran.current = true)
  const closeFocus = (e: Event) => {
    if (ran.current) e.preventDefault()
    ran.current = false
  }
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || e.defaultPrevented || isEditableTarget(e.target)) return
      const g = GROUPS.find((x) => t(`ide.menubar.key.${x}`) === e.key.toLowerCase())
      if (!g) return
      e.preventDefault()
      setOpen(g)
    }
    document.addEventListener("keydown", k)
    return () => document.removeEventListener("keydown", k)
  }, [t])
  return (
    <Menubar.Root value={open} onValueChange={setOpen} aria-label={t("ide.menubar.label")} className="flex shrink-0 flex-wrap items-center gap-0.5 border-b px-1 py-0.5">
      {GROUPS.map((g) => (
        <Menubar.Menu key={g} value={g}>
          <Menubar.Trigger
            aria-keyshortcuts={`Alt+${t(`ide.menubar.key.${g}`).toUpperCase()}`}
            className="rounded-md px-2 py-0.5 text-sm outline-hidden select-none focus:bg-accent data-[state=open]:bg-accent"
          >
            {t(`ide.menubar.${g}`)}
          </Menubar.Trigger>
          <Menubar.Portal>
            <Menubar.Content align="start" sideOffset={4} onCloseAutoFocus={closeFocus} className="z-50 min-w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md">
              {commands
                .filter((c) => c.group === g)
                .map((c) => (
                  <Entry key={c.id} c={c} ctx={ctx} onRan={onRan} />
                ))}
            </Menubar.Content>
          </Menubar.Portal>
        </Menubar.Menu>
      ))}
    </Menubar.Root>
  )
}
