import { useMemo } from "react"
import { ArrowRight } from "lucide-react"
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"

// --- différences entre deux versions --------------------------------------------------------------------

type Row = { op: " " | "-" | "+"; t: string }

// Différence ligne à ligne (plus longue sous-suite commune) ; les textes de mémoire sont courts.
export function lineDiff(before: string, after: string): Row[] {
  const beforeLines = before.split("\n")
  const afterLines = after.split("\n")
  const beforeCount = beforeLines.length
  const afterCount = afterLines.length
  // commonLengthFrom[beforeIndex][afterIndex] : longueur de la plus longue sous-suite commune des deux suffixes.
  const commonLengthFrom = Array.from({ length: beforeCount + 1 }, () => new Array<number>(afterCount + 1).fill(0))
  for (let beforeIndex = beforeCount - 1; beforeIndex >= 0; beforeIndex--) {
    for (let afterIndex = afterCount - 1; afterIndex >= 0; afterIndex--) {
      const linesMatch = beforeLines[beforeIndex] === afterLines[afterIndex]
      if (linesMatch) {
        commonLengthFrom[beforeIndex][afterIndex] = commonLengthFrom[beforeIndex + 1][afterIndex + 1] + 1
      } else {
        commonLengthFrom[beforeIndex][afterIndex] = Math.max(commonLengthFrom[beforeIndex + 1][afterIndex], commonLengthFrom[beforeIndex][afterIndex + 1])
      }
    }
  }
  const diffRows: Row[] = []
  let beforeIndex = 0
  let afterIndex = 0
  while (beforeIndex < beforeCount && afterIndex < afterCount) {
    if (beforeLines[beforeIndex] === afterLines[afterIndex]) {
      diffRows.push({ op: " ", t: beforeLines[beforeIndex] })
      beforeIndex++
      afterIndex++
      continue
    }
    const removalKeepsLongerCommon = commonLengthFrom[beforeIndex + 1][afterIndex] >= commonLengthFrom[beforeIndex][afterIndex + 1]
    if (removalKeepsLongerCommon) {
      diffRows.push({ op: "-", t: beforeLines[beforeIndex] })
      beforeIndex++
    } else {
      diffRows.push({ op: "+", t: afterLines[afterIndex] })
      afterIndex++
    }
  }
  while (beforeIndex < beforeCount) {
    diffRows.push({ op: "-", t: beforeLines[beforeIndex] })
    beforeIndex++
  }
  while (afterIndex < afterCount) {
    diffRows.push({ op: "+", t: afterLines[afterIndex] })
    afterIndex++
  }
  return diffRows
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
