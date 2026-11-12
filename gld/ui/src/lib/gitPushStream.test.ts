import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/auth", () => ({ authFetch: vi.fn() }))

import { authFetch } from "@/lib/auth"
import { streamGitPush } from "./gitPushStream"

const base64Of = (text: string) => btoa(text)

const streamedResponse = (lines: string[]) => new Response(lines.map((line) => line + "\n").join(""), { status: 200 })

describe("streamGitPush", () => {
  afterEach(() => vi.mocked(authFetch).mockReset())

  it("transmet la sortie du hook puis rend le code de retour", async () => {
    vi.mocked(authFetch).mockResolvedValue(
      streamedResponse([JSON.stringify({ out: base64Of("hook refuse\n") }), JSON.stringify({ exit: 1 })])
    )
    const receivedChunks: string[] = []

    const exitCode = await streamGitPush("/depot", "origin", (chunk) => receivedChunks.push(new TextDecoder().decode(chunk)))

    expect(receivedChunks).toEqual(["hook refuse\n"])
    expect(exitCode).toBe(1)
  })

  it("rend 0 quand le push réussit", async () => {
    vi.mocked(authFetch).mockResolvedValue(streamedResponse([JSON.stringify({ exit: 0 })]))

    expect(await streamGitPush("/depot", "origin", () => {})).toBe(0)
  })

  it("lève une erreur quand le flux se termine sans code de retour", async () => {
    vi.mocked(authFetch).mockResolvedValue(streamedResponse([JSON.stringify({ out: base64Of("x") })]))

    await expect(streamGitPush("/depot", "origin", () => {})).rejects.toThrow()
  })

  it("lève l'erreur de l'API quand le serveur refuse avant le flux", async () => {
    vi.mocked(authFetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "bad_request", message: "dossier invalide" } }), { status: 400 })
    )

    await expect(streamGitPush("/depot", "origin", () => {})).rejects.toThrow("dossier invalide")
  })
})
