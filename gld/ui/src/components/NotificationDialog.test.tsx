// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { getNotifications, resetNotifications, toast } from "@/lib/notify"
import { publishTasks, resetTasks } from "@/lib/tasksStore"
import { api } from "@/lib/api"
import { NotificationDialog } from "./NotificationDialog"

describe("NotificationDialog", () => {
  beforeEach(() => {
    resetNotifications()
    resetTasks()
  })
  afterEach(cleanup)

  it("reste fermée quand la cible est undefined", () => {
    render(<NotificationDialog target={undefined} onTarget={vi.fn()} />)
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("affiche message et description d'une notification", () => {
    const id = toast.error("échec", { description: <b>le détail</b> })
    render(<NotificationDialog target={id} onTarget={vi.fn()} />)
    expect(screen.getByText("échec")).toBeTruthy()
    expect(screen.getByText("le détail")).toBeTruthy()
  })

  it("le bouton d'action de la notification est appelable", () => {
    const onClick = vi.fn()
    const id = toast.error("échec", { action: { label: "Réessayer", onClick } })
    render(<NotificationDialog target={id} onTarget={vi.fn()} />)
    fireEvent.click(screen.getByText("Réessayer"))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it("l'action ferme la notification et la modale", () => {
    const id = toast.error("échec", {
      action: { label: "Annuler", onClick: vi.fn() },
    })
    const onTarget = vi.fn()
    render(<NotificationDialog target={id} onTarget={onTarget} />)
    fireEvent.click(screen.getByText("Annuler"))
    expect(getNotifications().visible).toHaveLength(0)
    expect(onTarget).toHaveBeenCalledWith(undefined)
  })

  it("l'action qui appelle preventDefault garde notification et modale", () => {
    const id = toast.error("échec", {
      action: {
        label: "Envoyer",
        onClick: (event) => event.preventDefault(),
      },
    })
    const onTarget = vi.fn()
    render(<NotificationDialog target={id} onTarget={onTarget} />)
    fireEvent.click(screen.getByText("Envoyer"))
    expect(getNotifications().visible).toHaveLength(1)
    expect(onTarget).not.toHaveBeenCalled()
  })

  it("liste l'historique, les plus récentes d'abord", () => {
    toast.error("ancienne")
    toast.error("récente")
    render(<NotificationDialog target={null} onTarget={vi.fn()} />)
    const rows = screen.getAllByRole("button", { name: /ancienne|récente/ })
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toContain("récente")
    expect(rows[1].textContent).toContain("ancienne")
  })

  it("cliquer une ligne ouvre son détail, retour revient à la liste", () => {
    const id = toast.error("cible")
    const onTarget = vi.fn()
    const { rerender } = render(
      <NotificationDialog target={null} onTarget={onTarget} />
    )
    fireEvent.click(screen.getByRole("button", { name: /cible/ }))
    expect(onTarget).toHaveBeenCalledWith(id)
    rerender(<NotificationDialog target={id} onTarget={onTarget} />)
    fireEvent.click(screen.getByText("Retour à la liste"))
    expect(onTarget).toHaveBeenLastCalledWith(null)
  })

  it("une notification fermée reste ouvrable depuis l'historique", () => {
    const id = toast.error("passée")
    toast.dismiss(id)
    render(<NotificationDialog target={id} onTarget={vi.fn()} />)
    expect(screen.getByText("passée")).toBeTruthy()
  })

  it("Vider l'historique supprime les notifications fermées", () => {
    const closedId = toast.error("fermée")
    toast.dismiss(closedId)
    toast.error("encore visible")
    render(<NotificationDialog target={null} onTarget={vi.fn()} />)
    fireEvent.click(screen.getByText("Vider l'historique"))
    expect(getNotifications().history.map((n) => n.message)).toEqual([
      "encore visible",
    ])
  })

  it("ouvrir marque tout comme lu", () => {
    toast.error("a")
    render(<NotificationDialog target={null} onTarget={vi.fn()} />)
    act(() => {})
    expect(getNotifications().history.every((n) => n.read)).toBe(true)
  })

  it("affiche l'état vide", () => {
    render(<NotificationDialog target={null} onTarget={vi.fn()} />)
    expect(screen.getByText("Aucune notification")).toBeTruthy()
  })

  it("affiche une tâche en cours et permet de l'arrêter", () => {
    const sessionSend = vi
      .spyOn(api, "sessionSend")
      .mockResolvedValue(undefined as never)
    publishTasks("s1", "/d", [
      { id: "t1", desc: "npm run build", status: "running", hasOutput: true },
    ])
    render(<NotificationDialog target="task:s1:t1" onTarget={vi.fn()} />)
    expect(screen.getByText("npm run build")).toBeTruthy()
    fireEvent.click(screen.getByText("Arrêter la tâche"))
    expect(sessionSend).toHaveBeenCalledWith(
      "s1",
      expect.stringContaining("t1")
    )
  })

  it("une tâche terminée retombe sur la liste", () => {
    render(<NotificationDialog target="task:s1:absente" onTarget={vi.fn()} />)
    expect(screen.getByText("Aucune notification")).toBeTruthy()
  })

  it("cliquer un bouton de la description ferme la modale", () => {
    const onOpenSession = vi.fn()
    const id = toast.warning("alerte", {
      description: <button onClick={onOpenSession}>ouvrir</button>,
    })
    const onTarget = vi.fn()
    render(<NotificationDialog target={id} onTarget={onTarget} />)
    fireEvent.click(screen.getByText("ouvrir"))
    expect(onOpenSession).toHaveBeenCalled()
    expect(onTarget).toHaveBeenCalledWith(undefined)
  })
})
