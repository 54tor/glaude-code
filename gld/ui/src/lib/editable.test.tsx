// @vitest-environment jsdom
import { cleanup, render, renderHook } from "@testing-library/react"
import { createRef } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useShortcuts } from "@/hooks/useShortcuts"
import { ThemeProvider } from "@/components/theme-provider"
import type { Actions } from "@/components/actions"

// Monaco 0.57 saisit par EditContext : l'élément focalisé est un simple div, ni champ ni contenteditable (absent de jsdom).
const monaco = () => {
  const root = document.createElement("div")
  root.className = "monaco-editor"
  const ed = document.createElement("div")
  ed.className = "native-edit-context"
  ed.tabIndex = 0
  root.append(ed)
  document.body.append(root)
  Object.defineProperty(ed, "isContentEditable", { value: false })
  return ed
}
const press = (el: Element, key: string) => {
  const ev = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true })
  el.dispatchEvent(ev)
  return ev.defaultPrevented
}

beforeEach(() => {
  localStorage.clear()
  document.documentElement.className = ""
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} }))
})
afterEach(() => {
  cleanup()
  document.body.innerHTML = ""
  vi.unstubAllGlobals()
})

describe("frappe dans Monaco", () => {
  it("useShortcuts n'intercepte ni j, k, flèches, ni /", () => {
    const main = document.createElement("main")
    main.innerHTML = `<div data-row data-id="a" tabindex="0"></div><div data-row data-id="b" tabindex="0"></div>`
    document.body.append(main)
    const search = createRef<HTMLInputElement>() as { current: HTMLInputElement | null }
    search.current = document.createElement("input")
    document.body.append(search.current)
    renderHook(() => useShortcuts({ search, picked: new Set(), clearPicked: () => {}, byId: new Map(), actions: {} as Actions }))
    const ed = monaco()
    ed.focus()
    for (const key of ["j", "k", "ArrowDown", "ArrowUp", "/"]) expect(press(ed, key), key).toBe(false)
    expect(document.activeElement).toBe(ed)
    // Témoin : hors éditeur, le raccourci agit bien.
    expect(press(document.body, "/")).toBe(true)
    expect(document.activeElement).toBe(search.current)
  })

  it("d ne bascule pas le thème", () => {
    render(<ThemeProvider defaultTheme="light"><span /></ThemeProvider>)
    const before = document.documentElement.className
    press(monaco(), "d")
    expect(document.documentElement.className).toBe(before)
    expect(localStorage.getItem("theme")).toBeNull()
    press(document.body, "d")
    expect(localStorage.getItem("theme")).toBe("dark")
  })
})
