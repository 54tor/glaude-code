// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api } from "@/lib/api"
import { GitChanges } from "../GitChanges"
import { toMarks, useGitMarks } from "./useGitMarks"

const f = (path: string, status: string, from?: string) => ({
  path,
  status,
  ...(from && { from }),
})
beforeEach(() => setLang("fr"))
afterEach(() => (cleanup(), vi.restoreAllMocks()))

describe("toMarks", () => {
  it("réduit les codes porcelain à une lettre et marque les dossiers parents", () => {
    const { marks, dirs } = toMarks([
      f("a/b/m.ts", "M "),
      f("a/w.ts", " M"),
      f("n.ts", "A "),
      f("u.ts", "??"),
      f("d.ts", " D"),
      f("x/new.ts", "R ", "x/old.ts"),
      f("c1.ts", "UU"),
      f("c2.ts", "AA"),
    ])
    expect(Object.fromEntries(marks)).toEqual({
      "a/b/m.ts": "M",
      "a/w.ts": "M",
      "n.ts": "A",
      "u.ts": "?",
      "d.ts": "D",
      "x/new.ts": "R",
      "c1.ts": "U",
      "c2.ts": "U",
    })
    expect([...dirs].sort()).toEqual(["a", "a/b", "x"])
  })
})

describe("useGitMarks", () => {
  it("lit à l'ouverture, expose le reste tronqué, se relit à la demande", async () => {
    const spy = vi
      .spyOn(api, "gitChanges")
      .mockResolvedValue({ files: [f("a.ts", " M")], more: 5 })
    const { result } = renderHook(() => useGitMarks("/p"))
    await waitFor(() => expect(result.current.marks.get("a.ts")).toBe("M"))
    expect(result.current.more).toBe(5)
    spy.mockResolvedValue({ files: [] })
    await act(async () => result.current.refresh())
    await waitFor(() => expect(result.current.marks.size).toBe(0))
    expect(result.current.more).toBe(0)
  })
  it("une erreur ne laisse aucun marqueur et ne lève rien", async () => {
    vi.spyOn(api, "gitChanges").mockRejectedValue(new Error("x"))
    const { result } = renderHook(() => useGitMarks("/p"))
    await act(async () => {})
    expect(result.current.marks.size).toBe(0)
  })
})

describe("GitChanges onOpen", () => {
  const files = [f("src/a.ts", " M")]
  it("un clic sur la ligne appelle onOpen et ferme la fenêtre", async () => {
    vi.spyOn(api, "gitChanges").mockResolvedValue({ files })
    const onOpen = vi.fn()
    render(<GitChanges dir="/p" count={1} onOpen={onOpen} />)
    fireEvent.click(screen.getByRole("button"))
    fireEvent.click(await screen.findByRole("button", { name: /src\/a\.ts/ }))
    expect(onOpen).toHaveBeenCalledWith("src/a.ts")
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })
  it("sans onOpen la ligne n'est pas un bouton", async () => {
    vi.spyOn(api, "gitChanges").mockResolvedValue({ files })
    render(<GitChanges dir="/p" count={1} />)
    fireEvent.click(screen.getByRole("button"))
    await screen.findByText("src/a.ts")
    expect(screen.queryByRole("button", { name: /src\/a\.ts/ })).toBeNull()
  })
})
