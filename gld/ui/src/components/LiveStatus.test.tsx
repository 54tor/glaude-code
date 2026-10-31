// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Detail, type LiveState, type Session, type SubLive } from "@/lib/api"
import { resetWarm } from "@/hooks/useKeepWarm"
import { ActionsCtx, type Actions } from "./actions"
import { LiveAgents, LiveBar, LiveStats, StatsButton, useLive, type Live } from "./LiveStatus"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const flush = () => act(async () => {})
const session: Session = { id: "s1", dir: "d1", title: "Migration REST", named: true, cwd: "/home/bob/p/sub", mtime: 1_700_000_000, kb: 1, tok: { in: 1200, out: 3400, cr: 9_000_000, cw: 100 } }
const sub = (overrides: Partial<SubLive> = {}): SubLive => ({ id: "a1", desc: "relecture", type: "reviewer", running: true, activity: "lit un fichier", context: 2500, out: 0, mtime: 0, ...overrides })
const liveState = (overrides: Partial<LiveState> = {}): LiveState => ({ open: true, status: "", activity: "", thinking: false, since: 0, context: 0, out: 0, model: "", subs: [], ...overrides })
const live = (state: Partial<LiveState> | null, open = true): Live => ({ st: state === null ? null : liveState(state), open })

const caps = { hotCache: false, keepWarm: false, config: true, memory: true, tuiPrompts: true, resume: true }
const actions = (overrides: Partial<{ live: string[]; rc: string[] }> = {}) =>
  ({ caps: () => caps, home: "/home/bob", live: new Set(overrides.live ?? ["s1"]), rc: new Set(overrides.rc ?? []), background: new Set(), waiting: new Set(), asking: new Set(), hotLeft: () => null }) as unknown as Actions
const withActions = (ui: React.ReactElement, overrides?: Parameters<typeof actions>[0]) => <ActionsCtx.Provider value={actions(overrides)}>{ui}</ActionsCtx.Provider>

beforeEach(() => {
  resetWarm()
  vi.spyOn(api, "keepWarm").mockResolvedValue({ items: {}, ttl: 0, below: 0 })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("useLive", () => {
  it("sans identifiant de session : aucune lecture", async () => {
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState())
    renderHook(() => useLive("d1", "", true))
    await flush()
    expect(readSpy).not.toHaveBeenCalled()
  })

  it("session ouverte : relue toutes les 2 s, seulement onglet visible", async () => {
    vi.useFakeTimers()
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState())
    renderHook(() => useLive("d1", "s1", true))
    await flush()
    expect(readSpy).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(readSpy).toHaveBeenCalledTimes(2)
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
    await act(async () => void vi.advanceTimersByTime(6000))
    expect(readSpy).toHaveBeenCalledTimes(2)
  })

  it("session fermée : lue une seule fois", async () => {
    vi.useFakeTimers()
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState())
    renderHook(() => useLive("d1", "s1", false))
    await flush()
    await act(async () => void vi.advanceTimersByTime(10_000))
    expect(readSpy).toHaveBeenCalledTimes(1)
  })

  it("un relevé identique garde le même objet : pas de nouveau rendu", async () => {
    vi.useFakeTimers()
    vi.spyOn(api, "sessionLive").mockImplementation(() => Promise.resolve(liveState({ activity: "x" })))
    const { result } = renderHook(() => useLive("d1", "s1", true))
    await flush()
    const firstState = result.current.st
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(result.current.st).toBe(firstState)
  })

  it("un relevé différent remplace l'état", async () => {
    vi.useFakeTimers()
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState({ activity: "a" }))
    const { result } = renderHook(() => useLive("d1", "s1", true))
    await flush()
    readSpy.mockResolvedValue(liveState({ activity: "b" }))
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(result.current.st?.activity).toBe("b")
  })

  it("arrête la relecture au démontage", async () => {
    vi.useFakeTimers()
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState())
    const { unmount } = renderHook(() => useLive("d1", "s1", true))
    await flush()
    unmount()
    await act(async () => void vi.advanceTimersByTime(10_000))
    expect(readSpy).toHaveBeenCalledTimes(1)
  })

  it("une erreur de lecture laisse l'état précédent", async () => {
    vi.useFakeTimers()
    const readSpy = vi.spyOn(api, "sessionLive").mockResolvedValue(liveState({ activity: "a" }))
    const { result } = renderHook(() => useLive("d1", "s1", true))
    await flush()
    readSpy.mockRejectedValue(new Error("réseau"))
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(result.current.st?.activity).toBe("a")
  })
})

describe("LiveBar", () => {
  it("rien sans relevé", () => {
    const { container } = render(<LiveBar live={live(null)} id="s1" />)
    expect(container.textContent).toBe("")
  })

  it("rien quand la liste des sessions ouvertes dit fermée, même si le relevé dit ouverte", () => {
    const { container } = render(<LiveBar live={live({ open: true }, false)} id="s1" />)
    expect(container.textContent).toBe("")
  })

  it("rien quand le relevé dit fermée", () => {
    const { container } = render(<LiveBar live={live({ open: false })} id="s1" />)
    expect(container.textContent).toBe("")
  })

  it("session en attente : « ouverte » et le dernier message de Claude", () => {
    render(<LiveBar live={live({ said: "Je termine ?" })} id="s1" />)
    expect(screen.getByText("ouverte")).toBeTruthy()
    expect(screen.getByText("Je termine ?")).toBeTruthy()
  })

  it("session qui réfléchit : « travaille » et le temps écoulé", () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(1_700_000_100_000))
    render(<LiveBar live={live({ thinking: true, since: 1_700_000_070 })} id="s1" />)
    expect(screen.getByText("travaille")).toBeTruthy()
    expect(screen.getByText("depuis 30 s")).toBeTruthy()
  })

  it("l'horloge avance chaque seconde tant que la session est ouverte", async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(1_700_000_100_000))
    render(<LiveBar live={live({ thinking: true, since: 1_700_000_040 })} id="s1" />)
    expect(screen.getByText("depuis 1 min 0 s")).toBeTruthy()
    await act(async () => void vi.advanceTimersByTime(5000))
    expect(screen.getByText("depuis 1 min 5 s")).toBeTruthy()
  })

  it("activité connue : affichée telle quelle", () => {
    render(<LiveBar live={live({ thinking: true, activity: "compile le projet" })} id="s1" />)
    expect(screen.getByText("compile le projet")).toBeTruthy()
  })

  it("formules tournantes : tirage stable pour une session et une tranche de 6 s", () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(1_700_000_100_000))
    const verbs = ["mijote", "rumine", "médite", "peaufine"]
    const first = render(<LiveBar live={live({ thinking: true, generic: true, verbs })} id="s1" />)
    const firstText = first.container.querySelector("span.truncate")?.textContent
    first.unmount()
    const second = render(<LiveBar live={live({ thinking: true, generic: true, verbs })} id="s1" />)
    expect(second.container.querySelector("span.truncate")?.textContent).toBe(firstText)
    expect(firstText).toMatch(/…$/)
    expect(verbs.map((verb) => verb + "…")).toContain(firstText)
  })

  it("outil en cours : le nom de l'outil est affiché et se déplie au clic", () => {
    render(<LiveBar live={live({ thinking: true, tool: "Bash: npm test", toolRunning: true })} id="s1" />)
    const toolButton = screen.getByTitle("Bash: npm test")
    expect(toolButton.getAttribute("aria-expanded")).toBe("false")
    expect(screen.getByLabelText("outil en cours")).toBeTruthy()
    fireEvent.click(toolButton)
    expect(toolButton.getAttribute("aria-expanded")).toBe("true")
  })

  it("outil terminé : libellé « dernier outil »", () => {
    render(<LiveBar live={live({ thinking: true, tool: "Read", toolRunning: false })} id="s1" />)
    expect(screen.getByLabelText("dernier outil")).toBeTruthy()
  })

  it("liste les sous-agents et tâches en cours, pas ceux terminés", () => {
    render(
      <LiveBar
        live={live({
          thinking: true,
          subs: [sub(), sub({ id: "a2", desc: "fini", running: false })],
          tasks: [{ id: "t1", desc: "npm run dev", status: "running", hasOutput: true }, { id: "t2", desc: "build", status: "completed", hasOutput: true }],
        })}
        id="s1"
      />
    )
    expect(screen.getByText("relecture")).toBeTruthy()
    expect(screen.getByText("npm run dev")).toBeTruthy()
    expect(screen.queryByText("fini")).toBeNull()
    expect(screen.queryByText("build")).toBeNull()
  })

  it("messages en file : marqués « en attente de lecture »", () => {
    render(<LiveBar live={live({ queue: [{ t: "continue", ts: "2026-10-31T10:00:00Z" }] })} id="s1" />)
    expect(screen.getByText("continue")).toBeTruthy()
    expect(screen.getByText("En attente de lecture par la session")).toBeTruthy()
  })
})

describe("StatsButton", () => {
  it("affiche le contexte et le total de tokens, et réagit au clic", () => {
    const onClick = vi.fn()
    render(<StatsButton live={live({ context: 45_000 })} s={session} onClick={onClick} />)
    const statsButton = screen.getByRole("button")
    expect(statsButton.textContent).toContain("ctx 45 k")
    expect(statsButton.textContent).toContain("4,7 k tok")
    fireEvent.click(statsButton)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it("sans contexte connu : pas de « ctx »", () => {
    render(<StatsButton live={live({ context: 0 })} s={session} onClick={() => {}} />)
    expect(screen.getByRole("button").textContent).not.toContain("ctx")
  })
})

describe("LiveStats", () => {
  const detail: Detail = { prompts: [], n: 12, branch: "feat/rest", models: ["claude-opus-5-5", "<synthetic>"], t0: "2026-10-30T08:00:00Z", t1: "2026-10-30T09:00:00Z", subagents: 3 }
  const mountStats = (state: Partial<LiveState> | null, options: { detail?: Detail | null; actionOverrides?: Parameters<typeof actions>[0] } = {}) =>
    render(withActions(<LiveStats live={live(state)} s={session} project="/home/bob/p" detail={options.detail ?? null} shown onShown={() => {}} />, options.actionOverrides))

  it("faits de la session : chemins abrégés en ~, état ouvert, remote control", async () => {
    mountStats({}, { detail, actionOverrides: { live: ["s1"], rc: ["s1"] } })
    await flush()
    expect(screen.getByText("~/p/sub")).toBeTruthy()
    expect(screen.getByText("~/p")).toBeTruthy()
    expect(screen.getByText("Ouverte")).toBeTruthy()
    expect(screen.getByText("Actif")).toBeTruthy()
    expect(screen.getByText("feat/rest")).toBeTruthy()
    expect(screen.getByText("12")).toBeTruthy()
    expect(screen.getByText("claude-opus-5-5")).toBeTruthy()
  })

  it("session fermée : « Fermée » et pas de ligne remote control", async () => {
    mountStats({}, { actionOverrides: { live: [] } })
    await flush()
    expect(screen.getByText("Fermée")).toBeTruthy()
    expect(screen.queryByText("Remote control")).toBeNull()
  })

  it("consommation en tokens abrégés", async () => {
    mountStats({})
    await flush()
    expect(screen.getByText("1,2 k")).toBeTruthy()
    expect(screen.getByText("9 M")).toBeTruthy()
    expect(screen.getByText("Traité : 4,7 k tokens (hors cache lu)")).toBeTruthy()
  })

  it("dernier appel : modèle, contexte et sortie, seulement si connus", async () => {
    mountStats({ model: "claude-opus-5-5", context: 80_000, out: 900 })
    await flush()
    expect(screen.getByText("Dernier appel")).toBeTruthy()
    expect(screen.getByText(/Contexte envoyé : ≈ 80 k tokens/)).toBeTruthy()
    expect(screen.getByText("Tokens produits : 900")).toBeTruthy()
  })

  it("aucun appel connu : pas de section « Dernier appel »", async () => {
    mountStats({})
    await flush()
    expect(screen.queryByText("Dernier appel")).toBeNull()
  })

  it("sous-agents : liste, puis conversation du sous-agent choisi", async () => {
    const convSpy = vi.spyOn(api, "conv").mockResolvedValue({ msgs: [{ r: "assistant", t: "Résultat de la relecture", o: 1 }] })
    mountStats({ subs: [sub()] })
    await flush()
    fireEvent.click(screen.getByTitle("Ouvrir la conversation de ce sous-agent"))
    await flush()
    expect(convSpy).toHaveBeenCalledWith({ dir: "d1", id: "s1", sub: "a1" }, false, { limit: 300 })
    expect(screen.getByText("Résultat de la relecture")).toBeTruthy()
  })
})

describe("LiveAgents", () => {
  it("rien sans sous-agent", () => {
    const { container } = render(<LiveAgents live={live({ subs: [] })} dir="d1" id="s1" />)
    expect(container.textContent).toBe("")
  })

  it("le bouton compte les sous-agents et ceux en cours", () => {
    render(<LiveAgents live={live({ subs: [sub(), sub({ id: "a2", running: false })] })} dir="d1" id="s1" />)
    expect(screen.getByRole("button").textContent).toContain("Sous-agents · 2")
    expect(screen.getByRole("button").textContent).toContain("(1 en cours)")
  })

  it("le titre de la fenêtre dit combien tournent", async () => {
    render(<LiveAgents live={live({ subs: [sub(), sub({ id: "a2" })] })} dir="d1" id="s1" />)
    fireEvent.click(screen.getByRole("button"))
    await flush()
    expect(screen.getByRole("dialog", { name: "Sous-agents (2, 2 en cours)" })).toBeTruthy()
  })

  it("seul un sous-agent en cours propose l'arrêt, qui passe par TaskStop", async () => {
    const sendSpy = vi.spyOn(api, "sessionSend").mockResolvedValue({} as never)
    vi.spyOn(toast, "success").mockReturnValue("")
    render(<LiveAgents live={live({ subs: [sub(), sub({ id: "a2", desc: "ancien", running: false })] })} dir="d1" id="s1" />)
    fireEvent.click(screen.getByRole("button"))
    await flush()
    const stopButtons = screen.getAllByRole("button", { name: "Arrêter ce sous-agent" })
    expect(stopButtons).toHaveLength(1)
    fireEvent.click(stopButtons[0])
    await flush()
    expect(sendSpy).toHaveBeenCalledWith("s1", "Arrête avec l'outil TaskStop la tâche a1 (relecture), sans autre commentaire.")
  })

  it("la conversation du sous-agent : bascule outils masqués/affichés et relecture", async () => {
    const convSpy = vi.spyOn(api, "conv").mockResolvedValue({ msgs: [] })
    render(<LiveAgents live={live({ subs: [sub({ running: false })] })} dir="d1" id="s1" />)
    fireEvent.click(screen.getByRole("button"))
    await flush()
    fireEvent.click(screen.getByTitle("Ouvrir la conversation de ce sous-agent"))
    await flush()
    expect(screen.getByText("Aucun message.")).toBeTruthy()
    const toolsToggle = screen.getByRole("button", { name: /outils/i })
    expect(toolsToggle.getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(toolsToggle)
    await flush()
    expect(convSpy).toHaveBeenLastCalledWith({ dir: "d1", id: "s1", sub: "a1" }, true, { limit: 300 })
    expect(toolsToggle.getAttribute("aria-pressed")).toBe("true")
  })

  it("sous-agent en cours : conversation relue toutes les 3 s", async () => {
    vi.useFakeTimers()
    const convSpy = vi.spyOn(api, "conv").mockResolvedValue({ msgs: [] })
    render(<LiveAgents live={live({ subs: [sub()] })} dir="d1" id="s1" />)
    fireEvent.click(screen.getByRole("button"))
    await flush()
    fireEvent.click(screen.getByTitle("Ouvrir la conversation de ce sous-agent"))
    await flush()
    expect(convSpy).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(3000))
    expect(convSpy).toHaveBeenCalledTimes(2)
  })
})
