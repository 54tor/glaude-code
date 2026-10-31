// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type Device } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { setAuth } from "@/lib/auth"
import { TooltipProvider } from "@/components/ui/tooltip"
import { loopback, PairButton, TunnelHint } from "./PairDevice"

const flush = () => act(async () => {})
const device = (overrides: Partial<Device> = {}): Device => ({ id: "d1", name: "Téléphone d'alice", ip: "192.168.1.20", created: 1, seen: Date.now() / 1000, ...overrides })
const pairing = (overrides: Partial<{ code: string; url: string; ttl: number }> = {}) => ({ code: "12345678", url: "https://gld.example.com/pair?c=12345678", ttl: 120, ...overrides })

const open = async () => {
  render(
    <TooltipProvider>
      <PairButton />
    </TooltipProvider>
  )
  fireEvent.click(screen.getByRole("button", { name: "Connecter un appareil" }))
  await flush()
}

beforeEach(() => {
  setAuth({ state: "open" })
  vi.spyOn(api, "pairCode").mockResolvedValue(pairing())
  vi.spyOn(api, "devices").mockResolvedValue({ devices: [] })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
  setAuth({ state: "loading" })
})

describe("loopback", () => {
  it.each(["http://localhost:8799/", "http://app.localhost/", "https://127.0.0.1:8799/x", "http://[::1]:80/"])("%s est une adresse de bouclage", (url) => {
    expect(loopback(url)).toBe(true)
  })

  it.each(["http://192.168.1.20:8799/", "https://gld.example.com/", "https://notlocalhost.example.com/"])("%s est joignable d'un autre appareil", (url) => {
    expect(loopback(url)).toBe(false)
  })
})

describe("TunnelHint", () => {
  it("propose la commande cloudflared avec l'origine de gld", () => {
    render(<TunnelHint origin="http://127.0.0.1:8799" />)
    expect(screen.getByText("cloudflared tunnel --url http://127.0.0.1:8799")).toBeTruthy()
  })
})

describe("PairButton", () => {
  it("masqué tant que l'authentification n'est pas établie", () => {
    setAuth({ state: "anon" })
    render(
      <TooltipProvider>
        <PairButton />
      </TooltipProvider>
    )
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("demande un code à l'ouverture et l'affiche en deux groupes de quatre chiffres", async () => {
    vi.useFakeTimers()
    await open()
    expect(api.pairCode).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText("Code de connexion").textContent).toBe("1234 5678")
    expect(screen.getByText("Valable encore 120 s")).toBeTruthy()
    expect(screen.getByTitle("QR code de connexion")).toBeTruthy()
  })

  it("le compte à rebours décroît puis le code expire et propose d'en demander un autre", async () => {
    vi.useFakeTimers()
    vi.mocked(api.pairCode).mockResolvedValue(pairing({ ttl: 3 }))
    await open()
    await act(async () => void vi.advanceTimersByTime(1000))
    expect(screen.getByText("Valable encore 2 s")).toBeTruthy()
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(screen.getByText("Code expiré")).toBeTruthy()
    expect(screen.queryByLabelText("Code de connexion")).toBeNull()
    vi.mocked(api.pairCode).mockResolvedValue(pairing({ code: "87654321" }))
    fireEvent.click(screen.getByRole("button", { name: /Nouveau code/ }))
    await flush()
    expect(screen.getByLabelText("Code de connexion").textContent).toBe("8765 4321")
  })

  it("délai absent de la réponse : 120 s par défaut", async () => {
    vi.useFakeTimers()
    vi.mocked(api.pairCode).mockResolvedValue({ code: "11112222", url: "https://gld.example.com/" } as never)
    await open()
    expect(screen.getByText("Valable encore 120 s")).toBeTruthy()
  })

  it("adresse de bouclage : la passerelle explique comment atteindre gld depuis un autre appareil, sans QR", async () => {
    vi.mocked(api.pairCode).mockResolvedValue(pairing({ url: "http://127.0.0.1:8799/pair?c=1" }))
    await open()
    expect(screen.getByText("cloudflared tunnel --url http://127.0.0.1:8799")).toBeTruthy()
    expect(screen.queryByTitle("QR code de connexion")).toBeNull()
  })

  it("échec de création du code : toast d'erreur et code expiré", async () => {
    vi.mocked(api.pairCode).mockRejectedValue(new ApiError(500, "boom", "serveur indisponible"))
    const toastError = vi.spyOn(toast, "error").mockReturnValue("")
    await open()
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("serveur indisponible"))
    expect(screen.getByText("Code expiré")).toBeTruthy()
  })

  it("mode jeton : liste les appareils jumelés et révoque puis relit la liste", async () => {
    vi.mocked(api.devices).mockResolvedValueOnce({ devices: [device(), device({ id: "d2", name: "" })] }).mockResolvedValueOnce({ devices: [device({ id: "d2", name: "" })] })
    const revokeSpy = vi.spyOn(api, "deviceRevoke").mockResolvedValue(undefined as never)
    await open()
    expect(screen.getByText("Téléphone d'alice")).toBeTruthy()
    expect(screen.getByText("Appareil inconnu")).toBeTruthy()
    fireEvent.click(screen.getAllByRole("button", { name: "Déconnecter" })[0])
    await flush()
    expect(revokeSpy).toHaveBeenCalledWith("d1")
    expect(screen.queryByText("Téléphone d'alice")).toBeNull()
  })

  it("mode jeton sans appareil : message dédié", async () => {
    await open()
    expect(screen.getByText("Aucun appareil jumelé.")).toBeTruthy()
  })

  it("échec de la révocation : toast, la liste reste", async () => {
    vi.mocked(api.devices).mockResolvedValue({ devices: [device()] })
    vi.spyOn(api, "deviceRevoke").mockRejectedValue(new ApiError(500, "boom", "révocation refusée"))
    const toastError = vi.spyOn(toast, "error").mockReturnValue("")
    await open()
    fireEvent.click(screen.getByRole("button", { name: "Déconnecter" }))
    await flush()
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("révocation refusée"))
    expect(screen.getByText("Téléphone d'alice")).toBeTruthy()
  })

  it("mode comptes : pas de liste d'appareils, renvoi vers les sessions du profil", async () => {
    setAuth({ state: "ok" })
    await open()
    expect(api.devices).not.toHaveBeenCalled()
    expect(screen.queryByText("Appareils jumelés")).toBeNull()
    expect(screen.getByText(/Profil › Sessions/)).toBeTruthy()
  })
})
