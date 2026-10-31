// Balayages tactiles : un seul doigt, horizontal franc (au moins 60 px, plus large que haut), sans bibliothèque.
// handlers() se pose sur l'élément (onTouchStart/onTouchEnd) ; edge limite le départ à une bande de 24 px du bord gauche.
export const SWIPE = 60
export const EDGE = 24

type TouchStart = { x: number; y: number; edge: boolean }
const touchStarts = new WeakMap<EventTarget, TouchStart>()

export function swipeStart(event: React.TouchEvent | TouchEvent, element: EventTarget) {
  if (event.touches.length !== 1) return
  const touch = event.touches[0]
  touchStarts.set(element, { x: touch.clientX, y: touch.clientY, edge: touch.clientX <= EDGE })
}

// Renvoie "left" ou "right" si le geste est un balayage, sinon undefined ; fromEdge exige un départ au bord gauche.
export function swipeEnd(event: React.TouchEvent | TouchEvent, element: EventTarget, fromEdge = false): "left" | "right" | undefined {
  const start = touchStarts.get(element)
  touchStarts.delete(element)
  if (!start || event.changedTouches.length !== 1) return
  if (fromEdge && !start.edge) return
  const touch = event.changedTouches[0]
  const deltaX = touch.clientX - start.x
  const deltaY = touch.clientY - start.y
  const isTooShort = Math.abs(deltaX) < SWIPE
  const isMostlyVertical = Math.abs(deltaY) > Math.abs(deltaX)
  if (isTooShort || isMostlyVertical) return
  return deltaX > 0 ? "right" : "left"
}

export function swipeHandlers(onSwipe: (direction: "left" | "right") => void, fromEdge = false) {
  return {
    onTouchStart: (event: React.TouchEvent) => swipeStart(event, event.currentTarget),
    onTouchEnd: (event: React.TouchEvent) => {
      const direction = swipeEnd(event, event.currentTarget, fromEdge)
      if (direction) onSwipe(direction)
    },
  }
}
