// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { resetNotifications, toast } from "@/lib/notify"
import { MAX_VISIBLE_CHIPS, StatusBar } from "./StatusBar"

describe("StatusBar", () => {
  beforeEach(() => resetNotifications())
  afterEach(cleanup)

  it("est une région nommée", () => {
    render(<StatusBar onOpen={vi.fn()} />)
    expect(screen.getByRole("region", { name: "Barre d'état" })).toBeTruthy()
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
})
