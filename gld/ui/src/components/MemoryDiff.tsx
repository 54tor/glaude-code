import { useMemo } from "react"
import { ArrowRight } from "lucide-react"
import { useTranslation } from "react-i18next"
import { diffLines } from "diff"
import { cn } from "@/lib/utils"

// --- différences entre deux versions --------------------------------------------------------------------

type Row = { op: " " | "-" | "+"; t: string }

// Différence ligne à ligne (jsdiff) ; un saut de ligne final ne produit pas de ligne vide en plus.
export function lineDiff(before: string, after: string): Row[] {
  return diffLines(before, after).flatMap((part) => {
    const op = part.added ? "+" : part.removed ? "-" : " "
    const lines = part.value.split("\n")
    if (part.value.endsWith("\n")) lines.pop()
    return lines.map((line): Row => ({ op, t: line }))
  })
}

// old : version de la corbeille ; cur : version actuelle du fichier. compact : sans titre ni marges (dans une boîte de dialogue).
export function DiffView({ old, cur, compact }: { old: string; cur: string; compact?: boolean }) {
  const { t } = useTranslation()
  const rows = useMemo(() => lineDiff(old, cur), [old, cur])
  const same = rows.every((row) => row.op === " ")
  return (
    <div className={compact ? "p-2 text-xs" : "session-text mx-auto max-w-page px-6 py-6"}>
      {!compact && (
        <p className="mb-3 flex items-center gap-1.5 text-xs text-muted-foreground">
          {t("memory.diff.old")} <ArrowRight className="size-3" /> {t("memory.diff.current")} {same && t("memory.diff.same")}
        </p>
      )}
      <pre className="font-mono whitespace-pre-wrap">
        {rows.map((row, rowIndex) => (
          <div
            key={rowIndex}
            className={cn(
              row.op === "+" && "bg-success/10 text-success",
              row.op === "-" && "bg-destructive/10 text-destructive",
              row.op === " " && "text-muted-foreground"
            )}
          >
            {row.op} {row.t}
          </div>
        ))}
      </pre>
    </div>
  )
}
