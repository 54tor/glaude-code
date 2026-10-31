import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
import { cn } from "cn"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import type { IndexState } from "./useIndex"
import { rank } from "./rank"

// onCloseFocus : où rendre le focus à la fermeture, à la place de l'élément d'avant (souvent un éditeur masqué depuis).
type Props = { open: boolean; files: string[]; state: IndexState; onPick: (path: string) => void; onClose: () => void; onCloseFocus?: () => void }

export function QuickOpen({ open, onClose, onCloseFocus, ...rest }: Props) {
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent
        className="top-[20%] translate-y-0 sm:max-w-xl"
        onCloseAutoFocus={(event) => {
          if (!onCloseFocus) return
          event.preventDefault()
          onCloseFocus()
        }}
      >{open && <Body {...rest} />}</DialogContent>
    </Dialog>
  )
}

function Body({ files, state, onPick }: Omit<Props, "open" | "onClose">) {
  const { t } = useTranslation()
  const [query, setQuery] = useState("")
  const [selected, setSelected] = useState(0)
  const list = useMemo(() => rank(files, query), [files, query])
  const box = useRef<HTMLUListElement>(null)
  // Corps en bloc : dans Chrome, scrollIntoView renvoie une Promise que React prendrait pour un nettoyage.
  useEffect(() => {
    box.current?.children[selected]?.scrollIntoView?.({ block: "nearest" })
  }, [selected])

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      const step = event.key === "ArrowDown" ? 1 : -1
      setSelected((current) => Math.max(0, Math.min(list.length - 1, current + step)))
      return
    }
    if (event.key === "Enter" && list[selected]) {
      event.preventDefault()
      onPick(list[selected])
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
        aria-activedescendant={list[selected] ? `ide-quick-${selected}` : undefined}
        aria-label={t("ide.quick.title")}
        placeholder={t("ide.quick.placeholder")}
        value={query}
        onChange={(event) => {
          setQuery(event.target.value)
          setSelected(0)
        }}
        onKeyDown={onKeyDown}
      />
      <ul ref={box} id="ide-quick-list" role="listbox" aria-label={t("ide.quick.title")} className="max-h-80 overflow-auto">
        {list.map((path, position) => {
          const nameStart = path.lastIndexOf("/") + 1
          return (
            <li
              key={path}
              id={`ide-quick-${position}`}
              role="option"
              aria-selected={position === selected}
              title={path}
              onMouseMove={() => setSelected(position)}
              onClick={() => onPick(path)}
              className={cn("flex cursor-default items-baseline gap-2 truncate rounded-md px-2 py-1", position === selected && "bg-accent text-accent-foreground")}
            >
              <span>{path.slice(nameStart)}</span>
              <span className="truncate text-xs text-muted-foreground">{path.slice(0, nameStart)}</span>
            </li>
          )
        })}
      </ul>
      {list.length === 0 && state !== "building" && <p className="px-2 py-1 text-muted-foreground">{t("ide.quick.none")}</p>}
    </div>
  )
}
