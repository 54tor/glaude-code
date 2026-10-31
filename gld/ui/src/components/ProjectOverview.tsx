import { useTranslation } from "react-i18next"
import { Folder } from "lucide-react"
import type { Route } from "@/lib/route"
import { ago, tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import { useGit } from "@/hooks/useGit"
import { useActions } from "./actions"
import { GitBadge } from "./GitBadge"

// Synthèse d'un projet : les totaux, puis une carte par dossier (sessions, dernière activité, tokens, état git).
export function ProjectOverview({ dirs, go }: { dirs: string[]; go: (route: Route) => void }) {
  const { t } = useTranslation()
  const actions = useActions()
  const git = useGit()
  const cards = dirs.map((directory) => {
    const sessions = actions.folder(directory)?.sessions ?? []
    return {
      directory,
      missing: actions.folder(directory)?.missing ?? false,
      sessionCount: sessions.length,
      last: Math.max(0, ...sessions.map((session) => session.mtime)),
      tokenCount: sessions.reduce((total, session) => total + session.tok.in + session.tok.out, 0),
    }
  })
  if (!cards.length) return null
  type Card = (typeof cards)[number]
  const sum = (valueOf: (card: Card) => number) => cards.reduce((total, card) => total + valueOf(card), 0)
  const last = Math.max(...cards.map((card) => card.last))
  const tokens = (tokenCount: number) => t("shell.overview.tokens", { n: tokenCount.toLocaleString() })
  const cardSummary = (card: Card) => {
    if (card.missing) return t("shell.head.missingSuffix").trim()
    const parts = [t("shell.count.sessions", { count: card.sessionCount }), card.last && ago(card.last), card.tokenCount > 0 && tokens(card.tokenCount)]
    return parts.filter(Boolean).join(" · ")
  }
  return (
    <section className="mb-4 space-y-2" aria-label={t("shell.overview.title")}>
      <p className="text-sm text-muted-foreground">
        {t("shell.count.dirs", { count: cards.length })} · {t("shell.count.sessions", { count: sum((card) => card.sessionCount) })} · {tokens(sum((card) => card.tokenCount))}
        {last > 0 && ` · ${t("shell.overview.last", { when: ago(last) })}`}
      </p>
      <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {cards.map((card) => {
          const gitStatus = git.status[card.directory]
          return (
            <li key={card.directory}>
              <button
                className="flex h-full w-full flex-col gap-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
                onClick={() => go({ kind: "dir", key: segs(card.directory, actions.home).join("/") })}
                title={card.directory}
              >
                <span className="flex items-center gap-2">
                  <Folder className="size-4 shrink-0 text-info" />
                  <span className="min-w-0 flex-1 truncate font-medium">{tilde(card.directory, actions.home)}</span>
                  {gitStatus && <GitBadge st={gitStatus} />}
                </span>
                <span className="text-xs text-muted-foreground">{cardSummary(card)}</span>
                {(git.repos[card.directory] ?? []).map((repoDir) => (
                  <span key={repoDir} className="flex items-center gap-2 text-xs" title={repoDir}>
                    <span className="min-w-0 flex-1 truncate pl-6 text-muted-foreground">{repoDir.split("/").pop()}</span>
                    {git.status[repoDir] && <GitBadge st={git.status[repoDir]} />}
                  </span>
                ))}
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
