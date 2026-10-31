import { useEffect, useState } from "react"
import { AlertTriangle, CheckCircle2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { api, type CfgIssue, type EffRow } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Choose } from "./Choose"

const KIND = new Set(["json", "hook", "permission", "plugin", "secret"])
const fileLabel = (
  t: TFunction,
  id: string,
  home: string,
  projects: { dir: string; project: string }[]
) => {
  if (id === "u") return "~/.claude/settings.json"
  const projectDir = id.slice(2)
  const projectPath = projects.find((project) => project.dir === projectDir)?.project ?? projectDir
  const labelKey = id[0] === "p" ? "config.health.fileProject" : "config.health.fileLocal"
  return t(labelKey, { dir: tilde(projectPath, home) })
}

// --- santé ---------------------------------------------------------------------------------------------------

export function HealthTab({
  issues,
  home,
  projects,
}: {
  issues: CfgIssue[]
  home: string
  projects: { dir: string; project: string }[]
}) {
  const { t } = useTranslation()
  if (!issues.length)
    return (
      <p className="flex items-center gap-2 py-8 text-sm text-muted-foreground">
        <CheckCircle2 className="size-4 text-success" /> {t("config.health.ok")}
      </p>
    )
  return (
    <ul className="space-y-1">
      {issues.map((issue, issueIndex) => (
        <li
          key={issueIndex}
          className="flex items-start gap-2 rounded-md border px-3 py-2 text-sm"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
          <span className="min-w-0">
            <span className="block text-xs font-medium text-muted-foreground">
              {KIND.has(issue.kind) ? t(`config.issueKind.${issue.kind}`) : issue.kind} ·{" "}
              {fileLabel(t, issue.file, home, projects)}
            </span>
            <span className="break-words">{issue.msg}</span>
          </span>
        </li>
      ))}
    </ul>
  )
}

// --- précédence ----------------------------------------------------------------------------------------------

const LEVEL = {
  u: "config.precedence.colUser",
  p: "config.precedence.colProject",
  l: "config.precedence.colLocal",
} as const

export function PrecedenceTab({
  projects,
  home,
}: {
  projects: { dir: string; project: string }[]
  home: string
}) {
  const { t } = useTranslation()
  const [dir, setDir] = useState("")
  const [query, setQuery] = useState("")
  const [rows, setRows] = useState<EffRow[] | null>(null)
  useEffect(() => {
    let isMounted = true
    api.configEffective(dir).then((response) => isMounted && setRows(response.rows))
    return () => {
      isMounted = false
    }
  }, [dir])
  const lowerQuery = query.toLowerCase()
  const shown = (rows ?? []).filter((effectiveRow) => {
    if (!lowerQuery) return true
    if (effectiveRow.path.toLowerCase().includes(lowerQuery)) return true
    return Object.values(effectiveRow.values).some((levelValue) => levelValue.toLowerCase().includes(lowerQuery))
  })
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm">{t("config.precedence.seenFrom")}</span>
        <Choose
          value={dir}
          onChange={setDir}
          options={[
            ["", t("config.precedence.userOnly")],
            ...projects.map((project) => [project.dir, tilde(project.project, home)] as const),
          ]}
          label={t("config.precedence.project")}
          className="max-w-80 min-w-0"
        />
        <Input
          value={query}
          onChange={(changeEvent) => setQuery(changeEvent.target.value)}
          placeholder={t("config.precedence.filterPlaceholder")}
          className="h-8 w-48"
          aria-label={t("config.precedence.filterAria")}
        />
        <span className="text-xs text-muted-foreground">
          {t("config.precedence.hint")}
        </span>
      </div>
      {/* Sous sm, chaque réglage devient une carte : les trois niveaux s'empilent sous son nom, chacun avec son libellé. */}
      {rows === null ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full table-fixed text-sm max-sm:block">
            <colgroup className="max-sm:hidden">
              <col className="w-[28%]" />
              <col className="w-[24%]" />
              <col className="w-[24%]" />
              <col className="w-[24%]" />
            </colgroup>
            <thead className="bg-muted/50 text-left text-xs font-semibold text-muted-foreground max-sm:hidden">
              <tr>
                <th className="px-3 py-1.5">
                  {t("config.precedence.colSetting")}
                </th>
                <th className="px-3 py-1.5">
                  {t("config.precedence.colUser")}
                </th>
                <th className="px-3 py-1.5">
                  {t("config.precedence.colProject")}
                </th>
                <th className="px-3 py-1.5">
                  {t("config.precedence.colLocal")}
                </th>
              </tr>
            </thead>
            <tbody className="max-sm:block">
              {shown.map((effectiveRow) => (
                <tr
                  key={effectiveRow.path}
                  className="border-t align-top max-sm:block max-sm:py-1.5 max-sm:first:border-t-0"
                >
                  <td className="px-3 py-1.5 max-sm:block">
                    <span className="font-mono text-xs break-all">
                      {effectiveRow.path}
                    </span>
                    {describeKey(effectiveRow.path) && (
                      <span className="mt-0.5 block text-2xs leading-snug text-muted-foreground">
                        {describeKey(effectiveRow.path)}
                      </span>
                    )}
                  </td>
                  {(["u", "p", "l"] as const).map((level) => {
                    const levelValue = effectiveRow.values[level]
                    const isDefined = levelValue !== undefined
                    const isEffective = isDefined && (effectiveRow.merged || effectiveRow.winner === level)
                    return (
                      <td
                        key={level}
                        title={levelValue}
                        data-label={t(LEVEL[level])}
                        className={cn(
                          "px-3 py-1.5 text-xs break-words max-sm:block max-sm:py-0.5 max-sm:before:text-2xs max-sm:before:text-muted-foreground max-sm:before:content-[attr(data-label)]",
                          !isDefined && "max-sm:hidden",
                          isEffective ? "font-medium" : "text-muted-foreground"
                        )}
                      >
                        <span className={cn("line-clamp-3", !isEffective && "line-through decoration-muted-foreground/40")}>
                          {levelValue ?? ""}
                        </span>
                      </td>
                    )
                  })}
                </tr>
              ))}
              {shown.length === 0 && (
                <tr className="max-sm:block">
                  <td
                    colSpan={4}
                    className="px-3 py-6 text-center text-muted-foreground max-sm:block"
                  >
                    {t("config.precedence.none")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
      <p className="text-xs text-muted-foreground">
        {t("config.precedence.note")}
      </p>
    </div>
  )
}
