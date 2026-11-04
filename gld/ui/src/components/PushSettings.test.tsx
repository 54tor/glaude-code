// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setAuth } from "@/lib/auth"
import * as push from "@/lib/push"
import { PushButton, PushSection } from "./PushSettings"
import { TooltipProvider } from "@/components/ui/tooltip"

vi.mock("@/lib/push", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/push")>()),
  pushSupported: vi.fn(),
  currentPushState: vi.fn(),
  subscribePush: vi.fn(),
  unsubscribePush: vi.fn(),
}))

const flush = () => act(async () => {})

const ouvrir = async () => {
  render(<PushSection />)
  await flush()
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
  setAuth({ state: "open" })
  vi.mocked(push.pushSupported).mockReturnValue(true)
  vi.mocked(push.currentPushState).mockResolvedValue({ subscribed: false, asking: false, waiting: false })
  vi.mocked(push.subscribePush).mockResolvedValue(undefined)
  vi.mocked(push.unsubscribePush).mockResolvedValue(undefined)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  setAuth({ state: "loading" })
})

describe("PushSection", () => {
  it("explique l'installation quand le Web Push n'est pas disponible", async () => {
    vi.mocked(push.pushSupported).mockReturnValue(false)

    await ouvrir()

    expect(screen.queryAllByRole("switch")).toHaveLength(0)
    expect(screen.getByText(/écran d'accueil/i)).toBeTruthy()
  })

  it("active les notifications avec les deux types par défaut", async () => {
    await ouvrir()

    fireEvent.click(screen.getAllByRole("switch")[0])
    await flush()

    expect(push.subscribePush).toHaveBeenCalledWith({ asking: true, waiting: true })
  })

  it("change un type en conservant l'autre", async () => {
    vi.mocked(push.currentPushState).mockResolvedValue({ subscribed: true, asking: true, waiting: true })
    await ouvrir()

    fireEvent.click(screen.getAllByRole("switch")[2])
    await flush()

    expect(push.subscribePush).toHaveBeenCalledWith({ asking: true, waiting: false })
  })

  it("désactive via unsubscribePush", async () => {
    vi.mocked(push.currentPushState).mockResolvedValue({ subscribed: true, asking: true, waiting: true })
    await ouvrir()

    fireEvent.click(screen.getAllByRole("switch")[0])
    await flush()

    expect(push.unsubscribePush).toHaveBeenCalled()
  })

  it("laisse les cases de types inactives tant que l'appareil n'est pas abonné", async () => {
    await ouvrir()

    const interrupteurs = screen.getAllByRole("switch")

    expect((interrupteurs[1] as HTMLButtonElement).disabled).toBe(true)
    expect((interrupteurs[2] as HTMLButtonElement).disabled).toBe(true)
  })
})

describe("PushButton", () => {
  it("ouvre les réglages de notifications depuis le pied de la barre latérale", async () => {
    render(
      <TooltipProvider>
        <PushButton />
      </TooltipProvider>
    )

    fireEvent.click(screen.getByRole("button", { name: "Notifications" }))
    await flush()

    expect(screen.getAllByRole("switch")).toHaveLength(3)
  })
})
