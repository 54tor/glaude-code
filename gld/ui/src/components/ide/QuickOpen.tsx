import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { cn } from "cn"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import type { IndexState } from "./useIndex"
import { rank } from "./quickOpen"

type Props = { open: boolean; files: string[]; state: IndexState; onPick: (path: string) => void; onClose: () => void }

export function QuickOpen({ open, onClose, ...rest }: Props) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="top-[20%] translate-y-0 sm:max-w-xl">{open && <Body {...rest} />}</DialogContent>
    </Dialog>
  )
}

function Body({ files, state, onPick }: Omit<Props, "open" | "onClose">) {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [sel, setSel] = useState(0)
  const list = useMemo(() => rank(files, q), [files, q])
  const box = useRef<HTMLUListElement>(null)
  useEffect(() => box.current?.children[sel]?.scrollIntoView?.({ block: "nearest" }), [sel])

  const key = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault()
      setSel((s) => Math.max(0, Math.min(list.length - 1, s + (e.key === "ArrowDown" ? 1 : -1))))
    } else if (e.key === "Enter" && list[sel]) {
      e.preventDefault()
      onPick(list[sel])
    }
  }
  return (
    <div className="grid gap-2">
      <DialogHeader className="sr-only">
        <DialogTitle>{t("ide.quick.title")}</DialogTitle>
        <DialogDescription>{t("ide.quick.hint")}</DialogDescription>
      </DialogHeader>
      {state !== "ready" && <p className="rounded-md border px-2 py-1 text-xs text-muted-foreground">{t(state === "partial" ? "ide.quick.partial" : "ide.quick.building")}</p>}
      <Input
        autoFocus
        role="combobox"
        aria-expanded
        aria-controls="ide-quick-list"
        aria-activedescendant={list[sel] ? `ide-quick-${sel}` : undefined}
        aria-label={t("ide.quick.title")}
        placeholder={t("ide.quick.placeholder")}
        value={q}
        onChange={(e) => (setQ(e.target.value), setSel(0))}
        onKeyDown={key}
      />
      <ul ref={box} id="ide-quick-list" role="listbox" aria-label={t("ide.quick.title")} className="max-h-80 overflow-auto">
        {list.map((p, i) => {
          const cut = p.lastIndexOf("/") + 1
          return (
            <li
              key={p}
              id={`ide-quick-${i}`}
              role="option"
              aria-selected={i === sel}
              title={p}
              onMouseMove={() => setSel(i)}
              onClick={() => onPick(p)}
              className={cn("flex cursor-default items-baseline gap-2 truncate rounded-md px-2 py-1", i === sel && "bg-accent text-accent-foreground")}
            >
              <span>{p.slice(cut)}</span>
              <span className="truncate text-xs text-muted-foreground">{p.slice(0, cut)}</span>
            </li>
          )
        })}
      </ul>
      {list.length === 0 && state !== "building" && <p className="px-2 py-1 text-muted-foreground">{t("ide.quick.none")}</p>}
    </div>
  )
}
