// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest"

import { blockModifiedClicks } from "./blockModifiedClicks"

function clickOnChild(init: MouseEventInit) {
  const container = document.createElement("div")
  const child = document.createElement("div")
  container.appendChild(child)
  const onChild = vi.fn()
  child.addEventListener("mousedown", onChild)
  const release = blockModifiedClicks(container)
  child.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, ...init }))
  return { onChild, release, container, child }
}

describe("blockModifiedClicks", () => {
  it("ne transmet pas un clic avec Ctrl au terminal", () => {
    expect(clickOnChild({ ctrlKey: true }).onChild).not.toHaveBeenCalled()
  })

  it("ne transmet pas un clic avec Cmd au terminal", () => {
    expect(clickOnChild({ metaKey: true }).onChild).not.toHaveBeenCalled()
  })

  it("laisse passer un clic simple", () => {
    expect(clickOnChild({}).onChild).toHaveBeenCalledTimes(1)
  })

  it("rend les clics modifiés après le retrait du garde", () => {
    const { onChild, release, child } = clickOnChild({ ctrlKey: true })
    release()
    child.dispatchEvent(
      new MouseEvent("mousedown", { bubbles: true, ctrlKey: true })
    )
    expect(onChild).toHaveBeenCalledTimes(1)
  })
})
