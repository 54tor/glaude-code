// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, test, vi } from "vitest"
import { cleanup } from "@testing-library/react"
import { GuestFiles } from "./GuestFiles"
import { api } from "@/lib/api"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

test("liste le dossier, déplie un sous-dossier et lit un fichier", async () => {
  vi.spyOn(api, "fsTree").mockImplementation(async (_d, path) => ({
    entries: path === "" ? [{ name: "src", type: "dir", size: 0, mtime: 0 }, { name: "a.txt", type: "file", size: 1, mtime: 0 }] : [{ name: "b.go", type: "file", size: 1, mtime: 0 }],
    truncated: false,
  }))
  vi.spyOn(api, "fsFile").mockResolvedValue({ content: "bonjour", etag: "e", size: 7, mtime: 0 })
  render(<GuestFiles dir="/p" />)
  fireEvent.click(await screen.findByText("src"))
  expect(await screen.findByText("b.go")).toBeTruthy()
  fireEvent.click(screen.getByText("a.txt"))
  expect(await screen.findByText("bonjour")).toBeTruthy()
  expect(api.fsFile).toHaveBeenCalledWith("/p", "a.txt")
})
