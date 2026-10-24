import { useRef, type PointerEvent } from "react"

// Poignée de redimensionnement aux événements pointer : onMove pendant le glissement, onEnd au relâchement.
export function Handle({ label, vertical, start, onMove, onEnd }: { label: string; vertical?: boolean; start: () => number; onMove: (v: number) => void; onEnd: (v: number) => void }) {
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
