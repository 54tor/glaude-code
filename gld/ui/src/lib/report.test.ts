import { beforeEach, describe, expect, it, vi } from "vitest"

const success = vi.fn()
const error = vi.fn()
vi.mock("sonner", () => ({ toast: { success: (...a: unknown[]) => success(...a), error: (...a: unknown[]) => error(...a) } }))

const { reportAction, reportResult } = await import("./report")
const { ApiError } = await import("./apiError")

describe("reportResult", () => {
  beforeEach(() => {
    success.mockClear()
    error.mockClear()
  })
  it("met la première ligne en titre et le reste en description", () => {
    expect(reportResult({ ok: true, out: "Enregistré\n\nsauvegarde : a.bak\n" })).toBe(true)
    expect(success).toHaveBeenCalledWith("Enregistré", { id: undefined, description: "sauvegarde : a.bak" })
  })
  it("prend la dernière ligne avec pick last et garde l'id du toast", () => {
    expect(reportResult({ ok: false, out: "détail\nÉchec" }, { id: 7, pick: "last" })).toBe(false)
    expect(error).toHaveBeenCalledWith("Échec", { id: 7, description: "détail" })
  })
  it("donne un titre même sans sortie", () => {
    reportResult({ ok: true, out: "  " })
    expect(success.mock.calls[0][0]).toBeTruthy()
    expect(success.mock.calls[0][1].description).toBeUndefined()
  })
})

describe("reportAction", () => {
  beforeEach(() => {
    success.mockClear()
    error.mockClear()
  })
  it("succès : le message de la réponse", async () => {
    expect(await reportAction(async () => ({ message: "Fait\ndétail" }))).toBe(true)
    expect(success).toHaveBeenCalledWith("Fait", { id: undefined, description: "détail" })
  })
  it("échec : le message de l'ApiError", async () => {
    expect(await reportAction(async () => Promise.reject(new ApiError(409, "conflict", "déjà là")), { id: 3 })).toBe(false)
    expect(error).toHaveBeenCalledWith("déjà là", { id: 3, description: undefined })
  })
})
