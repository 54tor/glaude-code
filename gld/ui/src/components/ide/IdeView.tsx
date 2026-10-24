import { useRef, useState, type PointerEvent } from "react"
import { useTranslation } from "react-i18next"
import { PanelBottom } from "lucide-react"
import { useCan } from "@/lib/auth"
import { useLocal } from "@/hooks/useLocal"
import { Button } from "@/components/ui/button"
import { FileTree } from "./FileTree"

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

// Poignée de redimensionnement aux événements pointer : onMove pendant le glissement, onEnd au relâchement.
function Handle({ label, vertical, start, onMove, onEnd }: { label: string; vertical?: boolean; start: () => number; onMove: (v: number) => void; onEnd: (v: number) => void }) {
  const from = useRef<{ at: number; v: number } | null>(null)
  const pos = (e: PointerEvent) => (vertical ? e.clientY : e.clientX)
  const next = (e: PointerEvent) => from.current!.v + (vertical ? from.current!.at - pos(e) : pos(e) - from.current!.at)
  return (
    <div
      role="separator"
      aria-orientation={vertical ? "horizontal" : "vertical"}
      aria-label={label}
      className={vertical ? "h-1 shrink-0 cursor-row-resize bg-border hover:bg-ring/50" : "w-1 shrink-0 cursor-col-resize bg-border hover:bg-ring/50"}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        from.current = { at: pos(e), v: start() }
      }}
      onPointerMove={(e) => from.current && onMove(next(e))}
      onPointerUp={(e) => {
        if (from.current) onEnd(next(e))
        from.current = null
      }}
    />
  )
}

// Espace de travail d'un dossier : arborescence, zone centrale, panneau du bas repliable.
export function IdeView({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const can = useCan()
  const [wStr, setW] = useLocal(`gld.ide.${dir}.w`, "260")
  const [hStr, setH] = useLocal(`gld.ide.${dir}.h`, "160")
  const [bottom, setBottom] = useLocal(`gld.ide.${dir}.bottom`, "1")
  const [drag, setDrag] = useState<{ w?: number; h?: number }>({})
  const [sel, setSel] = useState("")
  const w = drag.w ?? clamp(Number(wStr) || 260, 140, 700)
  const h = drag.h ?? clamp(Number(hStr) || 160, 60, 600)

  if (!can("fs_read")) return <p className="p-6 text-sm text-muted-foreground">{t("ide.denied")}</p>
  return (
    <div className="flex min-h-0 flex-1">
      <aside className="flex min-h-0 shrink-0 flex-col" style={{ width: w }}>
        <FileTree dir={dir} canWrite={can("fs_write")} selected={sel} onOpen={setSel} />
      </aside>
      <Handle label={t("ide.resize.tree")} start={() => w} onMove={(v) => setDrag({ w: clamp(v, 140, 700) })} onEnd={(v) => (setW(String(clamp(v, 140, 700))), setDrag({}))} />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="grid min-h-0 flex-1 place-items-center p-6 text-sm text-muted-foreground">{sel ? t("ide.center.selected", { path: sel }) : t("ide.center.none")}</div>
        <div className="flex justify-end border-t px-2 py-0.5">
          <Button variant="ghost" size="icon-sm" onClick={() => setBottom(bottom ? "" : "1")} aria-pressed={!!bottom} aria-label={t(bottom ? "ide.bottom.hide" : "ide.bottom.show")}>
            <PanelBottom />
          </Button>
        </div>
        {bottom && (
          <>
            <Handle label={t("ide.resize.bottom")} vertical start={() => h} onMove={(v) => setDrag({ h: clamp(v, 60, 600) })} onEnd={(v) => (setH(String(clamp(v, 60, 600))), setDrag({}))} />
            <div className="shrink-0 overflow-auto" style={{ height: h }} />
          </>
        )}
      </div>
    </div>
  )
}
