// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api, type Data } from "@/lib/api"
import { IdeView } from "./IdeView"

vi.mock("./CodeEditor", () => ({
  setTsLib: vi.fn(),
  clearTsLibs: vi.fn(),
  default: ({ value }: { value: string }) => (
    <pre data-testid="ed">{value}</pre>
  ),
}))
vi.mock("../FullTerminal", () => ({
  default: ({ id }: { id: string }) => <div data-testid="term">{id}</div>,
}))

class FakeES {
  static all: FakeES[] = []
  onopen: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  constructor() {
    FakeES.all.push(this)
  }
  close() {}
}
const ok = (body: object) => ({ status: 200, ok: true, body }) as never
let git: ReturnType<typeof vi.spyOn>
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  FakeES.all = []
  vi.stubGlobal("EventSource", FakeES)
  vi.spyOn(api, "fsIndex").mockResolvedValue(ok({ state: "ready", files: [] }))
  vi.spyOn(api, "fsTree").mockImplementation(async (_d, p) => ({
    entries:
      p === ""
        ? [
            { name: "src", type: "dir", size: 0, mtime: 1 },
            { name: "a.txt", type: "file", size: 1, mtime: 1 },
          ]
        : [],
    truncated: false,
  }))
  vi.spyOn(api, "fsFile").mockResolvedValue(
    ok({ content: "hello", etag: "e1", size: 5, mtime: 1 })
  )
  git = vi.spyOn(api, "gitChanges").mockResolvedValue({
    files: [
      { path: "a.txt", status: " M" },
      { path: "src/x.ts", status: "??" },
    ],
    more: 3,
  })
  vi.spyOn(api, "termInfo").mockResolvedValue({ enabled: true })
  vi.spyOn(api, "data").mockResolvedValue({
    live: ["s1"],
    groups: [
      {
        project: "p",
        missing: false,
        sessions: [{ id: "s1", cwd: "/p", mtime: 1 }],
      },
    ],
  } as unknown as Data)
})
afterEach(
  () => (
    cleanup(),
    vi.restoreAllMocks(),
    vi.unstubAllGlobals(),
    vi.useRealTimers()
  )
)

describe("IdeView marqueurs git et terminal", () => {
  it("lettre sur le fichier, point sur le dossier parent, « et N autres »", async () => {
    render(<IdeView dir="/p" />)
    expect(await screen.findByTitle("Modifié")).toBeTruthy()
    expect(
      screen.getByTitle("Contient des changements non validés")
    ).toBeTruthy()
    expect(screen.getByText("et 3 autres")).toBeTruthy()
  })
  it("un évènement disque relit l'état git, avec 1 s d'anti-rebond", async () => {
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(git).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(FakeES.all.length).toBe(1))
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const es = FakeES.all[0]
    const ev = (path: string) =>
      act(() =>
        es.onmessage?.({
          data: JSON.stringify({ path, type: "write" }),
        } as MessageEvent)
      )
    ev("a.txt")
    ev("b.txt")
    await act(async () => void vi.advanceTimersByTime(900))
    expect(git).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(200))
    expect(git).toHaveBeenCalledTimes(2)
  })
  it("zone basse repliée : ni session ni /term-open", async () => {
    localStorage.setItem("gld.ide./p.bottom", "")
    render(<IdeView dir="/p" />)
    await screen.findByTitle("Modifié")
    expect(screen.queryByTestId("term")).toBeNull()
    expect(api.termInfo).not.toHaveBeenCalled()
  })
  it("zone basse ouverte : le terminal de la session vivante est monté", async () => {
    render(<IdeView dir="/p" />)
    expect((await screen.findByTestId("term")).textContent).toBe("s1")
  })
})
