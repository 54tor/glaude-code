// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { resetNotifications, toast } from "@/lib/notify"
import { publishTasks, resetTasks } from "@/lib/tasksStore"
import { api } from "@/lib/api"
import { MAX_VISIBLE_CHIPS, StatusBar } from "./StatusBar"

describe("StatusBar", () => {
  beforeEach(() => {
    resetNotifications()
    resetTasks()
    vi.spyOn(api, "updateInfo").mockRejectedValue(new Error("hors ligne"))
  })
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("est une région nommée", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    expect(screen.getByRole("region", { name: "Barre d'état" })).toBeTruthy()
  })

  it("annonce les nouvelles notifications aux lecteurs d'écran", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => void toast.error("alerte"))
    expect(screen.getByText("alerte").closest("[aria-live]")).not.toBeNull()
  })

  it("rend une pastille par notification visible", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => {
      toast.error("première")
      toast.error("seconde")
    })
    expect(screen.getByText("première")).toBeTruthy()
    expect(screen.getByText("seconde")).toBeTruthy()
  })

  it("plafonne les pastilles et affiche le reste en +N", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => {
      for (let index = 0; index < MAX_VISIBLE_CHIPS + 3; index++)
        toast.error(`m${index}`)
    })
    expect(screen.getByText("+3")).toBeTruthy()
    expect(screen.queryByText("m0")).toBeNull()
  })

  it("un clic sur une pastille ouvre son détail", () => {
    const onOpen = vi.fn()
    render(<StatusBar onOpen={onOpen} />)
    let id = ""
    act(() => {
      id = toast.error("détail")
    })
    fireEvent.click(screen.getByText("détail"))
    expect(onOpen).toHaveBeenCalledWith(id)
  })

  it("le bouton fermer retire la pastille sans ouvrir le détail", () => {
    const onOpen = vi.fn()
    render(<StatusBar onOpen={onOpen} />)
    act(() => void toast.error("à fermer"))
    fireEvent.click(screen.getByLabelText("Fermer la notification"))
    expect(screen.queryByText("à fermer")).toBeNull()
    expect(onOpen).not.toHaveBeenCalled()
  })

  it("le bouton historique ouvre la liste", () => {
    const onOpen = vi.fn()
    render(<StatusBar onOpen={onOpen} />)
    fireEvent.click(screen.getByLabelText("Historique des notifications"))
    expect(onOpen).toHaveBeenCalledWith(null)
  })

  it("le bouton historique compte les notifications non lues", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => {
      toast.error("a")
      toast.error("b")
    })
    expect(screen.getByText("2")).toBeTruthy()
  })

  it("garde pointerdown et focus hors des écouteurs du document (dialogues Radix)", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => void toast.error("dans la barre"))
    const seen = vi.fn()
    document.addEventListener("pointerdown", seen)
    document.addEventListener("focusin", seen)
    const chip = screen.getByText("dans la barre")
    fireEvent.pointerDown(chip)
    fireEvent.focusIn(chip)
    expect(seen).not.toHaveBeenCalled()
    fireEvent.pointerDown(document.body)
    expect(seen).toHaveBeenCalledTimes(1)
    document.removeEventListener("pointerdown", seen)
    document.removeEventListener("focusin", seen)
  })

  it("affiche les tâches en cours avant les notifications, clic = task:clé", () => {
    const onOpen = vi.fn()
    render(<StatusBar onOpen={onOpen} />)
    act(() => {
      toast.error("une erreur")
      publishTasks("s1", "/d", [
        { id: "t1", desc: "npm run build", status: "running", hasOutput: true },
      ])
    })
    const labels = screen
      .getAllByRole("button")
      .map((button) => button.textContent)
    expect(labels.indexOf("npm run build")).toBeLessThan(
      labels.indexOf("une erreur")
    )
    fireEvent.click(screen.getByText("npm run build"))
    expect(onOpen).toHaveBeenCalledWith("task:s1:t1")
  })

  it("le plafond compte aussi les tâches", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => {
      publishTasks(
        "s1",
        "/d",
        Array.from({ length: MAX_VISIBLE_CHIPS + 2 }, (_, index) => ({
          id: `t${index}`,
          desc: `tâche ${index}`,
          status: "running",
          hasOutput: false,
        }))
      )
      toast.error("masquée")
    })
    expect(screen.getByText("+3")).toBeTruthy()
    expect(screen.queryByText("masquée")).toBeNull()
  })

  it("le bouton historique annonce le nombre de non lues", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() => void toast.error("a"))
    expect(
      screen.getByLabelText(
        "Historique des notifications, 1 notification non lue"
      )
    ).toBeTruthy()
  })

  it("une tâche sans description affiche son identifiant", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    act(() =>
      publishTasks("s1", "/d", [
        { id: "t9", desc: "", status: "running", hasOutput: false },
      ])
    )
    expect(screen.getByText("t9")).toBeTruthy()
  })

  it("version : affichée avec un lien de mise à jour seulement si une release plus récente existe", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue({
      current: "v0.9.0",
      latest: "v0.9.1",
      url: "https://github.com/example/glaude-code/releases/tag/v0.9.1",
      newer: true,
    })
    render(<StatusBar onOpen={vi.fn()} />)
    expect(await screen.findByText("v0.9.0")).toBeTruthy()
    const updateLink = screen.getByRole("link", {
      name: "Mettre à jour : v0.9.1",
    })
    expect(updateLink.getAttribute("rel")).toContain("noopener")
  })

  it("version à jour : pas de lien", async () => {
    vi.mocked(api.updateInfo).mockResolvedValue({
      current: "v0.9.1",
      latest: "v0.9.1",
      url: "x",
      newer: false,
    })
    render(<StatusBar onOpen={vi.fn()} />)
    expect(await screen.findByText("v0.9.1")).toBeTruthy()
    expect(screen.queryByRole("link", { name: /Mettre à jour/ })).toBeNull()
  })

  it("version indisponible : rien n'est affiché", async () => {
    render(<StatusBar onOpen={vi.fn()} />)
    await act(async () => {})
    expect(screen.getByRole("region").textContent).not.toMatch(/v\d/)
  })
})
