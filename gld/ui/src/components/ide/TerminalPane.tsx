import { Suspense, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { useLocal } from "@/hooks/useLocal"
import { retryLazy } from "@/lib/retryLazy"
import type { Session } from "./useTerminalAvailability"

const FullTerminal = retryLazy(() => import("../FullTerminal"))
const SHORT_ID_LENGTH = 8

// Le serveur ne lance jamais de shell : le terminal s'attache à une session Claude déjà vivante dont le dossier est le projet.
// focus : demande de focus en attente, acquittée par onFocused pour ne jamais être rejouée à un remontage.
export function TerminalContent({ dir, sessions, focus, onFocused }: { dir: string; sessions: Session[]; focus?: boolean; onFocused?: () => void }) {
  const { t } = useTranslation()
  const [seen, setSeen] = useState("")
  const [pick, setPick] = useLocal(`gld.ide.${dir}.term`, "")
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!focus) return
    box.current?.focus()
    onFocused?.()
  }, [focus, onFocused])
  const byId = (id: string) => sessions.find((session) => session.id === id)
  // Collante : le tri par activité change à chaque sondage, la session montée ne doit pas changer avec lui.
  // Une fois une session montée, seul le sélecteur en change : ni le choix mémorisé qui revivrait, ni la disparition
  // de la session (l'utilisateur choisit alors la suivante).
  const current = seen ? byId(seen) : (byId(pick) ?? sessions[0])
  if (current && current.id !== seen) setSeen(current.id)
  const showChooser = current ? sessions.length > 1 : !!seen && sessions.length > 0
  const chooser = showChooser && (
    <select
      aria-label={t("ide.term.pick")}
      value={current?.id ?? ""}
      onChange={(event) => {
        setPick(event.target.value)
        setSeen(event.target.value)
      }}
      className="m-1 w-fit rounded-md border bg-background px-2 py-1 text-xs"
    >
      {!current && <option value="" disabled>{t("ide.term.pick")}</option>}
      {sessions.map((session) => (
        <option key={session.id} value={session.id}>
          {session.title || session.id.slice(0, SHORT_ID_LENGTH)}
        </option>
      ))}
    </select>
  )
  return (
    <div ref={box} tabIndex={-1} className="flex size-full min-h-0 flex-col outline-none" aria-label={t("ide.term.label")}>
      {!current && <p className="p-3 text-sm text-muted-foreground">{t(seen ? "ide.term.ended" : "ide.term.none")}</p>}
      {chooser}
      {current && (
        <Suspense fallback={null}>
          <FullTerminal key={current.id} id={current.id} takeHand={false} />
        </Suspense>
      )}
    </div>
  )
}
