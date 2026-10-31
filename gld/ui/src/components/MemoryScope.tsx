import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api, type MemFile, type MemProject } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"

// --- portée d'une règle ---------------------------------------------------------------------------------

// Sépare des motifs par des virgules, hors accolades : « a/*.{ts,tsx}, b/** » donne deux motifs.
function splitGlobs(text: string): string[] {
  const globs: string[] = []
  let braceDepth = 0
  let currentGlob = ""
  for (const character of text) {
    if (character === "{") braceDepth++
    else if (character === "}") braceDepth = Math.max(braceDepth - 1, 0)
    const isTopLevelSeparator = character === "," && braceDepth === 0
    if (isTopLevelSeparator) {
      globs.push(currentGlob)
      currentGlob = ""
    } else currentGlob += character
  }
  globs.push(currentGlob)
  return globs.map((glob) => glob.trim()).filter(Boolean)
}

const PREVIEW_FILE_LIMIT = 12

type MatchResult = { files?: string[]; total?: number; truncated?: boolean; error?: string }

// Globs du champ `paths:` d'une règle globale, éditables, et aperçu des fichiers d'un projet qu'ils ciblent.
export function ScopeBar({ file, projects, home, onSaved }: { file: MemFile; projects: MemProject[]; home: string; onSaved: () => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState((file.paths ?? []).join(", "))
  const [busy, setBusy] = useState(false)
  const existingProjects = projects.filter((project) => !project.missing)
  const [dir, setDir] = useState(existingProjects[0]?.dir ?? "")
  const [matches, setMatches] = useState<MatchResult | null>(null)
  const scoped = (file.paths?.length ?? 0) > 0
  const wanted = splitGlobs(text)
  const dirty = wanted.join("\n") !== (file.paths ?? []).join("\n")

  useEffect(() => {
    if (!scoped || !dir) return
    let isMounted = true
    api.memoryMatches(file.key, dir).then(
      (matchResult) => isMounted && setMatches(matchResult),
      (matchError) => isMounted && setMatches({ error: errMsg(matchError) })
    )
    return () => {
      isMounted = false
    }
  }, [file.key, file.paths, scoped, dir])

  const save = async () => {
    setBusy(true)
    try {
      await api.memoryPaths(file.key, wanted)
      onSaved()
    } catch (saveError) {
      setMatches({ error: errMsg(saveError) })
    }
    setBusy(false)
  }
  const renderMatchSummary = () => {
    if (matches?.error) return <span className="text-destructive">{matches.error}</span>
    if (!matches) return null
    return (
      <span className="text-muted-foreground">
        {t("memory.scope.matches", { count: matches.total ?? 0 })}
        {matches.truncated && t("memory.scope.truncated")}
      </span>
    )
  }
  const matchedFiles = scoped ? matches?.files : undefined
  return (
    <div className="space-y-2 border-b bg-muted/30 px-6 py-2 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{t("memory.scope.title")}</span>
        <Input
          value={text}
          onChange={(event) => setText(event.target.value)}
          placeholder={t("memory.scope.placeholder")}
          className="h-7 min-w-64 flex-1 text-xs"
          aria-label={t("memory.scope.pathsAria")}
        />
        <Button size="sm" onClick={save} disabled={!dirty || busy}>
          {t("memory.scope.save")}
        </Button>
      </div>
      <p className="text-muted-foreground">{scoped ? t("memory.scope.scoped") : t("memory.scope.unscoped")}</p>
      {scoped && existingProjects.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-muted-foreground">{t("memory.scope.previewIn")}</span>
          <Choose
            value={dir}
            onChange={setDir}
            options={existingProjects.map((project) => [project.dir, tilde(project.project, home)] as const)}
            label={t("memory.scope.previewAria")}
            className="min-w-0 max-w-72"
          />
          {renderMatchSummary()}
        </div>
      )}
      {matchedFiles && matchedFiles.length > 0 && (
        <p className="line-clamp-2 break-all text-muted-foreground">
          {matchedFiles.slice(0, PREVIEW_FILE_LIMIT).join(" · ")}
          {(matches?.total ?? 0) > PREVIEW_FILE_LIMIT && " …"}
        </p>
      )}
    </div>
  )
}
