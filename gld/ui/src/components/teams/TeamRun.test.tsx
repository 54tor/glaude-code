// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
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
    { Name: "team-lead", Display: "Chef de projet", AgentID: "chef-de-projet", Role: "", Pane: "%1", State: "idle", Lead: true },
    { Name: "ada", Display: "Ada", AgentID: "backend", Role: "Backend", Pane: "%2", State: "working", Lead: false },
  ],
  Tasks: [{ ID: "1", Title: "Écrire l'API", Status: "in_progress" }],
  Messages: [{ ID: "m1", From: "ada", To: "team-lead", Text: "fini", Summary: "", Kind: "", At: 2000, Read: false }],
  Direct: [{ Author: "Max", To: "ada", Text: "bonjour Ada", At: 1000 }],
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
  const marcus = { Name: "marcus", Display: "Marcus", AgentID: "testeur", Role: "Testeur", Pane: "%3", State: "idle", Lead: false }
  it("agent : « Prénom · Rôle », prénom seul sans rôle, chef sans rôle", async () => {
    mockGet(view({ Agents: [...view().Agents, marcus, { ...marcus, Name: "zoe", Display: "Zoe", Role: "" }] }))
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    const list = screen.getByRole("region", { name: "Agents" })
    expect(list.textContent).toContain("Chef de projet")
    expect(list.textContent).not.toContain("team-lead")
    const li = (n: string) => within(list).getByText(n).closest("li")!.textContent
    expect(li("Marcus")).toContain("· Testeur")
    expect(li("Zoe")).not.toMatch(/·|Testeur/)
    expect(screen.getByRole("option", { name: "Marcus · Testeur" })).toBeTruthy()
    expect(screen.getByRole("option", { name: "Zoe" })).toBeTruthy()
  })
  it("liste étroite : en-tête du détail « Prénom · Rôle »", async () => {
    width = 600
    mockGet(view({ Agents: [...view().Agents, marcus] }))
    render(<TeamRun name={NAME} />)
    fireEvent.click(await screen.findByRole("button", { name: /Marcus/ }))
    expect(screen.getAllByText("Marcus · Testeur").some((e) => e.tagName === "SPAN")).toBe(true)
  })
  it("envoi à Marcus : nom brut en argument, option valeur brute", async () => {
    mockGet(view({ Agents: [...view().Agents, marcus] }))
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    expect((screen.getByRole("option", { name: "Marcus · Testeur" }) as HTMLOptionElement).value).toBe("marcus")
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "marcus" } })
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "salut" } })
    fireEvent.click(screen.getByRole("button", { name: "Envoyer" }))
    await waitFor(() => expect(teams.teamSend).toHaveBeenCalledWith(NAME, "marcus", "salut"))
  })
  it("messages : noms bruts remplacés par Display, auteur humain inchangé", async () => {
    mockGet(view({ Agents: [...view().Agents, marcus], Messages: [{ ID: "m2", From: "marcus", To: "team-lead", Text: "ok", Summary: "", Kind: "", At: 3000, Read: false }], Direct: [{ Author: "Max", To: "marcus", Text: "hello", At: 1000 }] }))
    const { container } = render(<TeamRun name={NAME} />)
    await screen.findByText("hello")
    const heads = [...container.querySelectorAll("li .text-xs")].map((e) => e.textContent)
    expect(heads).toContain("Max → Marcus")
    expect(heads).toContain("Marcus → Chef de projet")
    expect(container.textContent).not.toMatch(/marcus →|→ marcus|team-lead →|→ team-lead/)
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
    await waitFor(() => expect(teams.teamKey).toHaveBeenCalledWith(NAME, "team-lead", "1", expect.any(String)))
    fireEvent.click(screen.getByRole("button", { name: "Non" }))
    await waitFor(() => expect(teams.teamKey).toHaveBeenCalledWith(NAME, "team-lead", "Escape", expect.any(String)))
  })
  it("Unknown : marqueur format inconnu", async () => {
    mockGet(view({ Unknown: ["foo.json"] }))
    render(<TeamRun name={NAME} />)
    expect(await screen.findByText("format inconnu")).toBeTruthy()
    expect(screen.getAllByText("Chef de projet").length).toBeGreaterThan(0)
  })
  it("Live=false : actions désactivées, historique lisible", async () => {
    mockGet(view({ Live: false, Permission: "x" }))
    render(<TeamRun name={NAME} />)
    await screen.findByText("bonjour Ada")
    for (const b of screen.getAllByRole("button", { name: (n) => n !== "Fermer la team" })) expect((b as HTMLButtonElement).disabled).toBe(true)
  })
  it("envoi : destinataire choisi, en file", async () => {
    mockGet()
    vi.mocked(teams.teamSend).mockResolvedValue({ ok: true, queued: true })
    render(<TeamRun name={NAME} />)
    await screen.findByRole("heading", { name: NAME })
    const send = screen.getByRole("button", { name: "Envoyer" }) as HTMLButtonElement
    expect(send.disabled).toBe(true)
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "ada" } })
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "salut" } })
    fireEvent.click(send)
    await waitFor(() => expect(teams.teamSend).toHaveBeenCalledWith(NAME, "ada", "salut"))
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
    expect(teams.teamSend).toHaveBeenCalledWith(NAME, "team-lead", "x")
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
  it("team terminée : Fermer reste actif et supprime après confirmation", async () => {
    mockGet(view({ Live: false }))
    render(<TeamRun name={NAME} />)
    fireEvent.click(await screen.findByRole("button", { name: "Fermer la team" }))
    fireEvent.click(await screen.findByRole("button", { name: "Fermer la team" }))
    await waitFor(() => expect(teams.teamClose).toHaveBeenCalledWith(NAME))
  })
  it("demande illisible : renvoie au terminal", async () => {
    const a = view().Agents.map((x) => (x.Lead ? { ...x, State: "waiting" } : x))
    mockGet(view({ Agents: a, Permission: "" }))
    render(<TeamRun name={NAME} />)
    expect(await screen.findByText("Répondre dans le terminal")).toBeTruthy()
  })
  it("compte rendu d'un coéquipier (idle avec texte) affiché comme un message", async () => {
    mockGet(view({ Messages: [{ ID: "i1", From: "ada", To: "team-lead", Text: "rapport final", Summary: "", Kind: "idle", At: 3000, Read: false }] }))
    render(<TeamRun name={NAME} />)
    expect(await screen.findByText("rapport final")).toBeTruthy()
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
  it("touche : expect = texte affiché, un seul envoi si double appel", async () => {
    mockGet(view({ Permission: "dlg" }))
    let done: (v: never) => void = () => {}
    vi.mocked(teams.teamKey).mockImplementation(() => new Promise((r) => (done = r as never)))
    render(<TeamRun name={NAME} />)
    const oui = await screen.findByRole("button", { name: "Oui" })
    fireEvent.click(oui)
    fireEvent.click(oui)
    expect(teams.teamKey).toHaveBeenCalledTimes(1)
    expect(teams.teamKey).toHaveBeenCalledWith(NAME, "team-lead", "1", "dlg")
    await act(async () => done({ ok: true } as never))
  })
  it("relève partie avant la touche : Oui/Non restent désactivés", async () => {
    vi.useFakeTimers()
    let slow: (v: never) => void = () => {}
    let n = 0
    vi.mocked(teams.teamGet).mockImplementation(() => {
      n++
      return n === 2 ? new Promise((r) => (slow = r as never)) : Promise.resolve({ ok: true, view: view({ Permission: "dlg" }) } as never)
    })
    render(<TeamRun name={NAME} />)
    await act(() => vi.advanceTimersByTimeAsync(0))
    await act(() => vi.advanceTimersByTimeAsync(2100)) // relève 2, lente, partie avant la touche
    fireEvent.click(screen.getByRole("button", { name: "Oui" }))
    await act(() => vi.advanceTimersByTimeAsync(0))
    await act(async () => slow({ ok: true, view: view({ Permission: "dlg" }) } as never)) // réponse d'avant la touche
    expect((screen.getByRole("button", { name: "Oui" }) as HTMLButtonElement).disabled).toBe(true)
    await act(() => vi.advanceTimersByTimeAsync(2100)) // relève partie après
    expect((screen.getByRole("button", { name: "Oui" }) as HTMLButtonElement).disabled).toBe(false)
  })
  it("touche refusée : erreur serveur en texte", async () => {
    mockGet(view({ Permission: "dlg" }))
    vi.mocked(teams.teamKey).mockResolvedValue({ ok: false, out: "<i>périmé</i>" })
    render(<TeamRun name={NAME} />)
    fireEvent.click(await screen.findByRole("button", { name: "Oui" }))
    expect(await screen.findByText("<i>périmé</i>")).toBeTruthy()
  })
  it("changement de nom : réponse de l'ancienne team jetée, pas d'action avec l'ancien chef", async () => {
    let relA: (v: never) => void = () => {}
    const vB = view({ Name: "session-bbbbbbbb", Permission: "dlgB", Agents: [{ Name: "zed", Display: "Zed", AgentID: "z", Role: "R", Pane: "%9", State: "idle", Lead: true }] })
    vi.mocked(teams.teamGet).mockImplementation((n) => (n === NAME ? new Promise((r) => (relA = r as never)) : Promise.resolve({ ok: true, view: vB } as never)))
    const { rerender } = render(<TeamRun name={NAME} />)
    rerender(<TeamRun name="session-bbbbbbbb" />)
    await screen.findByRole("heading", { name: "session-bbbbbbbb" })
    await act(async () => relA({ ok: true, view: view({ Permission: "dlgA" }) } as never))
    expect(screen.queryByText("dlgA")).toBeNull()
    expect(screen.getByRole("heading", { level: 2 }).textContent).toBe("session-bbbbbbbb")
    fireEvent.click(screen.getByRole("button", { name: "Oui" }))
    await waitFor(() => expect(teams.teamKey).toHaveBeenCalledWith("session-bbbbbbbb", "zed", "1", "dlgB"))
  })
})
