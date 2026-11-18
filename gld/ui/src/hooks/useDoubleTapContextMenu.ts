import { useRef, type PointerEvent, type MouseEvent } from "react"

const DOUBLE_TAP_MS = 300
const DOUBLE_TAP_PX = 30
// Laisse la sélection native du double tap se poser avant de lire window.getSelection().
const OPEN_DELAY_MS = 60

// Radix ouvre le menu à l'appui long tactile, ce qui écrase la sélection native.
// Au tactile : appui long = sélection native, double tap = menu contextuel.
export function useDoubleTapContextMenu() {
  const touchActive = useRef(false)
  const opening = useRef(false)
  const lastTap = useRef({ time: 0, x: 0, y: 0 })

  const onPointerDownCapture = (pointerEvent: PointerEvent<HTMLElement>) => {
    touchActive.current = pointerEvent.pointerType !== "mouse"
    if (touchActive.current) pointerEvent.stopPropagation()
  }

  const onPointerUp = (pointerEvent: PointerEvent<HTMLElement>) => {
    if (pointerEvent.pointerType === "mouse") return
    const now = Date.now()
    const previous = lastTap.current
    const isDoubleTap =
      now - previous.time < DOUBLE_TAP_MS &&
      Math.hypot(pointerEvent.clientX - previous.x, pointerEvent.clientY - previous.y) < DOUBLE_TAP_PX
    lastTap.current = isDoubleTap ? { time: 0, x: 0, y: 0 } : { time: now, x: pointerEvent.clientX, y: pointerEvent.clientY }
    if (!isDoubleTap) return
    const { target, clientX, clientY } = pointerEvent
    window.setTimeout(() => {
      opening.current = true
      target.dispatchEvent(new window.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX, clientY }))
      opening.current = false
    }, OPEN_DELAY_MS)
  }

  const onContextMenuCapture = (mouseEvent: MouseEvent<HTMLElement>) => {
    if (touchActive.current && !opening.current) mouseEvent.stopPropagation()
  }

  return { onPointerDownCapture, onPointerUp, onContextMenuCapture }
}
