import { useState } from "react"
import { AlertTriangle, Fan, Sparkles, Wrench } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { Finding, Issue } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

// --- santé -------------------------------------------------------------------------------------------

// Valeur technique renvoyée par le serveur → clé de catalogue.
const KINDS: Record<string, string> = {
  "index-ligne": "memory.health.kinds.indexLine",
  "index-absent": "memory.health.kinds.indexMissing",
  "index-long": "memory.health.kinds.indexLong",
  import: "memory.health.kinds.import",
  lien: "memory.health.kinds.link",
  "front-matter": "memory.health.kinds.frontMatter",
  perime: "memory.health.kinds.stale",
}

// Valeurs de « kind » de l'analyse par le modèle (memlint.go) : des codes stables, affichés traduits.
const LINT_KINDS: Record<string, string> = {
  doublon: "memory.health.kinds.lintDuplicate",
  contradiction: "memory.health.kinds.lintContradiction",
  périmé: "memory.health.kinds.lintStale",
}

const COST_DECIMALS = 4

type LintResult = { scope: string; findings?: Finding[]; error?: string; cost?: number }

const isFrontMatterIssue = (issue: Issue) => issue.kind === "front-matter"

const isRepairableIndexIssue = (issue: Issue) => issue.dir && (issue.kind === "index-ligne" || issue.kind === "index-absent")

const shortMemoryKey = (key: string) => key.replace(/^g\/rules\//, "rules/").replace(/^m\/[^/]+\//, "")

export function HealthDialog({
  open,
  issues,
  onClose,
  onOpenKey,
  onRepair,
  onFix,
  scopes,
  onLint,
}: {
  open: boolean
  issues: Issue[]
  onClose: () => void
  onOpenKey: (key: string) => void
  onRepair: (dir: string) => void
  onFix: (keys: string[]) => void // met entre guillemets les valeurs YAML qui cassent le front matter
  scopes: readonly (readonly [string, string])[] // portées de l'analyse : « global », puis les projets
  onLint: (scope: string) => Promise<{ findings?: Finding[]; error?: string; cost?: number }>
}) {
  const { t } = useTranslation()
  const [scope, setScope] = useState("global")
  const [busy, setBusy] = useState(false)
  const [lint, setLint] = useState<LintResult | null>(null)
  const analyse = async () => {
    setBusy(true)
    setLint({ scope, ...(await onLint(scope)) })
    setBusy(false)
  }
  const fixable = [...new Set(issues.filter(isFrontMatterIssue).map((issue) => issue.key))]
  const repairable = [...new Set(issues.filter(isRepairableIndexIssue).map((issue) => issue.dir!))]
  const openKeyAndClose = (key: string) => {
    onOpenKey(key)
    onClose()
  }
  const renderLintResult = (lintResult: LintResult) => {
    if (lintResult.error) return <p className="text-destructive">{lintResult.error}</p>
    if (lintResult.findings?.length === 0) {
      return <p className="text-muted-foreground">{t("memory.health.noFindings", { cost: lintResult.cost?.toFixed(COST_DECIMALS) ?? "" })}</p>
    }
    return lintResult.findings?.map((finding, findingIndex) => (
      <div key={findingIndex} className="rounded-md border px-2 py-1.5">
        <p className="text-xs font-medium text-muted-foreground">{LINT_KINDS[finding.kind] ? t(LINT_KINDS[finding.kind]) : finding.kind}</p>
        <p>{finding.why}</p>
        <p className="mt-1 flex flex-wrap gap-1">
          {finding.keys.map((key) => (
            <Button key={key} size="xs" variant="outline" onClick={() => openKeyAndClose(key)}>
              {shortMemoryKey(key)}
            </Button>
          ))}
        </p>
      </div>
    ))
  }
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("memory.health.title")}</DialogTitle>
          <DialogDescription>{issues.length ? t("memory.health.toCheck", { count: issues.length }) : t("memory.health.nothing")}</DialogDescription>
        </DialogHeader>
        {(repairable.length > 0 || fixable.length > 0) && (
          <div className="flex flex-wrap gap-2">
            {fixable.length > 1 && (
              <Button size="xs" variant="outline" onClick={() => onFix(fixable)} title={t("memory.health.fixTitle")}>
                <Wrench /> {t("memory.health.fixAll", { n: fixable.length })}
              </Button>
            )}
            {repairable.map((directory) => (
              <Button key={directory} size="xs" variant="outline" onClick={() => onRepair(directory)} title={t("memory.health.repairTitle")}>
                <Wrench /> {t("memory.health.repair", { dir: directory })}
              </Button>
            ))}
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 p-2 text-xs">
          <Sparkles className="size-3.5 text-muted-foreground" />
          <span className="font-medium">{t("memory.health.lintHeading")}</span>
          <Choose value={scope} onChange={setScope} options={scopes} label={t("memory.health.scopeAria")} size="xs" className="min-w-0 max-w-64" />
          <Button size="xs" onClick={analyse} disabled={busy} title={t("memory.health.analyzeTitle")}>
            {busy ? <Fan className="animate-spin" /> : <Sparkles />} {t("memory.health.analyze")}
          </Button>
        </div>
        {lint && <div className="space-y-1 text-sm">{renderLintResult(lint)}</div>}
        <ScrollArea className="max-h-96">
          <ul className="space-y-1 pr-3">
            {issues.map((issue, issueIndex) => (
              <li key={issueIndex} className="flex items-start gap-1">
                <button onClick={() => openKeyAndClose(issue.key)} className="flex min-w-0 flex-1 items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted">
                  <AlertTriangle className={cn("mt-0.5 size-3.5 shrink-0", issue.kind === "perime" ? "text-muted-foreground" : "text-warning")} />
                  <span className="min-w-0">
                    <span className="block text-xs font-medium text-muted-foreground">
                      {KINDS[issue.kind] ? t(KINDS[issue.kind]) : issue.kind} · {issue.key}
                    </span>
                    <span className="break-words">{issue.msg}</span>
                  </span>
                </button>
                {isFrontMatterIssue(issue) && (
                  <Button size="xs" variant="outline" className="mt-1 shrink-0" onClick={() => onFix([issue.key])} title={t("memory.health.fixTitle")}>
                    <Wrench /> {t("memory.health.fix")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
