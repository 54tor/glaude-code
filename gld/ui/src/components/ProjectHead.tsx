import { useState } from "react"
import { Send } from "lucide-react"
import { tilde } from "@/lib/format"
import { isSend } from "@/lib/utils"
import { useGit } from "@/hooks/useGit"
import { Button } from "@/components/ui/button"
import { useActions } from "./actions"
import type { Choice } from "@/lib/models"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { ModelPicker } from "./ModelPicker"

// En tête d'un dossier : l'état git (branche, changements en attente, avance et retard) du dossier et de ses dépôts, puis un champ
// pour lancer une nouvelle session dans ce dossier.
export function ProjectHead({ project, missing }: { project: string; missing: boolean }) {
  const a = useActions()
  const git = useGit()
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<Choice | null>(null) // modèle et effort choisis ici ; sinon ceux des réglages
  const choice = picked ?? a.defaultChoice
  const repos = git.repos[project] ?? []
  const own = git.status[project]
  const send = async () => {
    const prompt = text.trim()
    if (!prompt || busy) return
    setBusy(true)
    const started = await a.startSession(project, prompt, choice)
    setBusy(false)
    if (started) setText("")
  }
  return (
    <div className="mb-4 space-y-3">
      {(own || repos.length > 0) && (
        <ul className="space-y-1 rounded-lg border px-3 py-2 text-sm" aria-label="État git">
          {own && (
            <li className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={project}>
                {tilde(project, a.home)}
              </span>
              <GitBadge st={own} />
              <GitChanges dir={project} count={own.changed + own.untracked} />
            </li>
          )}
          {repos.map((d) => (
            <li key={d} className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={d}>
                {tilde(d, a.home)}
              </span>
              <GitBadge st={git.status[d]} />
              <GitChanges dir={d} count={git.status[d] ? git.status[d].changed + git.status[d].untracked : 0} />
            </li>
          ))}
        </ul>
      )}
      {!missing && (
        <div className="space-y-2">
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => isSend(e) && (e.preventDefault(), void send())}
            placeholder="Nouvelle session dans ce dossier… (Entrée lance, Maj+Entrée : saut de ligne)"
            rows={2}
            maxLength={32000}
            disabled={busy}
            aria-label="Prompt d'une nouvelle session"
            className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          />
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <ModelPicker value={choice} onChange={setPicked} disabled={busy} />
            <Button size="sm" className="ml-auto" onClick={send} disabled={!text.trim() || busy}>
              <Send /> Lancer la session
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
