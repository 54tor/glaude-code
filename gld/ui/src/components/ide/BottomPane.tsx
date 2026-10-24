import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { PanelBottom } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocal } from "@/hooks/useLocal"
import { Handle } from "./Handle"
import { clamp } from "./ideHelpers"
import { TerminalContent } from "./TerminalPane"
import { useTerminalAvailability } from "./useTerminalAvailability"
import type { ViewPrefs } from "./useViewPrefs"

export type BottomView = { open: boolean; available: boolean }

// Barre toujours visible : l'utilisateur voit l'état du terminal même replié ou indisponible.
// Le terminal ne s'ouvre seul que s'il y a de quoi l'attacher ; un choix explicite de l'utilisateur prime ensuite.
// focusN : demande de focus de la session (ouvre la zone, sans prendre la main).
// onToggle : bascule de la zone par le registre des commandes, avec l'état affiché ici (celui remonté par onView
// peut avoir un rendu de retard) ; à défaut, la préférence directement.
export function BottomPane({ dir, prefs, focusN = 0, onView, onToggle }: { dir: string; prefs: ViewPrefs; focusN?: number; onView?: (v: BottomView) => void; onToggle?: (open: boolean) => void }) {
  const { t } = useTranslation()
  const { state, sessions } = useTerminalAvailability(dir)
  const [hStr, setH] = useLocal(`gld.ide.${dir}.h`, "160")
  const [drag, setDrag] = useState<number>()
  const h = drag ?? clamp(Number(hStr) || 160, 60, 600)
  const usable = state === "ready" || state === "nosession"
  const open = usable && (prefs.showTerminalChosen ? prefs.showTerminal : state === "ready")
  const available = usable
  useEffect(() => onView?.({ open, available }), [open, available, onView])
  const handled = useRef(focusN)
  const [pending, setPending] = useState(false)
  const focused = useCallback(() => setPending(false), [])
  useEffect(() => {
    if (focusN === handled.current || !usable) return
    handled.current = focusN
    setPending(true)
    if (!open) prefs.set("showTerminal", true)
  }, [focusN, usable, open, prefs])
  const reason = state === "forbidden" ? t("ide.reason.noExec") : state === "disabled" ? t("ide.term.disabled") : ""
  const status =
    state === "loading" ? "" : reason || (state === "nosession" ? t("ide.term.help") : t("ide.term.ready", { count: sessions.length }))
  return (
    <>
      {open && <Handle label={t("ide.resize.bottom")} vertical start={() => h} onMove={(v) => setDrag(clamp(v, 60, 600))} onEnd={(v) => (setH(String(clamp(v, 60, 600))), setDrag(undefined))} />}
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
          onClick={() => (onToggle ? onToggle(open) : prefs.set("showTerminal", !open))}
        >
          <PanelBottom />
        </Button>
      </div>
      {open && (
        <div className="flex shrink-0 flex-col overflow-auto" style={{ height: h }}>
          <TerminalContent dir={dir} sessions={sessions} focus={pending} onFocused={focused} />
        </div>
      )}
    </>
  )
}
