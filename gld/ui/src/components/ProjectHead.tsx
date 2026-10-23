import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Send, ShieldCheck } from "lucide-react"
import { argAt } from "@/lib/commands"
import { tilde } from "@/lib/format"
import { isSend } from "@/lib/utils"
import { useGit } from "@/hooks/useGit"
import { Button } from "@/components/ui/button"
import { useActions } from "./actions"
import type { Choice } from "@/lib/models"
import { GitBadge } from "./GitBadge"
import { GitChanges } from "./GitChanges"
import { GitPush } from "./GitPush"
import { Choose } from "./Choose"
import { ModelPicker } from "./ModelPicker"
import { SnippetMenu } from "./SnippetMenu"

// En tête d'un dossier : l'état git (branche, changements en attente, avance et retard) du dossier et de ses dépôts, puis un champ
// pour lancer une nouvelle session dans ce dossier.
// dirs : vue d'un projet (ensemble de dossiers) : le prompt part dans le dossier choisi dans la liste, au lieu d'un dossier fixe.
export function ProjectHead({ project: fixed, missing: fixedMissing, dirs }: { project?: string; missing?: boolean; dirs?: string[] }) {
  const { t } = useTranslation()
  const a = useActions()
  const [dir, setDir] = useState("")
  const project = dirs ? (dirs.includes(dir) ? dir : (dirs.find((d) => !a.folder(d)?.missing) ?? dirs[0] ?? "")) : (fixed ?? "")
  const missing = dirs ? (a.folder(project)?.missing ?? false) : !!fixedMissing
  const git = useGit()
  const [text, setText] = useState("")
  const area = useRef<HTMLTextAreaElement>(null)
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<Choice | null>(null) // modèle et effort choisis ici ; sinon ceux des réglages
  const choice = picked ?? a.defaultChoice
  const repos = git.repos[project] ?? []
  const own = git.status[project]
  // Insère le snippet à la position du curseur ; son premier $ARGUMENTS est sélectionné pour être remplacé en tapant.
  const insert = (snip: string) => {
    const el = area.current
    const i = el?.selectionStart ?? text.length
    const j = el?.selectionEnd ?? text.length
    const arg = argAt(snip)
    setText(text.slice(0, i) + snip + text.slice(j))
    requestAnimationFrame(() => {
      el?.focus()
      const from = i + (arg?.at ?? snip.length)
      el?.setSelectionRange(from, from + (arg?.len ?? 0))
    })
  }
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
              <GitBadge st={own} />
              <GitPush dir={project} st={own} />
              <GitChanges dir={project} count={own.changed + own.untracked} />
            </li>
          )}
          {repos.map((d) => (
            <li key={d} className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="min-w-0 flex-1 truncate" title={d}>
                {tilde(d, a.home)}
              </span>
              <GitBadge st={git.status[d]} />
              <GitPush dir={d} st={git.status[d]} />
              <GitChanges dir={d} count={git.status[d] ? git.status[d].changed + git.status[d].untracked : 0} />
            </li>
          ))}
        </ul>
      )}
      {!missing && (
        <div className="space-y-2">
          <textarea
            ref={area}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => isSend(e) && (e.preventDefault(), void send())}
            placeholder={dirs ? t("shell.head.placeholderChosen") : t("shell.head.placeholderThis")}
            rows={2}
            maxLength={32000}
            disabled={busy}
            aria-label={t("shell.head.promptLabel")}
            className="session-text field-sizing-content max-h-48 min-h-14 w-full resize-none rounded-lg border bg-background px-3 py-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60"
          />
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <SnippetMenu onInsert={insert} />
            <Button size="sm" variant="outline" onClick={() => a.openPerms(project)} title={t("shell.head.permsHint")}>
              <ShieldCheck /> {t("shell.head.perms")}
            </Button>
            <ModelPicker value={choice} onChange={setPicked} disabled={busy} />
            <Button size="sm" className="ml-auto" onClick={send} disabled={!text.trim() || busy}>
              <Send /> {t("shell.head.start")}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
