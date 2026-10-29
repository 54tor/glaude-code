// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { Compose, type ComposeAsk } from "./Compose"

vi.mock("./actions", () => ({ useActions: () => ({ home: "/home/u", pickDir: async () => null }) }))
vi.mock("./ModelPicker", () => ({ ModelPicker: () => null }))
vi.mock("./SnippetMenu", () => ({ SnippetMenu: () => null }))
vi.mock("@/lib/auth", async (orig) => ({
  ...(await orig<typeof import("@/lib/auth")>()),
  authFetch: async () => new Response(JSON.stringify({ path: "/home/u/.claude/gld-web-images/p.png", message: "" }), { status: 201 }),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Compose, pièces jointes", () => {
  it("un fichier collé part en « @chemin » à la suite du texte, même sans texte", async () => {
    vi.spyOn(api, "memory").mockResolvedValue({ global: [], projects: [] } as never)
    const run = vi.fn().mockResolvedValue(true)
    const ask = { id: "a", title: "T", subtitle: "s", project: "/p", send: "Go", choice: { model: "", effort: "" }, run } as ComposeAsk
    render(<Compose ask={ask} onClose={() => {}} />)
    const area = await screen.findByRole("textbox")
    const go = screen.getByRole("button", { name: /go/i }) as HTMLButtonElement
    expect(go.disabled).toBe(true)
    fireEvent.paste(area, { clipboardData: { files: [new File(["x"], "p.png", { type: "image/png" })] } })
    await waitFor(() => expect(go.disabled).toBe(false))
    fireEvent.click(go)
    await waitFor(() => expect(run).toHaveBeenCalled())
    expect(run.mock.calls[0][0]).toMatch(/^@.*\/gld-web-images\/p\.png$/)
  })
})
