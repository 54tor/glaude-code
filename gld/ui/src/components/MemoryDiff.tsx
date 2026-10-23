import { useMemo } from "react"
import { ArrowRight } from "lucide-react"
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"

// --- différences entre deux versions --------------------------------------------------------------------

type Row = { op: " " | "-" | "+"; t: string }

// Différence ligne à ligne (plus longue sous-suite commune) ; les textes de mémoire sont courts.
export function lineDiff(a: string, b: string): Row[] {
  const x = a.split("\n")
  const y = b.split("\n")
  const n = x.length
  const m = y.length
  const l = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) l[i][j] = x[i] === y[j] ? l[i + 1][j + 1] + 1 : Math.max(l[i + 1][j], l[i][j + 1])
  const out: Row[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      out.push({ op: " ", t: x[i++] })
      j++
    }
    else if (l[i + 1][j] >= l[i][j + 1]) out.push({ op: "-", t: x[i++] })
    else out.push({ op: "+", t: y[j++] })
  }
  while (i < n) out.push({ op: "-", t: x[i++] })
  while (j < m) out.push({ op: "+", t: y[j++] })
  return out
}

// old : version de la corbeille ; cur : version actuelle du fichier. compact : sans titre ni marges (dans une boîte de dialogue).
export function DiffView({ old, cur, compact }: { old: string; cur: string; compact?: boolean }) {
  const { t } = useTranslation()
  const rows = useMemo(() => lineDiff(old, cur), [old, cur])
  const same = rows.every((r) => r.op === " ")
  return (
    <div className={compact ? "p-2 text-xs" : "session-text mx-auto max-w-7xl px-6 py-6"}>
      {!compact && (
        <p className="mb-3 flex items-center gap-1.5 text-xs text-muted-foreground">
          {t("memory.diff.old")} <ArrowRight className="size-3" /> {t("memory.diff.current")} {same && t("memory.diff.same")}
        </p>
      )}
      <pre className="font-mono whitespace-pre-wrap">
        {rows.map((r, i) => (
          <div
            key={i}
            className={cn(
              r.op === "+" && "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
              r.op === "-" && "bg-red-500/15 text-red-700 dark:text-red-300",
              r.op === " " && "text-muted-foreground"
            )}
          >
            {r.op} {r.t}
          </div>
        ))}
      </pre>
    </div>
  )
}
