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
function splitGlobs(s: string): string[] {
  const out: string[] = []
  let depth = 0
  let cur = ""
  for (const c of s) {
    if (c === "{") depth++
    else if (c === "}") depth = Math.max(depth - 1, 0)
    if (c === "," && depth === 0) {
      out.push(cur)
      cur = ""
    } else cur += c
  }
  out.push(cur)
  return out.map((g) => g.trim()).filter(Boolean)
}

// Globs du champ `paths:` d'une règle globale, éditables, et aperçu des fichiers d'un projet qu'ils ciblent.
export function ScopeBar({ file, projects, home, onSaved }: { file: MemFile; projects: MemProject[]; home: string; onSaved: () => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState((file.paths ?? []).join(", "))
  const [busy, setBusy] = useState(false)
  const real = projects.filter((p) => !p.missing)
  const [dir, setDir] = useState(real[0]?.dir ?? "")
  const [res, setRes] = useState<{ files?: string[]; total?: number; truncated?: boolean; error?: string } | null>(null)
  const scoped = (file.paths?.length ?? 0) > 0
  const wanted = splitGlobs(text)
  const dirty = wanted.join("\n") !== (file.paths ?? []).join("\n")

  useEffect(() => {
    if (!scoped || !dir) return
    let live = true
    api.memoryMatches(file.key, dir).then(
      (r) => live && setRes(r),
      (e) => live && setRes({ error: errMsg(e) })
    )
    return () => {
      live = false
    }
  }, [file.key, file.paths, scoped, dir])

  const save = async () => {
    setBusy(true)
    try {
      await api.memoryPaths(file.key, wanted)
      onSaved()
    } catch (e) {
      setRes({ error: errMsg(e) })
    }
    setBusy(false)
  }
  return (
    <div className="space-y-2 border-b bg-muted/30 px-6 py-2 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{t("memory.scope.title")}</span>
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t("memory.scope.placeholder")}
          className="h-7 min-w-64 flex-1 text-xs"
          aria-label={t("memory.scope.pathsAria")}
        />
        <Button size="xs" onClick={save} disabled={!dirty || busy}>
          {t("memory.scope.save")}
        </Button>
      </div>
      <p className="text-muted-foreground">{scoped ? t("memory.scope.scoped") : t("memory.scope.unscoped")}</p>
      {scoped && real.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-muted-foreground">{t("memory.scope.previewIn")}</span>
          <Choose
            value={dir}
            onChange={setDir}
            options={real.map((p) => [p.dir, tilde(p.project, home)] as const)}
            label={t("memory.scope.previewAria")}
            className="min-w-0 max-w-72"
          />
          {res?.error ? (
            <span className="text-destructive">{res.error}</span>
          ) : res ? (
            <span className="text-muted-foreground">
              {t("memory.scope.matches", { count: res.total ?? 0 })}
              {res.truncated && t("memory.scope.truncated")}
            </span>
          ) : null}
        </div>
      )}
      {scoped && res?.files && res.files.length > 0 && (
        <p className="line-clamp-2 break-all text-muted-foreground">
          {res.files.slice(0, 12).join(" · ")}
          {(res.total ?? 0) > 12 && " …"}
        </p>
      )}
    </div>
  )
}
