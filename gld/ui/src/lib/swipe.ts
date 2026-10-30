// Balayages tactiles : un seul doigt, horizontal franc (au moins 60 px, plus large que haut), sans bibliothèque.
// handlers() se pose sur l'élément (onTouchStart/onTouchEnd) ; edge limite le départ à une bande de 24 px du bord gauche.
export const SWIPE = 60
export const EDGE = 24

type T = { x: number; y: number; edge: boolean }
const starts = new WeakMap<EventTarget, T>()

export function swipeStart(e: React.TouchEvent | TouchEvent, el: EventTarget) {
  if (e.touches.length !== 1) return
  const t = e.touches[0]
  starts.set(el, { x: t.clientX, y: t.clientY, edge: t.clientX <= EDGE })
}

// Renvoie "left" ou "right" si le geste est un balayage, sinon undefined ; fromEdge exige un départ au bord gauche.
export function swipeEnd(e: React.TouchEvent | TouchEvent, el: EventTarget, fromEdge = false): "left" | "right" | undefined {
  const s = starts.get(el)
  starts.delete(el)
  if (!s || e.changedTouches.length !== 1 || (fromEdge && !s.edge)) return
  const t = e.changedTouches[0]
  const dx = t.clientX - s.x
  const dy = t.clientY - s.y
  if (Math.abs(dx) < SWIPE || Math.abs(dy) > Math.abs(dx)) return
  return dx > 0 ? "right" : "left"
}

export function swipeHandlers(on: (dir: "left" | "right") => void, fromEdge = false) {
  return {
    onTouchStart: (e: React.TouchEvent) => swipeStart(e, e.currentTarget),
    onTouchEnd: (e: React.TouchEvent) => {
      const d = swipeEnd(e, e.currentTarget, fromEdge)
      if (d) on(d)
    },
  }
}
