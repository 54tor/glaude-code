import { useEffect, useRef, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { X } from "lucide-react"
import { cn } from "cn"
import { isDirty, type Tab } from "./ideState"

const MIDDLE_BUTTON = 1
const base = (path: string) => path.slice(path.lastIndexOf("/") + 1)

export function Tabs({ tabs, active, onFocus, onClose }: { tabs: Tab[]; active: string; onFocus(path: string): void; onClose(path: string): void }) {
  const { t } = useTranslation()
  const box = useRef<HTMLDivElement>(null)
  const fromKeyboard = useRef(false) // le focus ne suit l'onglet actif que si le changement vient du clavier
  useEffect(() => {
    if (fromKeyboard.current) box.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(active)}"]`)?.focus()
    fromKeyboard.current = false
  }, [active, tabs.length])
  // Activation automatique : la flèche choisit et affiche l'onglet. Suppr ferme l'onglet qui a le focus.
  const onKeyDown = (event: KeyboardEvent, tabIndex: number) => {
    const goTo = (targetIndex: number) => onFocus(tabs[(targetIndex + tabs.length) % tabs.length].path)
    const actions: Record<string, () => void> = {
      ArrowLeft: () => goTo(tabIndex - 1),
      ArrowRight: () => goTo(tabIndex + 1),
      Home: () => goTo(0),
      End: () => goTo(tabs.length - 1),
      Enter: () => goTo(tabIndex),
      Delete: () => onClose(tabs[tabIndex].path),
    }
    if (!actions[event.key]) return
    event.preventDefault()
    fromKeyboard.current = true
    actions[event.key]()
  }
  return (
    <div ref={box} role="tablist" aria-label={t("ide.tabs.label")} className="flex min-h-9 shrink-0 overflow-x-auto border-b">
      {tabs.map((tab, tabIndex) => (
        <div
          key={tab.path}
          data-path={tab.path}
          role="tab"
          tabIndex={tab.path === active ? 0 : -1}
          aria-selected={tab.path === active}
          title={tab.path}
          onClick={() => onFocus(tab.path)}
          aria-keyshortcuts="Delete"
          onKeyDown={(event) => onKeyDown(event, tabIndex)}
          onAuxClick={(event) => event.button === MIDDLE_BUTTON && onClose(tab.path)}
          className={cn(
            "group flex max-w-56 shrink-0 cursor-default items-center gap-1.5 border-r pr-1 pl-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/80",
            tab.path === active ? "bg-background text-foreground" : "bg-muted/40 text-muted-foreground hover:bg-muted"
          )}
        >
          <span className={cn("truncate", tab.readOnly && "italic")}>{base(tab.path)}</span>
          {isDirty(tab) && <span role="img" aria-label={t("ide.tabs.dirty")} className="size-2 shrink-0 rounded-full bg-foreground/70" />}
          <button
            tabIndex={-1}
            aria-label={t("ide.tabs.close", { path: tab.path })}
            className="grid size-5 shrink-0 place-items-center rounded-sm opacity-60 hover:bg-accent hover:opacity-100"
            onClick={(event) => {
              event.stopPropagation()
              onClose(tab.path)
            }}
          >
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  )
}
