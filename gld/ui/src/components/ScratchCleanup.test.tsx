// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { ScratchCleanup } from "./ScratchCleanup"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it("n'efface qu'après la confirmation", async () => {
  vi.spyOn(api, "scratchStats").mockResolvedValue({ count: 3, kb: 2048 })
  const purge = vi
    .spyOn(api, "scratchPurge")
    .mockResolvedValue({ count: 3, kb: 2048 })
  render(<ScratchCleanup />)
  fireEvent.click(await screen.findByRole("button", { name: /Nettoyer/ }))
  expect(purge).not.toHaveBeenCalled()
  fireEvent.click(
    screen.getByRole("button", { name: /Effacer définitivement/ })
  )
  await waitFor(() => expect(purge).toHaveBeenCalledTimes(1))
})
