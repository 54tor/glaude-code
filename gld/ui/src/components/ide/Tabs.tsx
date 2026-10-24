import { useEffect, useRef, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { X } from "lucide-react"
import { cn } from "cn"
import { isDirty, type Tab } from "./ideState"

const base = (p: string) => p.slice(p.lastIndexOf("/") + 1)

export function Tabs({ tabs, active, onFocus, onClose }: { tabs: Tab[]; active: string; onFocus(path: string): void; onClose(path: string): void }) {
  const { t } = useTranslation()
  const box = useRef<HTMLDivElement>(null)
  const kbd = useRef(false) // le focus ne suit l'onglet actif que si le changement vient du clavier
  useEffect(() => {
    if (kbd.current) box.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(active)}"]`)?.focus()
    kbd.current = false
  }, [active, tabs.length])
  // Activation automatique : la flèche choisit et affiche l'onglet. Suppr ferme l'onglet qui a le focus.
  const key = (e: KeyboardEvent, i: number) => {
    const go = (j: number) => onFocus(tabs[(j + tabs.length) % tabs.length].path)
    const acts: Record<string, () => void> = {
      ArrowLeft: () => go(i - 1),
      ArrowRight: () => go(i + 1),
      Home: () => go(0),
      End: () => go(tabs.length - 1),
      Enter: () => go(i),
      Delete: () => onClose(tabs[i].path),
    }
    if (!acts[e.key]) return
    e.preventDefault()
    kbd.current = true
    acts[e.key]()
  }
  return (
    <div ref={box} role="tablist" aria-label={t("ide.tabs.label")} className="flex min-h-9 shrink-0 overflow-x-auto border-b">
      {tabs.map((tab, i) => (
        <div
          key={tab.path}
          data-path={tab.path}
          role="tab"
          tabIndex={tab.path === active ? 0 : -1}
          aria-selected={tab.path === active}
          title={tab.path}
          onClick={() => onFocus(tab.path)}
          aria-keyshortcuts="Delete"
          onKeyDown={(e) => key(e, i)}
          onAuxClick={(e) => e.button === 1 && onClose(tab.path)}
          className={cn(
            "group flex max-w-56 shrink-0 cursor-default items-center gap-1.5 border-r pr-1 pl-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
            tab.path === active ? "bg-background text-foreground" : "bg-muted/40 text-muted-foreground hover:bg-muted"
          )}
        >
          <span className={cn("truncate", tab.readOnly && "italic")}>{base(tab.path)}</span>
          {isDirty(tab) && <span role="img" aria-label={t("ide.tabs.dirty")} className="size-2 shrink-0 rounded-full bg-foreground/70" />}
          <button
            tabIndex={-1}
            aria-label={t("ide.tabs.close", { path: tab.path })}
            className="grid size-5 shrink-0 place-items-center rounded-sm opacity-60 hover:bg-accent hover:opacity-100"
            onClick={(e) => (e.stopPropagation(), onClose(tab.path))}
          >
            <X className="size-3.5" />
          </button>
        </div>
      ))}
    </div>
  )
}
