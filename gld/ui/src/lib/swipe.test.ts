// @vitest-environment jsdom
import { describe, expect, it } from "vitest"
import { swipeEnd, swipeStart } from "./swipe"

const ev = (number: number, localNumber: number) => ({ touches: [{ clientX: number, clientY: localNumber }], changedTouches: [{ clientX: number, clientY: localNumber }] }) as unknown as TouchEvent

describe("swipe", () => {
  it("reconnaît un balayage horizontal, pas un défilement ni un geste trop court", () => {
    const el = new EventTarget()
    swipeStart(ev(10, 100), el)
    expect(swipeEnd(ev(120, 110), el)).toBe("right")
    swipeStart(ev(200, 100), el)
    expect(swipeEnd(ev(100, 90), el)).toBe("left")
    swipeStart(ev(10, 100), el)
    expect(swipeEnd(ev(80, 200), el)).toBeUndefined()
    swipeStart(ev(10, 100), el)
    expect(swipeEnd(ev(40, 100), el)).toBeUndefined()
  })
  it("fromEdge n'accepte qu'un départ au bord gauche", () => {
    const el = new EventTarget()
    swipeStart(ev(100, 100), el)
    expect(swipeEnd(ev(300, 100), el, true)).toBeUndefined()
    swipeStart(ev(5, 100), el)
    expect(swipeEnd(ev(200, 100), el, true)).toBe("right")
  })
})
