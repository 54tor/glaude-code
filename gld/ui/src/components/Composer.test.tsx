// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"
import "@/i18n"
import { api, type QItem } from "@/lib/api"
import { draftOf, setDraft } from "@/hooks/useDraft"
import { Composer } from "./Composer"

vi.mock("./actions", () => ({ useActions: () => ({ background: new Set(), live: new Set() }) }))
vi.mock("@/lib/auth", async (orig) => ({ ...(await orig<typeof import("@/lib/auth")>()), useCan: () => () => true }))

const item = (o: Partial<QItem>): QItem => ({ id: "i1", text: "bonjour", author: "host", state: "queued", at: 1, ...o })
const rmOk = (text: string) => ({ ok: true, out: "", text }) as Awaited<ReturnType<typeof api.queueRm>>

function mount(items: QItem[], thinking = false) {
  vi.spyOn(api, "queue").mockResolvedValue({ items })
  vi.spyOn(api, "sendInfo").mockResolvedValue({ ids: ["s1"], why: "" })
  vi.spyOn(api, "slashCommands").mockResolvedValue({ commands: [] })
  vi.spyOn(api, "screen").mockResolvedValue({ ok: true, text: "" })
  const st = { open: true, thinking } as never
  return render(<Composer dir="/d" id="s1" args="" defaultChoice={{ model: "", effort: "" } as never} live liveState={{ st, open: true }} onSent={() => {}} turn={null} setTurn={() => {}} onDone={() => {}} />)
}

beforeEach(() => setDraft("s1", { text: "" }))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("Composer, file serveur", () => {
  it("montre un item incertain avec renvoyer et retirer", async () => {
    mount([item({ state: "uncertain" })])
    await screen.findByText("bonjour")
    const retry = vi.spyOn(api, "queueRetry").mockResolvedValue({ ok: true, out: "" })
    fireEvent.click(screen.getByRole("button", { name: /renvoyer|resend/i }))
    await waitFor(() => expect(retry).toHaveBeenCalledWith("s1", "i1"))
    expect(screen.getByRole("button", { name: /retirer|remove/i })).toBeTruthy()
  })
  it("retirer remet le texte dans le brouillon", async () => {
    mount([item({})])
    await screen.findByText("bonjour")
    const rm = vi.spyOn(api, "queueRm").mockResolvedValue(rmOk("bonjour"))
    setDraft("s1", { text: "déjà là" })
    fireEvent.click(screen.getByRole("button", { name: /retirer|remove/i }))
    await waitFor(() => expect(draftOf("s1").text).toBe("déjà là\nbonjour"))
    expect(rm).toHaveBeenCalledWith("s1", "i1")
  })
  it("affiche le badge d'un item d'invité", async () => {
    mount([item({ author: "guest" })])
    await screen.findByText(/invité|guest/i)
  })
  it("une erreur du serveur à l'ajout affiche un toast et garde le brouillon", async () => {
    mount([], true)
    vi.spyOn(api, "queueAdd").mockResolvedValue({ ok: false, out: "file pleine" })
    const err = vi.spyOn(toast, "error").mockImplementation(() => "")
    const field = await screen.findByPlaceholderText(/Écrire à la session|Write to the session/i)
    fireEvent.change(field, { target: { value: "salut" } })
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true })
    await waitFor(() => expect(err).toHaveBeenCalledWith("file pleine"))
    expect(draftOf("s1").text).toBe("salut")
  })
})
