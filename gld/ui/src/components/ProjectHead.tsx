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
  const actions = useActions()
  const [dir, setDir] = useState("")
  const isMissingFolder = (directory: string) => actions.folder(directory)?.missing
  const chooseProject = () => {
    if (!dirs) return fixed ?? ""
    if (dirs.includes(dir)) return dir
    return dirs.find((directory) => !isMissingFolder(directory)) ?? dirs[0] ?? ""
  }
  const project = chooseProject()
  const missing = dirs ? (isMissingFolder(project) ?? false) : !!fixedMissing
  const directoryOption = (directory: string) => {
    const missingSuffix = isMissingFolder(directory) ? t("shell.head.missingSuffix") : ""
    return [directory, tilde(directory, actions.home) + missingSuffix] as const
  }
  const git = useGit()
  const repos = git.repos[project] ?? []
  const own = git.status[project]
  return (
    <div className="mb-4 space-y-3">
      {dirs && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          {t("shell.head.sessionDir")}
          <Choose value={project} onChange={setDir} options={dirs.map(directoryOption)} label={t("shell.head.newSessionDir")} className="min-w-0 max-w-xl" />
        </div>
      )}
      {(own || repos.length > 0) && (
        <ul className="space-y-1 rounded-lg border px-3 py-2 text-sm" aria-label={t("shell.head.gitState")}>
          {own && (
            <li className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={project}>
                {tilde(project, actions.home)}
              </span>
              <GitRepo dir={project} st={own} />
            </li>
          )}
          {repos.map((repoDir) => (
            <li key={repoDir} className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={repoDir}>
                {tilde(repoDir, actions.home)}
              </span>
              <GitRepo dir={repoDir} st={git.status[repoDir]} />
            </li>
          ))}
        </ul>
      )}
      {!missing && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          {dirs && (
            <Button size="sm" onClick={() => actions.newSession(project)} title={t("shell.header.newInDirHint")}>
              <MessageSquarePlus /> {t("shell.header.newSession")}
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => actions.openPerms(project)} title={t("shell.head.permsHint")}>
            <ShieldCheck /> {t("shell.head.perms")}
          </Button>
          <WorkspaceButton dir={project} />
        </div>
      )}
    </div>
  )
}
