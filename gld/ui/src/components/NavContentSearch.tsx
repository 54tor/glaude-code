import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Brain, FileText, FolderKanban, MessageSquare } from "lucide-react"
import { api, type Hit, type Project, type Session } from "@/lib/api"
import type { Row } from "@/components/actions"
import type { Route } from "@/lib/route"

const MIN_QUERY_LENGTH = 2
const TYPING_PAUSE_MS = 300

type ContentHits = { query: string; sessions: Hit[]; memory: [string, string][]; snippets: [string, string][] }

type Props = {
  query: string
  findSession: (sessionId: string) => Row | undefined
  projects: Project[]
  read: (session: Session) => void
  go: (route: Route) => void
}

function ResultSection({ title, icon: Icon, children }: { title: string; icon: typeof Brain; children: React.ReactNode }) {
  return (
    <section className="mb-3">
      <h2 className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground uppercase">
        <Icon className="size-3.5" /> {title}
      </h2>
      {children}
    </section>
  )
}

function ResultRow({ label, excerpt, onClick }: { label: string; excerpt: string; onClick: () => void }) {
  return (
    <button className="block w-full rounded-md px-2 py-1.5 text-left hover:bg-accent" onClick={onClick}>
      <span className="block truncate text-sm">{label}</span>
      <span className="block truncate text-xs text-muted-foreground">{excerpt}</span>
    </button>
  )
}

export function NavContentSearch({ query, findSession, projects, read, go }: Props) {
  const { t } = useTranslation()
  const [found, setFound] = useState<ContentHits | null>(null)
  const isLongEnough = query.length >= MIN_QUERY_LENGTH

  useEffect(() => {
    if (!isLongEnough) return
    let mounted = true
    const debounceTimer = setTimeout(async () => {
      const [sessionResponse, memoryResponse, snippetResponse] = await Promise.all([
        api.search(query).catch(() => ({ hits: [] as Hit[] })),
        api.memorySearch(query).catch(() => ({ hits: {} as Record<string, string> })),
        api.snippetSearch(query).catch(() => ({ hits: {} as Record<string, string> })),
      ])
      if (!mounted) return
      setFound({
        query,
        sessions: sessionResponse.hits,
        memory: Object.entries(memoryResponse.hits),
        snippets: Object.entries(snippetResponse.hits),
      })
    }, TYPING_PAUSE_MS)
    return () => {
      mounted = false
      clearTimeout(debounceTimer)
    }
  }, [query, isLongEnough])

  if (!isLongEnough) return <p className="px-3 py-2 text-xs text-muted-foreground">{t("shell.sidebar.contentTooShort")}</p>
  if (found?.query !== query) return <p className="px-3 py-2 text-xs text-muted-foreground">{t("shell.sidebar.searching")}…</p>

  const sessionRows = found.sessions.flatMap((hit) => {
    const row = findSession(hit.id)
    return row ? [{ hit, session: row.s, folder: row.g.project }] : []
  })
  const sessionRowsByProject = projects
    .map((projectItem) => ({ name: projectItem.name, rows: sessionRows.filter((sessionRow) => projectItem.dirs.includes(sessionRow.folder)) }))
    .filter((projectResult) => projectResult.rows.length > 0)
  const hasNoResult = sessionRows.length + found.memory.length + found.snippets.length === 0
  if (hasNoResult) return <p className="px-3 py-2 text-xs text-muted-foreground">{t("shell.sidebar.contentNone")}</p>

  return (
    <div className="px-2 pb-3">
      {sessionRowsByProject.map((projectResult) => (
        <ResultSection key={projectResult.name} title={projectResult.name} icon={FolderKanban}>
          {projectResult.rows.map(({ hit, session }) => (
            <ResultRow key={hit.id} label={session.title} excerpt={hit.snip} onClick={() => read(session)} />
          ))}
        </ResultSection>
      ))}
      {sessionRows.length > 0 && (
        <ResultSection title={t("shell.sidebar.contentSessions")} icon={MessageSquare}>
          {sessionRows.map(({ hit, session }) => (
            <ResultRow key={hit.id} label={session.title} excerpt={hit.snip} onClick={() => read(session)} />
          ))}
        </ResultSection>
      )}
      {found.memory.length > 0 && (
        <ResultSection title={t("shell.sidebar.contentMemory")} icon={Brain}>
          {found.memory.map(([memoryKey, excerpt]) => (
            <ResultRow key={memoryKey} label={memoryKey} excerpt={excerpt} onClick={() => go({ kind: "memory", key: memoryKey })} />
          ))}
        </ResultSection>
      )}
      {found.snippets.length > 0 && (
        <ResultSection title={t("shell.sidebar.contentSnippets")} icon={FileText}>
          {found.snippets.map(([snippetKey, excerpt]) => (
            <ResultRow key={snippetKey} label={snippetKey} excerpt={excerpt} onClick={() => go({ kind: "snippets", path: snippetKey })} />
          ))}
        </ResultSection>
      )}
    </div>
  )
}
