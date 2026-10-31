import { useState, type ReactNode } from "react"
import { ChevronRight, TriangleAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { SyncItem } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"

export type Mode = "merge" | "replace"
export type Picks = Record<string, Mode>

const STATUS = { new: "text-success", diff: "text-warning", same: "text-muted-foreground" } as const
const KINDS = ["setting", "perm", "file", "skill"]
const CATS = ["skills", "plugins", "settings", "memory", "mcp"]

// Liste d'éléments comparables (plan de synchronisation ou d'import) : case, statut, aperçu source/cible, fusionner ou remplacer.
// groupBy : sections par genre d'élément (défaut) ou par catégorie d'archive ; extra : ligne ajoutée sous un élément.
export function SyncItems({
  items,
  picks,
  setPicks,
  groupBy = "kind",
  extra,
}: {
  items: SyncItem[]
  picks: Picks
  setPicks: (f: (p: Picks) => Picks) => void
  groupBy?: "kind" | "cat"
  extra?: (i: SyncItem) => ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Set<string>>(new Set())
  const togglePick = (item: SyncItem, isChecked: boolean) =>
    setPicks((previousPicks) => {
      const nextPicks = { ...previousPicks }
      if (isChecked) nextPicks[item.id] = item.merge ? "merge" : "replace"
      else delete nextPicks[item.id]
      return nextPicks
    })
  const toggleExpanded = (itemId: string) =>
    setOpen((previouslyOpen) => {
      const nextOpen = new Set(previouslyOpen)
      if (!nextOpen.delete(itemId)) nextOpen.add(itemId)
      return nextOpen
    })
  const sectionKeys = groupBy === "cat" ? CATS : KINDS
  const sectionKeyOf = (item: SyncItem) => (groupBy === "cat" ? item.cat ?? "" : item.kind)
  return (
    <>
      {sectionKeys.map((kind) => {
        const rows = items.filter((item) => sectionKeyOf(item) === kind)
        if (rows.length === 0) return null
        return (
          <section key={kind}>
            <h3 className="text-sm font-medium">{groupBy === "cat" ? t(`backup.cat.${kind}`) : t(`config.sync.kind.${kind}`)}</h3>
            <ul className="mt-1 space-y-px">
              {rows.map((item) => {
                const label = t(`config.sync.status.${item.status}`)
                const tone = STATUS[item.status]
                const isOpen = open.has(item.id)
                const isPicked = item.id in picks
                const canChooseMode = isPicked && item.merge && item.status === "diff"
                return (
                  <li key={item.id} className="rounded-md px-2 py-1 text-sm hover:bg-muted">
                    <div className="flex flex-wrap items-center gap-2">
                      <Checkbox checked={isPicked} disabled={item.status === "same"} onCheckedChange={(checkedState) => togglePick(item, checkedState === true)} aria-label={t("config.sync.copyItem", { label: item.label })} />
                      <button className="flex min-w-0 flex-1 items-center gap-1 text-left" onClick={() => toggleExpanded(item.id)} aria-expanded={isOpen}>
                        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", isOpen && "rotate-90")} />
                        <code className="truncate text-xs">{item.label}</code>
                      </button>
                      {item.risk === "exec" && (
                        <span className="flex items-center gap-1 text-xs text-warning" title={t("config.sync.riskTitle")}>
                          <TriangleAlert className="size-3.5" /> {t("config.sync.risk")}
                        </span>
                      )}
                      <span className={cn("shrink-0 text-xs", tone)}>{label}</span>
                      {canChooseMode && (
                        <span className="flex shrink-0 gap-1" role="radiogroup" aria-label={t("config.sync.modeAria")}>
                          {(["merge", "replace"] as const).map((mode) => (
                            <Button key={mode} size="xs" variant={picks[item.id] === mode ? "secondary" : "ghost"} role="radio" aria-checked={picks[item.id] === mode} onClick={() => setPicks((previousPicks) => ({ ...previousPicks, [item.id]: mode }))}>
                              {mode === "merge" ? t("config.shared.merge") : t("config.shared.replace")}
                            </Button>
                          ))}
                        </span>
                      )}
                    </div>
                    {extra?.(item)}
                    {isOpen && (
                      <div className="mt-1 grid gap-2 md:grid-cols-2">
                        {[
                          ["source", item.from],
                          ["target", item.to],
                        ].map(([side, text]) => (
                          <div key={side}>
                            <p className="text-xs text-muted-foreground">{t(`config.sync.${side}`)}</p>
                            <pre className="max-h-56 overflow-auto rounded-md border bg-background p-2 font-mono text-xs whitespace-pre-wrap">{text || t("config.sync.absent")}</pre>
                          </div>
                        ))}
                      </div>
                    )}
                  </li>
                )
              })}
            </ul>
          </section>
        )
      })}
    </>
  )
}
