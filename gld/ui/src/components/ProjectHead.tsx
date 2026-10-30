import { useState } from "react"
import { useTranslation } from "react-i18next"
import { MessageSquarePlus, ShieldCheck } from "lucide-react"
import { tilde } from "@/lib/format"
import { useGit } from "@/hooks/useGit"
import { Button } from "@/components/ui/button"
import { useActions } from "./actions"
import { WorkspaceButton } from "./ide/WorkspaceButton"
import { GitRepo } from "./GitRepo"
import { Choose } from "./Choose"

// En tête d'un dossier : l'état git (branche, changements en attente, avance et retard) du dossier et de ses dépôts, puis ses raccourcis.
// dirs : vue d'un projet (ensemble de dossiers) : la nouvelle session (en modale) part dans le dossier choisi dans la liste.
export function ProjectHead({ project: fixed, missing: fixedMissing, dirs }: { project?: string; missing?: boolean; dirs?: string[] }) {
  const { t } = useTranslation()
  const a = useActions()
  const [dir, setDir] = useState("")
  const project = dirs ? (dirs.includes(dir) ? dir : (dirs.find((d) => !a.folder(d)?.missing) ?? dirs[0] ?? "")) : (fixed ?? "")
  const missing = dirs ? (a.folder(project)?.missing ?? false) : !!fixedMissing
  const git = useGit()
  const repos = git.repos[project] ?? []
  const own = git.status[project]
  return (
    <div className="mb-4 space-y-3">
      {dirs && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          {t("shell.head.sessionDir")}
          <Choose value={project} onChange={setDir} options={dirs.map((d) => [d, tilde(d, a.home) + (a.folder(d)?.missing ? t("shell.head.missingSuffix") : "")] as const)} label={t("shell.head.newSessionDir")} className="min-w-0 max-w-xl" />
        </div>
      )}
      {(own || repos.length > 0) && (
        <ul className="space-y-1 rounded-lg border px-3 py-2 text-sm" aria-label={t("shell.head.gitState")}>
          {own && (
            <li className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={project}>
                {tilde(project, a.home)}
              </span>
              <GitRepo dir={project} st={own} />
            </li>
          )}
          {repos.map((d) => (
            <li key={d} className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={d}>
                {tilde(d, a.home)}
              </span>
              <GitRepo dir={d} st={git.status[d]} />
            </li>
          ))}
        </ul>
      )}
      {!missing && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {dirs && (
            <Button size="sm" onClick={() => a.newSession(project)} title={t("shell.header.newInDirHint")}>
              <MessageSquarePlus /> {t("shell.header.newSession")}
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => a.openPerms(project)} title={t("shell.head.permsHint")}>
            <ShieldCheck /> {t("shell.head.perms")}
          </Button>
          <WorkspaceButton dir={project} />
        </div>
      )}
    </div>
  )
}
