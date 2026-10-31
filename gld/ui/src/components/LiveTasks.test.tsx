// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type BgTask, type Workflow } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import type { Live } from "./LiveStatus"
import { LiveTasks, stopTask, WorkflowProgress } from "./LiveTasks"

const task = (overrides: Partial<BgTask> = {}): BgTask => ({ id: "t1", desc: "npm run build", status: "running", hasOutput: true, ...overrides })
const liveWith = (tasks?: BgTask[]): Live => ({ st: { tasks } as never, open: true })
const workflow = (overrides: Partial<Workflow> = {}): Workflow => ({
  runId: "r1",
  taskId: "t1",
  name: "audit",
  summary: "",
  status: "running",
  startTime: 0,
  durationMs: 0,
  totalTokens: 1500,
  totalToolCalls: 0,
  phases: [{ title: "Review", detail: "" }],
  agents: [],
  logs: [],
  ...overrides,
})

const flush = () => act(async () => {})
const mount = async (tasks?: BgTask[]) => {
  render(<LiveTasks live={liveWith(tasks)} dir="/home/bob/p" id="s1" />)
  await flush()
}
const openList = async () => {
  fireEvent.click(screen.getByRole("button", { name: /tâche\(s\)/ }))
  await flush()
}

beforeEach(() => {
  vi.spyOn(api, "taskOutput").mockResolvedValue({ text: "sortie" })
  vi.spyOn(api, "workflows").mockResolvedValue({ workflows: [] })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe("LiveTasks", () => {
  it("rien sans tâche", async () => {
    await mount([])
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("le bouton compte les tâches et celles en cours", async () => {
    await mount([task(), task({ id: "t2", status: "completed" }), task({ id: "t3" })])
    const button = screen.getByRole("button", { name: /tâche\(s\)/ })
    expect(button.textContent).toContain("3 tâche(s)")
    expect(button.textContent).toMatch(/2/)
  })

  it("la liste est la plus récente d'abord, avec l'état de chaque tâche", async () => {
    await mount([task({ id: "a", desc: "première" }), task({ id: "b", desc: "seconde", status: "failed" })])
    await openList()
    const labels = screen.getAllByTitle("Voir la sortie").map((row) => row.textContent)
    expect(labels[0]).toContain("seconde")
    expect(labels[0]).toContain("échec")
    expect(labels[1]).toContain("première")
    expect(labels[1]).toContain("en cours")
  })

  it("seule une tâche en cours propose l'arrêt", async () => {
    await mount([task({ id: "a" }), task({ id: "b", status: "completed" })])
    await openList()
    expect(screen.getAllByRole("button", { name: "Arrêter la tâche" })).toHaveLength(1)
  })

  it("l'arrêt demande à la session d'exécuter TaskStop sur la tâche", async () => {
    const sendSpy = vi.spyOn(api, "sessionSend").mockResolvedValue({} as never)
    const toastSuccess = vi.spyOn(toast, "success").mockReturnValue("")
    await mount([task({ id: "bg-7", desc: "tail -f log" })])
    await openList()
    fireEvent.click(screen.getByRole("button", { name: "Arrêter la tâche" }))
    await flush()
    expect(sendSpy).toHaveBeenCalledWith("s1", "Arrête avec l'outil TaskStop la tâche bg-7 (tail -f log), sans autre commentaire.")
    expect(toastSuccess).toHaveBeenCalledWith("Arrêt demandé à la session")
  })

  it("un clic sur une tâche affiche sa sortie", async () => {
    await mount([task({ status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(api.taskOutput).toHaveBeenCalledWith("/home/bob/p", "s1", "t1")
    expect(screen.getByText("sortie")).toBeTruthy()
  })

  it("sortie vide : le dit", async () => {
    vi.mocked(api.taskOutput).mockResolvedValue({ text: "" })
    await mount([task({ status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.getByText("(sortie vide)")).toBeTruthy()
  })

  it("fichier de sortie disparu (404) : message dédié", async () => {
    vi.mocked(api.taskOutput).mockRejectedValue(new ApiError(404, "not_found", "x"))
    await mount([task({ status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.getByText(/Sortie introuvable/)).toBeTruthy()
  })

  it("une erreur autre que 404 ne remplace pas la sortie par un faux « introuvable »", async () => {
    vi.mocked(api.taskOutput).mockRejectedValue(new ApiError(500, "boom", "x"))
    await mount([task({ status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.queryByText(/Sortie introuvable/)).toBeNull()
    expect(screen.getByText("…")).toBeTruthy()
  })

  it("tâche en cours : sortie relue toutes les 3 s tant que l'onglet est visible", async () => {
    vi.useFakeTimers()
    await mount([task()])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(api.taskOutput).toHaveBeenCalledTimes(1)
    await act(async () => void vi.advanceTimersByTime(3000))
    expect(api.taskOutput).toHaveBeenCalledTimes(2)
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
    await act(async () => void vi.advanceTimersByTime(9000))
    expect(api.taskOutput).toHaveBeenCalledTimes(2)
  })

  it("tâche terminée : lue une seule fois", async () => {
    vi.useFakeTimers()
    await mount([task({ status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    await act(async () => void vi.advanceTimersByTime(12000))
    expect(api.taskOutput).toHaveBeenCalledTimes(1)
  })

  it("tâche Workflow : affiche la progression du workflow rattaché", async () => {
    vi.mocked(api.workflows).mockResolvedValue({
      workflows: [workflow({ taskId: "autre", name: "étranger" }), workflow({ name: "audit-ui", agents: [{ label: "relecteur", phase: "Review", state: "running", tokens: 0, durationMs: 0 } as never] })],
    })
    await mount([task({ tool: "Workflow", desc: "audit" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.getByText("audit-ui")).toBeTruthy()
    expect(screen.getByText("relecteur")).toBeTruthy()
    expect(screen.queryByText("étranger")).toBeNull()
  })

  it("tâche Workflow en cours sans taskId connu : retombe sur le workflow en cours non rattaché", async () => {
    vi.mocked(api.workflows).mockResolvedValue({ workflows: [workflow({ taskId: "", name: "orphelin" })] })
    await mount([task({ tool: "Workflow" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.getByText("orphelin")).toBeTruthy()
  })

  it("tâche Workflow terminée : pas de repli sur un workflow non rattaché", async () => {
    vi.mocked(api.workflows).mockResolvedValue({ workflows: [workflow({ taskId: "", name: "orphelin" })] })
    await mount([task({ tool: "Workflow", status: "completed" })])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(screen.queryByText("orphelin")).toBeNull()
  })

  it("tâche hors Workflow : aucune lecture de workflows", async () => {
    await mount([task()])
    await openList()
    fireEvent.click(screen.getByTitle("Voir la sortie"))
    await flush()
    expect(api.workflows).not.toHaveBeenCalled()
  })
})

describe("stopTask", () => {
  it("l'échec d'envoi s'affiche en toast d'erreur", async () => {
    vi.spyOn(api, "sessionSend").mockRejectedValue(new ApiError(500, "boom", "hors ligne"))
    const toastError = vi.spyOn(toast, "error").mockReturnValue("")
    const toastSuccess = vi.spyOn(toast, "success").mockReturnValue("")
    await stopTask("s1", "t1", "d", "ok")
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("hors ligne"))
    expect(toastSuccess).not.toHaveBeenCalled()
  })
})

describe("WorkflowProgress", () => {
  it("regroupe les agents par phase, y compris une phase absente de la liste déclarée", () => {
    const agents = [
      { label: "a1", phase: "Review", state: "done", tokens: 2000, durationMs: 65000, lastTool: "Read" },
      { label: "a2", phase: "Extra", state: "failed", tokens: 0, durationMs: 0 },
    ] as never
    render(<WorkflowProgress w={workflow({ agents, durationMs: 3_700_000 })} />)
    const review = screen.getByText("Review").parentElement as HTMLElement
    expect(within(review).getByText("a1")).toBeTruthy()
    expect(within(review).getByText("Read")).toBeTruthy()
    expect(within(review).getByText("1 min 5 s")).toBeTruthy()
    expect(within(screen.getByText("Extra").parentElement as HTMLElement).getByText("a2")).toBeTruthy()
    expect(screen.getByText("1 h 1 min")).toBeTruthy()
    expect(screen.getByText("2 agent(s)")).toBeTruthy()
  })

  it("agent sans phase : libellé « sans phase »", () => {
    render(<WorkflowProgress w={workflow({ phases: [], agents: [{ label: "x", phase: "", state: "queued", tokens: 0, durationMs: 0 }] as never })} />)
    expect(screen.getByText("(sans phase)")).toBeTruthy()
  })
})
