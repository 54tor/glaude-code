import { useTranslation } from "react-i18next"
import { Folder } from "lucide-react"
import type { Route } from "@/lib/route"
import { ago, tilde } from "@/lib/format"
import { segs } from "@/lib/paths"
import { useGit } from "@/hooks/useGit"
import { useActions } from "./actions"
import { GitBadge } from "./GitBadge"

// Synthèse d'un projet : les totaux, puis une carte par dossier (sessions, dernière activité, tokens, état git).
export function ProjectOverview({ dirs, go }: { dirs: string[]; go: (r: Route) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const git = useGit()
  const cards = dirs.map((d) => {
    const ss = a.folder(d)?.sessions ?? []
    return { d, missing: a.folder(d)?.missing ?? false, n: ss.length, last: Math.max(0, ...ss.map((s) => s.mtime)), tok: ss.reduce((n, s) => n + s.tok.in + s.tok.out, 0) }
  })
  if (!cards.length) return null
  const sum = (f: (c: (typeof cards)[number]) => number) => cards.reduce((n, c) => n + f(c), 0)
  const last = Math.max(...cards.map((c) => c.last))
  const tokens = (n: number) => t("shell.overview.tokens", { n: n.toLocaleString() })
  return (
    <section className="mb-4 space-y-2" aria-label={t("shell.overview.title")}>
      <p className="text-sm text-muted-foreground">
        {t("shell.count.dirs", { count: cards.length })} · {t("shell.count.sessions", { count: sum((c) => c.n) })} · {tokens(sum((c) => c.tok))}
        {last > 0 && ` · ${t("shell.overview.last", { when: ago(last) })}`}
      </p>
      <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {cards.map((c) => {
          const st = git.status[c.d]
          return (
            <li key={c.d}>
              <button
                className="flex h-full w-full flex-col gap-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
                onClick={() => go({ kind: "dir", key: segs(c.d, a.home).join("/") })}
                title={c.d}
              >
                <span className="flex items-center gap-2">
                  <Folder className="size-4 shrink-0 text-info" />
                  <span className="min-w-0 flex-1 truncate font-medium">{tilde(c.d, a.home)}</span>
                  {st && <GitBadge st={st} />}
                </span>
                <span className="text-xs text-muted-foreground">
                  {c.missing
                    ? t("shell.head.missingSuffix").trim()
                    : [t("shell.count.sessions", { count: c.n }), c.last && ago(c.last), c.tok > 0 && tokens(c.tok)].filter(Boolean).join(" · ")}
                </span>
                {(git.repos[c.d] ?? []).map((r) => (
                  <span key={r} className="flex items-center gap-2 text-xs" title={r}>
                    <span className="min-w-0 flex-1 truncate pl-6 text-muted-foreground">{r.split("/").pop()}</span>
                    {git.status[r] && <GitBadge st={git.status[r]} />}
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
