// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type DocEntry } from "@/lib/api"
import { DocumentsDialog } from "./DocumentsDialog"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver

const openDialogWith = async (documents: DocEntry[]) => {
  vi.spyOn(api, "sessionDocuments").mockResolvedValue({ documents })
  render(<DocumentsDialog id="s1" dir="d1" open onClose={() => {}} />)
  await act(async () => {})
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("DocumentsDialog", () => {
  it("affiche un message quand la session n'a aucun document", async () => {
    await openDialogWith([])

    expect(screen.getByText(/Aucun document|No documents/)).toBeTruthy()
  })

  it("liste chaque document avec un lien de téléchargement", async () => {
    await openDialogWith([{ path: "/home/bob/rapport.md", name: "rapport.md", size: 2048 }])

    expect(screen.getByText("rapport.md")).toBeTruthy()
    const downloadLink = document.querySelector("a[download='rapport.md']") as HTMLAnchorElement
    expect(downloadLink.getAttribute("href")).toContain("download=1")
  })

  it("signale un fichier introuvable sans proposer de téléchargement", async () => {
    await openDialogWith([{ path: "/home/bob/disparu.md", name: "disparu.md", size: 0, missing: true }])

    expect(screen.getByText(/disparu\.md · (Fichier introuvable|File not found)/)).toBeTruthy()
    expect(document.querySelector("a[download]")).toBeNull()
  })
})
