import { useRef, type PointerEvent } from "react"

// Poignée de redimensionnement aux événements pointer : onMove pendant le glissement, onEnd au relâchement.
export function Handle({ label, vertical, start, onMove, onEnd }: { label: string; vertical?: boolean; start: () => number; onMove: (size: number) => void; onEnd: (size: number) => void }) {
  const from = useRef<{ at: number; size: number } | null>(null)
  const pointerPosition = (event: PointerEvent) => (vertical ? event.clientY : event.clientX)
  const next = (event: PointerEvent) => {
    const origin = from.current!
    const travelled = pointerPosition(event) - origin.at
    return origin.size + (vertical ? -travelled : travelled)
  }
  return (
    <div
      role="separator"
      aria-orientation={vertical ? "horizontal" : "vertical"}
      aria-label={label}
      className={vertical ? "h-1 shrink-0 cursor-row-resize bg-border hover:bg-ring/50" : "w-1 shrink-0 cursor-col-resize bg-border hover:bg-ring/50"}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId)
        from.current = { at: pointerPosition(event), size: start() }
      }}
      onPointerMove={(event) => from.current && onMove(next(event))}
      onPointerUp={(event) => {
        if (from.current) onEnd(next(event))
        from.current = null
      }}
    />
  )
}
