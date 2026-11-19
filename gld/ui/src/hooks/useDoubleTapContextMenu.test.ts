// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest"
import { renderHook } from "@testing-library/react"
import { useDoubleTapContextMenu } from "./useDoubleTapContextMenu"

function pointer(pointerType: string, x = 10, y = 10) {
  const target = document.createElement("div")
  return {
    pointerType,
    clientX: x,
    clientY: y,
    target,
    stopPropagation: vi.fn(),
  } as never as React.PointerEvent<HTMLElement> & { target: HTMLElement }
}

describe("useDoubleTapContextMenu", () => {
  afterEach(() => vi.useRealTimers())

  it("ouvre le menu contextuel au double tap tactile seulement", () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useDoubleTapContextMenu())
    const premierTap = pointer("touch")
    const secondTap = pointer("touch")
    const spy = vi.spyOn(secondTap.target, "dispatchEvent")
    result.current.onPointerUp(premierTap)
    result.current.onPointerUp(secondTap)
    vi.advanceTimersByTime(100)
    expect(spy.mock.calls.length).toBe(1)
    expect((spy.mock.calls[0][0] as Event).type).toBe("contextmenu")
  })

  it("bloque le contextmenu natif de l'appui long tactile", () => {
    const { result } = renderHook(() => useDoubleTapContextMenu())
    result.current.onPointerDownCapture(pointer("touch"))
    const evenement = { stopPropagation: vi.fn() } as never
    result.current.onContextMenuCapture(evenement)
    expect(
      (evenement as { stopPropagation: ReturnType<typeof vi.fn> })
        .stopPropagation
    ).toHaveBeenCalledTimes(1)
  })

  it("laisse passer le clic droit souris", () => {
    const { result } = renderHook(() => useDoubleTapContextMenu())
    result.current.onPointerDownCapture(pointer("mouse"))
    const evenement = { stopPropagation: vi.fn() } as never
    result.current.onContextMenuCapture(evenement)
    expect(
      (evenement as { stopPropagation: ReturnType<typeof vi.fn> })
        .stopPropagation
    ).not.toHaveBeenCalled()
  })
})
