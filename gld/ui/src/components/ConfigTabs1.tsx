import { useEffect, useState } from "react"
import { AlertTriangle, CheckCircle2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { api, type CfgIssue, type EffRow, type HookInfo } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Choose } from "./Choose"

const LV: Record<string, string> = { u: "user", p: "project", l: "local" }
const KIND = new Set(["json", "hook", "permission", "plugin", "secret"])
const fileLabel = (
  t: TFunction,
  id: string,
  home: string,
  projects: { dir: string; project: string }[]
) =>
  id === "u"
    ? "~/.claude/settings.json"
    : t(
        id[0] === "p" ? "config.health.fileProject" : "config.health.fileLocal",
        {
          dir: tilde(
            projects.find((p) => p.dir === id.slice(2))?.project ?? id.slice(2),
            home
          ),
        }
      )

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
      {issues.map((i, n) => (
        <li
          key={n}
          className="flex items-start gap-2 rounded-md border px-3 py-2 text-sm"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
          <span className="min-w-0">
            <span className="block text-xs font-medium text-muted-foreground">
              {KIND.has(i.kind) ? t(`config.issueKind.${i.kind}`) : i.kind} ·{" "}
              {fileLabel(t, i.file, home, projects)}
            </span>
            <span className="break-words">{i.msg}</span>
          </span>
        </li>
      ))}
    </ul>
  )
}

// --- hooks ---------------------------------------------------------------------------------------------------

export function HooksTab({ hooks }: { hooks: HookInfo[] }) {
  const { t } = useTranslation()
  if (!hooks.length)
    return (
      <p className="py-8 text-sm text-muted-foreground">
        {t("config.hooks.none")}
      </p>
    )
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">
        {t("config.hooks.readOnly")}
      </p>
      <ul className="space-y-1">
        {hooks.map((h, n) => (
          <li key={n} className="rounded-md border px-3 py-2 text-sm">
            <p className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{h.event}</span>
              {h.matcher && (
                <code className="rounded-sm bg-muted px-1 text-xs">
                  {h.matcher}
                </code>
              )}
              <span className="text-xs text-muted-foreground">
                {LV[h.scope] ? t(`config.scope.${LV[h.scope]}`) : undefined}
              </span>
              {h.missing && (
                <span className="text-xs text-warning">
                  {t("config.hooks.missing", { path: h.missing })}
                </span>
              )}
            </p>
            <code className="mt-1 block text-xs break-all text-muted-foreground">
              {h.command || `(${h.type})`}
            </code>
          </li>
        ))}
      </ul>
    </div>
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
  const [q, setQ] = useState("")
  const [rows, setRows] = useState<EffRow[] | null>(null)
  useEffect(() => {
    let live = true
    api.configEffective(dir).then((r) => live && setRows(r.rows))
    return () => {
      live = false
    }
  }, [dir])
  const f = q.toLowerCase()
  const shown = (rows ?? []).filter(
    (r) =>
      !f ||
      r.path.toLowerCase().includes(f) ||
      Object.values(r.values).some((v) => v.toLowerCase().includes(f))
  )
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm">{t("config.precedence.seenFrom")}</span>
        <Choose
          value={dir}
          onChange={setDir}
          options={[
            ["", t("config.precedence.userOnly")],
            ...projects.map((p) => [p.dir, tilde(p.project, home)] as const),
          ]}
          label={t("config.precedence.project")}
          className="max-w-80 min-w-0"
        />
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
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
              {shown.map((r) => (
                <tr
                  key={r.path}
                  className="border-t align-top max-sm:block max-sm:py-1.5 max-sm:first:border-t-0"
                >
                  <td className="px-3 py-1.5 max-sm:block">
                    <span className="font-mono text-xs break-all">
                      {r.path}
                    </span>
                    {describeKey(r.path) && (
                      <span className="mt-0.5 block text-2xs leading-snug text-muted-foreground">
                        {describeKey(r.path)}
                      </span>
                    )}
                  </td>
                  {(["u", "p", "l"] as const).map((lv) => (
                    <td
                      key={lv}
                      title={r.values[lv]}
                      data-label={t(LEVEL[lv])}
                      className={cn(
                        "px-3 py-1.5 text-xs break-words max-sm:block max-sm:py-0.5 max-sm:before:text-2xs max-sm:before:text-muted-foreground max-sm:before:content-[attr(data-label)]",
                        r.values[lv] === undefined && "max-sm:hidden",
                        r.values[lv] !== undefined &&
                          (r.merged || r.winner === lv)
                          ? "font-medium"
                          : "text-muted-foreground"
                      )}
                    >
                      <span
                        className={cn(
                          "line-clamp-3",
                          !(
                            r.values[lv] !== undefined &&
                            (r.merged || r.winner === lv)
                          ) && "line-through decoration-muted-foreground/40"
                        )}
                      >
                        {r.values[lv] ?? ""}
                      </span>
                    </td>
                  ))}
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
