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
  return (
    <div className="mb-4 space-y-3">
      {dirs && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          {t("shell.head.sessionDir")}
          <Choose value={project} onChange={setDir} options={dirs.map(directoryOption)} label={t("shell.head.newSessionDir")} className="min-w-0 max-w-xl" />
        </div>
      )}
      <ul className="space-y-1 rounded-lg border px-3 py-2 text-sm" aria-label={t("shell.head.gitState")}>
        {[project, ...repos].map((pathDir) => (
          <li key={pathDir} className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="min-w-0 flex-1 truncate" title={pathDir}>
              {tilde(pathDir, actions.home)}
            </span>
            {git.status[pathDir] && <GitRepo dir={pathDir} st={git.status[pathDir]} />}
            {!missing && (
              <Button size="icon-sm" variant="ghost" onClick={() => actions.openPerms(pathDir)} title={t("shell.head.permsHint")} aria-label={t("shell.head.perms")}>
                <ShieldCheck />
              </Button>
            )}
            {!missing && <WorkspaceButton dir={pathDir} />}
          </li>
        ))}
      </ul>
      {!missing && dirs && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Button size="sm" onClick={() => actions.newSession(project)} title={t("shell.header.newInDirHint")}>
            <MessageSquarePlus /> {t("shell.header.newSession")}
          </Button>
        </div>
      )}
    </div>
  )
}
