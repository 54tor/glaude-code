// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Caps, type Session, type WarmState } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { resetWarm } from "@/hooks/useKeepWarm"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { ActionsCtx, type Actions } from "./actions"
import { CacheState, KeepWarmBanner, KeepWarmItem } from "./KeepWarm"

// Radix Select/Dropdown mesure ses éléments ; jsdom n'a ni ResizeObserver ni pointer capture.
class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}
Element.prototype.hasPointerCapture = () => false
Element.prototype.releasePointerCapture = () => {}

const session: Session = { id: "s1", dir: "d1", title: "t", named: true, cwd: "/home/bob/p", mtime: 1, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 } }
const fullCaps: Caps = { hotCache: true, keepWarm: true, config: true, memory: true, tuiPrompts: true, resume: true }
const warmState = (overrides: Partial<WarmState> = {}): WarmState => ({ dir: "d1", on: true, pings: 2, max: 6, lastPing: 0, ...overrides })

type ActionOverrides = { caps?: Caps; hotLeft?: number | null; live?: string[]; background?: string[]; waiting?: string[]; asking?: string[] }
const buildActions = (overrides: ActionOverrides = {}): Actions =>
  ({
    caps: () => overrides.caps ?? fullCaps,
    hotLeft: () => (overrides.hotLeft === undefined ? 30 : overrides.hotLeft),
    live: new Set(overrides.live ?? ["s1"]),
    background: new Set(overrides.background ?? []),
    waiting: new Set(overrides.waiting ?? ["s1"]),
    asking: new Set(overrides.asking ?? []),
  }) as unknown as Actions

const mountWith = async (ui: React.ReactElement, warm: Record<string, WarmState>, overrides?: ActionOverrides) => {
  vi.spyOn(api, "keepWarm").mockResolvedValue({ items: warm, ttl: 0, below: 0 })
  const view = render(<ActionsCtx.Provider value={buildActions(overrides)}>{ui}</ActionsCtx.Provider>)
  await act(async () => {})
  return view
}

beforeEach(() => resetWarm())
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("CacheState", () => {
  it("n'affiche rien si le fournisseur n'a pas de cache chaud", async () => {
    const { container } = await mountWith(<CacheState s={session} />, {}, { caps: { ...fullCaps, hotCache: false } })
    expect(container.textContent).toBe("")
  })

  it("cache chaud lointain : minutes restantes", async () => {
    await mountWith(<CacheState s={session} />, {}, { hotLeft: 42 })
    expect(screen.getByText("cache 42 min")).toBeTruthy()
  })

  it("cache qui expire sous le seuil d'alerte", async () => {
    await mountWith(<CacheState s={session} />, {}, { hotLeft: 7 })
    expect(screen.getByText("cache expire dans 7 min")).toBeTruthy()
  })

  it("cache froid", async () => {
    await mountWith(<CacheState s={session} />, {}, { hotLeft: null })
    expect(screen.getByText("cache froid")).toBeTruthy()
  })

  it("session qui travaille : l'état de travail prime sur le cache", async () => {
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], waiting: [], hotLeft: 7 })
    expect(screen.getByText("Claude travaille")).toBeTruthy()
  })

  it("session qui attend votre message : le délai de cache reste affiché", async () => {
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], hotLeft: 42 })
    expect(screen.getByText("cache 42 min")).toBeTruthy()
  })

  it("question ouverte : prime sur le travail", async () => {
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], waiting: [], asking: ["s1"] })
    expect(screen.queryByText("Claude travaille")).toBeNull()
    expect(screen.getByText("attend une réponse à une question")).toBeTruthy()
  })

  it("maintien actif : compteur de pings et délai, un clic l'arrête", async () => {
    const setSpy = vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    await mountWith(<CacheState s={session} />, { s1: warmState() }, { hotLeft: 20 })
    const chip = screen.getByRole("button", { name: /maintien actif · 2\/6 · cache 20 min/ })
    fireEvent.click(chip)
    await act(async () => {})
    expect(setSpy).toHaveBeenCalledWith({ dir: "d1", id: "s1", on: false, max: undefined, reheat: undefined })
  })

  it("sans capacité de maintien : simple pastille, pas un bouton", async () => {
    await mountWith(<CacheState s={session} />, {}, { caps: { ...fullCaps, keepWarm: false }, hotLeft: 42 })
    expect(screen.queryByRole("button")).toBeNull()
    expect(screen.getByText("cache 42 min").parentElement?.getAttribute("title")).toContain("cache 42 min")
  })

  it("session fermée : pastille non cliquable qui explique pourquoi", async () => {
    await mountWith(<CacheState s={session} />, {}, { live: [] })
    expect(screen.queryByRole("button")).toBeNull()
    expect(screen.getByText("cache 30 min").parentElement?.getAttribute("title")).toContain("Session fermée")
  })

  it("tâche de fond : pas de ping possible", async () => {
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], background: ["s1"], waiting: ["s1"] })
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("le menu propose les plafonds de pings et active avec le plafond choisi", async () => {
    const setSpy = vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], waiting: ["s1"], hotLeft: 30 })
    fireEvent.pointerDown(screen.getByRole("button"), { button: 0, ctrlKey: false })
    fireEvent.click(await screen.findByText("Au plus 12 pings (≈ 12 h)"))
    await act(async () => {})
    expect(setSpy).toHaveBeenCalledWith({ dir: "d1", id: "s1", on: true, max: 12, reheat: false })
  })

  it("cache froid : le menu annonce le coût de reconstruction et demande de réchauffer", async () => {
    const setSpy = vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    await mountWith(<CacheState s={session} />, {}, { live: ["s1"], waiting: ["s1"], hotLeft: null })
    fireEvent.pointerDown(screen.getByRole("button"), { button: 0, ctrlKey: false })
    expect(await screen.findByText(/Cache froid : un premier message/)).toBeTruthy()
    fireEvent.click(screen.getByText("Réchauffer, puis au plus 3 pings"))
    await act(async () => {})
    expect(setSpy).toHaveBeenCalledWith({ dir: "d1", id: "s1", on: true, max: 3, reheat: true })
  })
})

describe("KeepWarmBanner", () => {
  it("masqué sans maintien ni note", async () => {
    const { container } = await mountWith(<KeepWarmBanner s={session} />, {})
    expect(container.textContent).toBe("")
  })

  it("maintien actif : avertit du coût, compte les pings, bouton d'arrêt", async () => {
    const setSpy = vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    await mountWith(<KeepWarmBanner s={session} />, { s1: warmState({ pings: 1, max: 3 }) })
    expect(screen.getByText(/ça coûte de l'argent/)).toBeTruthy()
    expect(screen.getByText("1/3 ping")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Arrêter" }))
    await act(async () => {})
    expect(setSpy).toHaveBeenCalledWith(expect.objectContaining({ id: "s1", on: false }))
  })

  it("maintien arrêté avec une note : affiche la note sans bouton", async () => {
    await mountWith(<KeepWarmBanner s={session} />, { s1: warmState({ on: false, note: "plafond atteint" }) })
    expect(screen.getByText("plafond atteint")).toBeTruthy()
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("maintien actif avec une note d'erreur : les deux apparaissent", async () => {
    await mountWith(<KeepWarmBanner s={session} />, { s1: warmState({ note: "ping échoué" }) })
    expect(screen.getByText("ping échoué")).toBeTruthy()
    expect(screen.getByText(/ça coûte de l'argent/)).toBeTruthy()
  })

  it("rien si le fournisseur ne sait pas maintenir au chaud", async () => {
    const { container } = await mountWith(<KeepWarmBanner s={session} />, { s1: warmState() }, { caps: { ...fullCaps, keepWarm: false } })
    expect(container.textContent).toBe("")
  })

  it("l'échec de l'arrêt s'affiche en toast", async () => {
    vi.spyOn(api, "keepWarmSet").mockRejectedValue(new ApiError(500, "internal", "boom"))
    const toastError = vi.spyOn(toast, "error").mockReturnValue("")
    await mountWith(<KeepWarmBanner s={session} />, { s1: warmState() })
    fireEvent.click(screen.getByRole("button", { name: "Arrêter" }))
    await act(async () => {})
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("boom"))
  })

  it("après un changement, l'état est relu aussitôt", async () => {
    vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    const readSpy = vi.spyOn(api, "keepWarm").mockResolvedValue({ items: { s1: warmState() }, ttl: 0, below: 0 })
    render(<ActionsCtx.Provider value={buildActions()}><KeepWarmBanner s={session} /></ActionsCtx.Provider>)
    await act(async () => {})
    const callsBefore = readSpy.mock.calls.length
    readSpy.mockResolvedValue({ items: {}, ttl: 0, below: 0 })
    fireEvent.click(screen.getByRole("button", { name: "Arrêter" }))
    await act(async () => {})
    expect(readSpy.mock.calls.length).toBeGreaterThan(callsBefore)
    expect(screen.queryByText(/ça coûte de l'argent/)).toBeNull()
  })
})

describe("KeepWarmItem", () => {
  const menu = (item: React.ReactElement) => (
    <DropdownMenu open>
      <DropdownMenuTrigger>ouvrir</DropdownMenuTrigger>
      <DropdownMenuContent>{item}</DropdownMenuContent>
    </DropdownMenu>
  )

  it("rien sans capacité", async () => {
    const { container } = await mountWith(<KeepWarmItem s={session} />, {}, { caps: { ...fullCaps, keepWarm: false } })
    expect(container.textContent).toBe("")
  })

  it("maintien actif : l'entrée arrête le maintien", async () => {
    const setSpy = vi.spyOn(api, "keepWarmSet").mockResolvedValue({} as never)
    await mountWith(menu(<KeepWarmItem s={session} />), { s1: warmState() })
    fireEvent.click(await screen.findByText("Maintien actif : arrêter"))
    await act(async () => {})
    expect(setSpy).toHaveBeenCalledWith(expect.objectContaining({ id: "s1", on: false }))
  })

  it("session fermée : entrée désactivée", async () => {
    await mountWith(menu(<KeepWarmItem s={session} />), {}, { live: [] })
    const item = await screen.findByText("Maintenir au chaud")
    expect(item.closest("[role=menuitem]")?.getAttribute("aria-disabled")).toBe("true")
  })

  it("session ouverte : sous-menu des plafonds", async () => {
    await mountWith(menu(<KeepWarmItem s={session} />), {})
    const trigger = await screen.findByText("Maintenir au chaud")
    expect(trigger.closest("[role=menuitem]")?.getAttribute("aria-haspopup")).toBe("menu")
  })
})
