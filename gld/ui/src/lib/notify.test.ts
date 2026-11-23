import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  AUTO_CLOSE_MS,
  HISTORY_LIMIT,
  getNotifications,
  resetNotifications,
  toast,
} from "./notify"

describe("notify", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    resetNotifications()
  })
  afterEach(() => vi.useRealTimers())

  it("ajoute une pastille visible par appel", () => {
    toast.success("a")
    toast.error("b")
    expect(getNotifications().visible.map((n) => n.message)).toEqual(["a", "b"])
  })

  it("remplace la pastille quand l'id est réutilisé", () => {
    const id = toast.loading("a")
    toast.success("b", { id })
    const { visible, history } = getNotifications()
    expect(visible).toHaveLength(1)
    expect(visible[0].kind).toBe("success")
    expect(visible[0].message).toBe("b")
    expect(history).toHaveLength(1)
  })

  it("un succès s'éteint après AUTO_CLOSE_MS mais reste dans l'historique", () => {
    const onAutoClose = vi.fn()
    toast.success("ok", { onAutoClose })
    vi.advanceTimersByTime(AUTO_CLOSE_MS + 1)
    expect(getNotifications().visible).toHaveLength(0)
    expect(getNotifications().history).toHaveLength(1)
    expect(onAutoClose).toHaveBeenCalledTimes(1)
  })

  it("une erreur reste visible tant qu'elle n'est pas fermée", () => {
    const onDismiss = vi.fn()
    const id = toast.error("ko", { onDismiss })
    vi.advanceTimersByTime(AUTO_CLOSE_MS * 10)
    expect(getNotifications().visible).toHaveLength(1)
    toast.dismiss(id)
    expect(getNotifications().visible).toHaveLength(0)
    expect(getNotifications().history).toHaveLength(1)
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it("duration Infinity empêche l'extinction, loading ne s'éteint jamais", () => {
    toast.success("long", { duration: Infinity })
    toast.loading("en cours")
    vi.advanceTimersByTime(AUTO_CLOSE_MS * 10)
    expect(getNotifications().visible).toHaveLength(2)
  })

  it("une durée explicite prime sur le défaut", () => {
    toast.error("court", { duration: 100 })
    vi.advanceTimersByTime(101)
    expect(getNotifications().visible).toHaveLength(0)
  })

  it("dismiss d'un id inconnu ne plante pas", () => {
    expect(() => toast.dismiss("absent")).not.toThrow()
  })

  it("dismiss sans id ferme toutes les pastilles", () => {
    toast.error("a")
    toast.error("b")
    toast.dismiss()
    expect(getNotifications().visible).toHaveLength(0)
    expect(getNotifications().history).toHaveLength(2)
  })

  it("l'historique est borné à HISTORY_LIMIT, les plus récentes gardées", () => {
    for (let index = 0; index < HISTORY_LIMIT + 50; index++)
      toast.error(`m${index}`)
    const { history } = getNotifications()
    expect(history).toHaveLength(HISTORY_LIMIT)
    expect(history[history.length - 1].message).toBe(`m${HISTORY_LIMIT + 49}`)
  })

  it("garde description et action telles quelles", () => {
    const description = { type: "div" }
    const onClick = vi.fn()
    toast.error("x", {
      description: description as never,
      action: { label: "Go", onClick },
    })
    const [notification] = getNotifications().visible
    expect(notification.description).toBe(description)
    expect(notification.action?.label).toBe("Go")
  })
})
