import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { PanelBottom } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocal } from "@/hooks/useLocal"
import { Handle } from "./Handle"
import { clamp } from "./ideHelpers"
import { TerminalContent } from "./TerminalPane"
import { useTerminalAvailability, type TermState } from "./useTerminalAvailability"
import type { ViewPrefs } from "./useViewPrefs"

export type BottomView = { open: boolean; state: TermState }

const DEFAULT_HEIGHT = 160
const MIN_HEIGHT = 60
const MAX_HEIGHT = 600
const clampHeight = (height: number) => clamp(height, MIN_HEIGHT, MAX_HEIGHT)
const REASONS: Partial<Record<TermState, string>> = {
  forbidden: "ide.reason.noExec",
  disabled: "ide.term.disabled",
  nosessions: "ide.reason.noSessionsRead",
  unavailable: "ide.reason.sessionsDown",
}

// Barre toujours visible : l'utilisateur voit l'état du terminal même replié ou indisponible.
// Jamais d'ouverture seule : l'attache réduit la fenêtre tmux de l'hôte, elle attend un geste de l'utilisateur, mémorisé ensuite.
// focusN : demande de focus de la session (ouvre la zone, sans prendre la main).
// onToggle : bascule de la zone par le registre des commandes, avec l'état affiché ici (celui remonté par onView
// peut avoir un rendu de retard, et les gardes du registre le lisent) ; à défaut, la préférence directement.
export function BottomPane({ dir, prefs, focusN = 0, onView, onToggle }: { dir: string; prefs: ViewPrefs; focusN?: number; onView?: (view: BottomView) => void; onToggle?: (view: BottomView) => void }) {
  const { t } = useTranslation()
  const { state, sessions } = useTerminalAvailability(dir)
  const [storedHeight, setStoredHeight] = useLocal(`gld.ide.${dir}.h`, String(DEFAULT_HEIGHT))
  const [drag, setDrag] = useState<number>()
  const height = drag ?? clampHeight(Number(storedHeight) || DEFAULT_HEIGHT)
  const usable = state === "ready" || state === "nosession"
  const open = usable && prefs.showTerminalChosen && prefs.showTerminal
  useEffect(() => onView?.({ open, state }), [open, state, onView])
  const handled = useRef(focusN)
  const [pending, setPending] = useState(false)
  const focused = useCallback(() => setPending(false), [])
  useEffect(() => {
    if (focusN === handled.current || !usable) return
    handled.current = focusN
    setPending(true)
    if (!open) prefs.set("showTerminal", true)
  }, [focusN, usable, open, prefs])
  const reasonKey = REASONS[state]
  const reason = reasonKey ? t(reasonKey) : ""
  const statusText = () => {
    if (state === "loading") return ""
    if (reason) return reason
    if (state === "nosession") return t("ide.term.help")
    return t(open ? "ide.term.ready" : "ide.term.readyClosed", { count: sessions.length })
  }
  const status = statusText()
  const endResize = (size: number) => {
    setStoredHeight(String(clampHeight(size)))
    setDrag(undefined)
  }
  return (
    <>
      {open && <Handle label={t("ide.resize.bottom")} vertical start={() => height} onMove={(size) => setDrag(clampHeight(size))} onEnd={endResize} />}
      <div className="flex items-center gap-2 border-t px-2 py-0.5">
        <span className="text-xs font-medium">{t("ide.term.title")}</span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" role="status">{status}</span>
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={!usable}
          title={reason || undefined}
          aria-expanded={open}
          aria-label={t(open ? "ide.bottom.hide" : "ide.bottom.show")}
          onClick={() => (onToggle ? onToggle({ open, state }) : prefs.set("showTerminal", !open))}
        >
          <PanelBottom />
        </Button>
      </div>
      {open && (
        <div className="flex shrink-0 flex-col overflow-auto" style={{ height }}>
          <TerminalContent dir={dir} sessions={sessions} focus={pending} onFocused={focused} />
        </div>
      )}
    </>
  )
}
