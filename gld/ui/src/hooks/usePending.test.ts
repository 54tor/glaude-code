// @vitest-environment jsdom
import { renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type QItem } from "@/lib/api"
import { usePending } from "./usePending"

afterEach(() => vi.restoreAllMocks())

describe("usePending", () => {
  it("renvoie les items de la file serveur", async () => {
    const it1: QItem = { id: "a", text: "un", author: "host", state: "queued", at: 1 }
    vi.spyOn(api, "queue").mockResolvedValue({ items: [it1] })
    const { result } = renderHook(() => usePending("s1"))
    await waitFor(() => expect(result.current).toEqual([it1]))
  })
})
