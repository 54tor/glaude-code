import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/http", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/http")>()),
  http: { get: vi.fn(), post: vi.fn(), del: vi.fn() },
}))

import { ApiError } from "@/lib/apiError"
import { http } from "@/lib/http"
import { followGitPush, streamGitPush, type PushRunInfo } from "./gitPushStream"

const runInfo = (overrides: Partial<PushRunInfo> = {}): PushRunInfo => ({
  id: "run1",
  dir: "/depot",
  branch: "main",
  remotes: [{ name: "origin", state: "succeeded", exit: 0 }],
  done: true,
  started: 1,
  ...overrides,
})
const page = (text: string | null, next: number, run: PushRunInfo) => ({
  out: text === null ? null : btoa(text),
  next,
  run,
})

describe("suivi d'une poussée durable", () => {
  afterEach(() => vi.resetAllMocks())

  it("transmet la sortie du hook puis rend le code de retour", async () => {
    const failedRun = runInfo({
      remotes: [{ name: "origin", state: "failed", exit: 1 }],
    })
    vi.mocked(http.get)
      .mockResolvedValueOnce(page("hook refuse\n", 12, failedRun))
      .mockResolvedValueOnce(page(null, 12, failedRun))
    const receivedChunks: string[] = []

    const exitCode = await followGitPush("run1", (chunk) =>
      receivedChunks.push(new TextDecoder().decode(chunk))
    )

    expect(receivedChunks).toEqual(["hook refuse\n"])
    expect(exitCode).toBe(1)
  })

  it("reprend au décalage déjà lu", async () => {
    vi.mocked(http.get)
      .mockResolvedValueOnce(page("a", 1, runInfo()))
      .mockResolvedValueOnce(page(null, 1, runInfo()))

    await followGitPush("run1", () => {})

    expect(vi.mocked(http.get).mock.calls.map((call) => call[0])).toEqual([
      "/git/push-runs/run1/output?offset=0",
      "/git/push-runs/run1/output?offset=1",
    ])
  })

  it("retente quand gld est injoignable, la poussée continuant dans tmux", async () => {
    vi.useFakeTimers()
    vi.mocked(http.get)
      .mockRejectedValueOnce(new ApiError(0, "net", "réseau", { net: true }))
      .mockResolvedValueOnce(page(null, 0, runInfo()))

    const pending = followGitPush("run1", () => {})
    await vi.advanceTimersByTimeAsync(2000)

    expect(await pending).toBe(0)
    vi.useRealTimers()
  })

  it("retente sur un 404 passager puis abandonne s'il persiste", async () => {
    vi.useFakeTimers()
    const notFound = new ApiError(404, "not_found", "introuvable")
    vi.mocked(http.get)
      .mockRejectedValueOnce(notFound)
      .mockResolvedValueOnce(page("suite\n", 6, runInfo()))
      .mockResolvedValueOnce(page(null, 6, runInfo()))
    const receivedChunks: string[] = []
    const recovered = followGitPush("run1", (chunk) =>
      receivedChunks.push(new TextDecoder().decode(chunk))
    )
    await vi.advanceTimersByTimeAsync(2000)
    expect(await recovered).toBe(0)
    expect(receivedChunks).toEqual(["suite\n"])

    vi.mocked(http.get).mockReset().mockRejectedValue(notFound)
    const persistent = followGitPush("run1", () => {})
    const outcome = persistent.catch((error: unknown) => error)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(await outcome).toBe(notFound)
    vi.useRealTimers()
  })

  it("signale une poussée interrompue plutôt qu'un état indéterminé", async () => {
    vi.mocked(http.get).mockResolvedValue(
      page(
        null,
        0,
        runInfo({
          done: false,
          interrupted: true,
          remotes: [{ name: "origin", state: "running" }],
        })
      )
    )

    await expect(followGitPush("run1", () => {})).rejects.toThrow()
  })

  it("lève l'erreur de l'API quand la création est refusée", async () => {
    vi.mocked(http.post).mockRejectedValue(
      new ApiError(409, "conflict", "déjà en cours")
    )

    await expect(streamGitPush("/depot", ["origin"], () => {})).rejects.toThrow(
      "déjà en cours"
    )
  })
})
