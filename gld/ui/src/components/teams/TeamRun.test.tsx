// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { setLang } from "@/i18n"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import * as teams from "@/lib/teams"
import { TeamRun } from "./TeamRun"

const rights = vi.hoisted(() => ({ exec: true }))
vi.mock("@/lib/auth", async (orig) => ({ ...(await orig<typeof import("@/lib/auth")>()), useCan: () => (r: string) => (r === "exec" ? rights.exec : true) }))
vi.mock("@/lib/teams")

setLang("fr")
const NAME = "session-0123abcd"
let width = 1200
class RO {
  cb: ResizeObserverCallback
  constructor(cb: ResizeObserverCallback) {
    this.cb = cb
  }
  observe(el: Element) {
    this.cb([{ target: el, contentRect: { width } } as ResizeObserverEntry], this as never)
  }
  disconnect() {}
  unobserve() {}
}
const view = (o: Partial<teams.TeamView> = {}): teams.TeamView => ({
  Name: NAME,
  LeadSessionID: "s1",
  Live: true,
  Agents: [
    { Name: "Atlas", AgentID: "atlas", Role: "chef de projet", Pane: "%1", State: "idle", Lead: true },
    { Name: "Ada", AgentID: "ada", Role: "backend", Pane: "%2", State: "working", Lead: false },
  ],
  Tasks: [{ ID: "1", Title: "Écrire l'API", Status: "in_progress" }],
  Messages: [{ ID: "m1", From: "Ada", To: "Atlas", Text: "fini", Summary: "", Kind: "", At: 2000, Read: false }],
  Direct: [{ Author: "Max", To: "Ada", Text: "bonjour Ada", At: 1000 }],
  Queued: 0,
  Permission: "",
  Unknown: [],
  ...o,
})
const mockGet = (v = view()) => vi.mocked(teams.teamGet).mockResolvedValue({ ok: true, view: v })

beforeEach(() => {
  rights.exec = true
  width = 1200
  vi.stubGlobal("ResizeObserver", RO)
  vi.mocked(teams.teamSend).mockResolvedValue({ ok: true, queued: false })
  vi.mocked(teams.teamKey).mockResolvedValue({ ok: true })
  vi.mocked(teams.teamClose).mockResolvedValue({ ok: true })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})

describe("TeamRun", () => {
  it("largeur 1200 : trois colonnes", async () => {
    mockGet()
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    expect(screen.getAllByRole("region").map((e) => e.getAttribute("aria-label"))).toEqual(["Agents", "Messages", "Tâches"])
  })
  it("largeur 600 : liste puis détail de l'agent choisi", async () => {
    width = 600
    mockGet()
    render(<TeamRun name={NAME} />)
    fireEvent.click(await screen.findByRole("button", { name: /Ada/ }))
    expect(screen.queryByRole("region", { name: "Messages" })).toBeNull()
    for (const n of ["Écrire à", "Tâches", "Tous les messages", "Écran"]) expect(screen.getByRole("button", { name: n })).toBeTruthy()
    expect(screen.getByText("bonjour Ada")).toBeTruthy()
  })
  it("sans ResizeObserver : disposition B", async () => {
    vi.unstubAllGlobals()
    mockGet()
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    expect(screen.queryByRole("region", { name: "Messages" })).toBeNull()
  })
  it("un message direct montre son auteur", async () => {
    mockGet()
    render(<TeamRun name={NAME} />)
    await screen.findByText("bonjour Ada")
    expect(screen.getByText("Max")).toBeTruthy()
  })
  it("permission : texte brut, Oui/Non vers le chef", async () => {
    const payload = "<img src=x onerror=alert(1)>"
    mockGet(view({ Permission: payload }))
    const { container } = render(<TeamRun name={NAME} />)
    await screen.findByText(payload)
    expect(container.querySelector("img")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Oui" }))
    await waitFor(() => expect(teams.teamKey).toHaveBeenCalledWith(NAME, "Atlas", "1"))
    fireEvent.click(screen.getByRole("button", { name: "Non" }))
    await waitFor(() => expect(teams.teamKey).toHaveBeenCalledWith(NAME, "Atlas", "Escape"))
  })
  it("Unknown : marqueur format inconnu", async () => {
    mockGet(view({ Unknown: ["foo.json"] }))
    render(<TeamRun name={NAME} />)
    expect(await screen.findByText("format inconnu")).toBeTruthy()
    expect(screen.getAllByText("Atlas").length).toBeGreaterThan(0)
  })
  it("Live=false : actions désactivées, historique lisible", async () => {
    mockGet(view({ Live: false, Permission: "x" }))
    render(<TeamRun name={NAME} />)
    await screen.findByText("bonjour Ada")
    for (const b of screen.getAllByRole("button")) expect((b as HTMLButtonElement).disabled).toBe(true)
  })
  it("envoi : destinataire choisi, en file", async () => {
    mockGet()
    vi.mocked(teams.teamSend).mockResolvedValue({ ok: true, queued: true })
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    const send = screen.getByRole("button", { name: "Envoyer" }) as HTMLButtonElement
    expect(send.disabled).toBe(true)
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "Ada" } })
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "salut" } })
    fireEvent.click(send)
    await waitFor(() => expect(teams.teamSend).toHaveBeenCalledWith(NAME, "Ada", "salut"))
    expect(await screen.findByText("en file")).toBeTruthy()
  })
  it("envoi refusé : out en texte", async () => {
    mockGet()
    vi.mocked(teams.teamSend).mockResolvedValue({ ok: false, out: "<b>trop long</b>" })
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "x" } })
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" })
    expect(await screen.findByText("<b>trop long</b>")).toBeTruthy()
    expect(teams.teamSend).toHaveBeenCalledWith(NAME, "Atlas", "x")
  })
  it("lecteur : aucune action", async () => {
    rights.exec = false
    mockGet(view({ Permission: "p" }))
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    expect(screen.queryByRole("button", { name: "Oui" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Envoyer" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Fermer la team" })).toBeNull()
    expect(screen.queryByRole("textbox")).toBeNull()
  })
  it("fermer demande confirmation", async () => {
    mockGet()
    render(<TeamRun name={NAME} />)
    fireEvent.click(await screen.findByRole("button", { name: "Fermer la team" }))
    expect(teams.teamClose).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole("button", { name: "Fermer la team" }))
    await waitFor(() => expect(teams.teamClose).toHaveBeenCalledWith(NAME))
  })
  it("polling sans chevauchement, arrêté au démontage", async () => {
    vi.useFakeTimers()
    let release: (v: never) => void = () => {}
    vi.mocked(teams.teamGet).mockImplementation(() => new Promise((r) => (release = r as never)))
    const { unmount } = render(<TeamRun name={NAME} />)
    await act(() => vi.advanceTimersByTimeAsync(4100))
    expect(teams.teamGet).toHaveBeenCalledTimes(1)
    await act(async () => release({ ok: true, view: view() } as never))
    await act(() => vi.advanceTimersByTimeAsync(2100))
    expect(teams.teamGet).toHaveBeenCalledTimes(2)
    unmount()
    await act(() => vi.advanceTimersByTimeAsync(10000))
    expect(teams.teamGet).toHaveBeenCalledTimes(2)
  })
})
