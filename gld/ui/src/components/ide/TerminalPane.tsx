import { lazy, Suspense, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useLocal } from "@/hooks/useLocal"
import { useTerminalAvailability, type Session } from "./useTerminalAvailability"

const FullTerminal = lazy(() => import("../FullTerminal"))

// Le serveur ne lance jamais de shell : le terminal s'attache à une session Claude déjà vivante dont le dossier est le projet.
// focusN : demande de focus du sélecteur (ou du conteneur) sans prendre la main sur le terminal.
export function TerminalContent({ dir, sessions, focusN = 0 }: { dir: string; sessions: Session[]; focusN?: number }) {
  const { t } = useTranslation()
  const [seen, setSeen] = useState("")
  const [pick, setPick] = useLocal(`gld.ide.${dir}.term`, "")
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (focusN) box.current?.focus()
  }, [focusN])
  // Collante : le tri par activité change à chaque sondage, la session montée ne doit pas changer avec lui.
  const cur = sessions.find((s) => s.id === pick) ?? sessions.find((s) => s.id === seen) ?? sessions[0]
  const curId = cur?.id
  if (curId && curId !== seen) setSeen(curId)
  if (!cur)
    return (
      <p className="p-3 text-sm text-muted-foreground">
        {t(seen ? "ide.term.ended" : "ide.term.none")}
      </p>
    )
  return (
    <div
      ref={box}
      tabIndex={-1}
      className="flex size-full min-h-0 flex-col outline-none"
      aria-label={t("ide.term.label")}
    >
      {sessions.length > 1 && (
        <select
          aria-label={t("ide.term.pick")}
          value={cur.id}
          onChange={(e) => setPick(e.target.value)}
          className="m-1 w-fit rounded-md border bg-background px-2 py-1 text-xs"
        >
          {sessions.map((s) => (
            <option key={s.id} value={s.id}>
              {s.title || s.id.slice(0, 8)}
            </option>
          ))}
        </select>
      )}
      <Suspense fallback={null}>
        <FullTerminal key={cur.id} id={cur.id} takeHand={false} />
      </Suspense>
    </div>
  )
}

export function TerminalPane({ dir }: { dir: string }) {
  const { state, sessions } = useTerminalAvailability(dir)
  if (state === "loading" || state === "forbidden") return null
  if (state === "disabled") return <DisabledNote />
  return <TerminalContent dir={dir} sessions={sessions} />
}

export function DisabledNote() {
  const { t } = useTranslation()
  return <p className="p-3 text-sm text-muted-foreground">{t("ide.term.disabled")}</p>
}
