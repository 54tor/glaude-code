import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"
import type { AgentView } from "@/lib/teams"

// Symbole en plus du texte : l'état ne repose jamais sur la couleur seule.
const SYM: Record<string, string> = { idle: "○", working: "●", waiting: "?", unknown: "–" }

export function stateKey(agentState: string) {
  return agentState in SYM ? agentState : "unknown"
}

// « Prénom · Rôle », prénom seul sans rôle.
export const agentLabel = (agent: AgentView) => {
  if (!agent.Role) return agent.Display
  return `${agent.Display} · ${agent.Role}`
}

export function AgentList({ agents, sel, onPick }: { agents: AgentView[]; sel?: string; onPick?: (name: string) => void }) {
  const { t } = useTranslation()
  return (
    <ul className="divide-y text-sm">
      {agents.map((agent) => {
        const agentStateKey = stateKey(agent.State)
        const isSelected = sel === agent.Name
        const rowContent = (
          <>
            <span aria-hidden>{SYM[agentStateKey]}</span>
            <span className="font-medium">{agent.Display}</span>
            {agent.Lead && <span className="text-xs text-muted-foreground">{t("teams.lead")}</span>}
            {agent.Role && <span className="truncate text-muted-foreground">· {agent.Role}</span>}
            <span className="ml-auto text-xs">{t(`teams.state.${agentStateKey}`)}</span>
          </>
        )
        const rowClassName = "flex w-full items-center gap-2 px-3 py-2 text-left"
        return (
          <li key={agent.Name}>
            {onPick ? (
              <button type="button" className={cn(rowClassName, "hover:bg-muted", isSelected && "bg-muted")} aria-pressed={isSelected} onClick={() => onPick(agent.Name)}>
                {rowContent}
              </button>
            ) : (
              <div className={rowClassName}>{rowContent}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}
