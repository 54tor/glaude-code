import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"
import type { AgentView } from "@/lib/teams"

// Symbole en plus du texte : l'état ne repose jamais sur la couleur seule.
const SYM: Record<string, string> = { idle: "○", working: "●", waiting: "?", unknown: "–" }

export function stateKey(s: string) {
  return s in SYM ? s : "unknown"
}

export function AgentList({ agents, sel, onPick }: { agents: AgentView[]; sel?: string; onPick?: (name: string) => void }) {
  const { t } = useTranslation()
  return (
    <ul className="divide-y text-sm">
      {agents.map((a) => {
        const k = stateKey(a.State)
        const body = (
          <>
            <span aria-hidden>{SYM[k]}</span>
            <span className="font-medium">{a.Name}</span>
            {a.Lead && <span className="text-xs text-muted-foreground">{t("teams.lead")}</span>}
            <span className="truncate text-muted-foreground">{a.Role}</span>
            <span className="ml-auto text-xs">{t(`teams.state.${k}`)}</span>
          </>
        )
        const cls = "flex w-full items-center gap-2 px-3 py-2 text-left"
        return (
          <li key={a.Name}>
            {onPick ? (
              <button type="button" className={cn(cls, "hover:bg-muted", sel === a.Name && "bg-muted")} aria-pressed={sel === a.Name} onClick={() => onPick(a.Name)}>
                {body}
              </button>
            ) : (
              <div className={cls}>{body}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}
