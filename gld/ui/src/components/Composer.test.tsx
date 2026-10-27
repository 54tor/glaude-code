// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { api, type QItem } from "@/lib/api"
import { setAuth } from "@/lib/auth"
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
    await screen.findByText(i18n.t("conversation.composer.sendUncertain"))
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
  it("montre le libellé de l'invité quand il est connu", async () => {
    mount([item({ author: "guest", guest: "i1", label: "Léa" })])
    await screen.findByText("Léa")
    expect(screen.queryByText(/^invité$|^guest$/i)).toBeNull()
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

  it("un item en cours d'envoi n'a pas de bouton retirer, et seul lui porte « part… »", async () => {
    mount([item({ id: "a", text: "un", state: "sending" }), item({ id: "b", text: "deux" })])
    await screen.findByText("un")
    expect(screen.getAllByRole("button", { name: /retirer|remove/i })).toHaveLength(1)
    expect(screen.getAllByText(i18n.t("conversation.composer.sending"))).toHaveLength(1)
  })
  it("deux Entrée rapprochées ne mettent le message qu'une fois en file", async () => {
    mount([], true)
    let release = () => {}
    const add = vi.spyOn(api, "queueAdd").mockImplementation(() => new Promise((r) => (release = () => r({ ok: true, out: "" }))))
    const field = await screen.findByPlaceholderText(/Écrire à la session|Write to the session/i)
    fireEvent.change(field, { target: { value: "salut" } })
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true })
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true })
    release()
    await waitFor(() => expect(draftOf("s1").text).toBe(""))
    expect(add).toHaveBeenCalledTimes(1)
  })
})

const chatOk = () => new Response('{"done":true,"ok":true}\n', { status: 200 })
function mountPlain(items: QItem[]) {
  vi.spyOn(api, "queue").mockResolvedValue({ items })
  return render(<Composer dir="/d" id="s1" args="" defaultChoice={{ model: "", effort: "" } as never} live={false} liveState={{ st: null, open: false }} onSent={() => {}} turn={null} setTurn={() => {}} onDone={() => {}} />)
}

describe("Composer claude -p, file serveur", () => {
  beforeEach(() => setAuth({ state: "open", csrf: "" }))
  afterEach(() => vi.unstubAllGlobals())
  it("ne part pas tout seul après un tour : la file reste suspendue", async () => {
    const f = vi.fn(async () => chatOk())
    vi.stubGlobal("fetch", f)
    const rm = vi.spyOn(api, "queueRm")
    mountPlain([item({})])
    const field = await screen.findByPlaceholderText(/Écrire à Claude|Write to Claude/i)
    fireEvent.change(field, { target: { value: "salut" } })
    fireEvent.keyDown(field, { key: "Enter", ctrlKey: true })
    await waitFor(() => expect(f).toHaveBeenCalledTimes(1))
    await new Promise((r) => setTimeout(r, 50))
    expect(rm).not.toHaveBeenCalled()
    expect(f).toHaveBeenCalledTimes(1)
  })
  it("« envoyer le suivant » : texte au brouillon d'abord, retiré, envoyé, brouillon rendu", async () => {
    const f = vi.fn(async () => chatOk())
    vi.stubGlobal("fetch", f)
    setDraft("s1", { text: "avant" })
    mountPlain([item({})])
    await screen.findByText("bonjour")
    let seen = ""
    vi.spyOn(api, "queueRm").mockImplementation(async () => ((seen = draftOf("s1").text), rmOk("bonjour")))
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.composer.sendNext") }))
    await waitFor(() => expect(f).toHaveBeenCalled())
    expect(seen).toBe("avant\nbonjour")
    expect(JSON.parse(String((f.mock.calls[0] as unknown as [string, RequestInit])[1].body)).prompt).toBe("bonjour")
    await waitFor(() => expect(draftOf("s1").text).toBe("avant"))
  })
  it("un envoi qui échoue laisse le texte dans le brouillon, sans le remettre en file", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 500 })))
    mountPlain([item({})])
    await screen.findByText("bonjour")
    vi.spyOn(api, "queueRm").mockResolvedValue(rmOk("bonjour"))
    const add = vi.spyOn(api, "queueAdd")
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.composer.sendNext") }))
    await waitFor(() => expect(draftOf("s1").text).toBe("bonjour"))
    await new Promise((r) => setTimeout(r, 50))
    expect(add).not.toHaveBeenCalled()
  })
  it("une tête incertaine n'est pas reprise : renvoyer ou retirer seulement", async () => {
    mountPlain([item({ state: "uncertain" })])
    await screen.findByText(i18n.t("conversation.composer.sendUncertain"))
    expect(screen.queryByRole("button", { name: i18n.t("conversation.composer.sendNext") })).toBeNull()
    expect(screen.getByRole("button", { name: i18n.t("conversation.composer.resend") })).toBeTruthy()
    expect(screen.getByRole("button", { name: i18n.t("conversation.composer.dequeue") })).toBeTruthy()
  })
})
