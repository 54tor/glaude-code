// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { PermImportDialog, PermIOButtons } from "./ConfigPermIO"

afterEach(cleanup)

const documentWithRules = {
  permissions: { allow: ["Bash(ls)", "Read"], deny: ["Bash(rm:*)"] },
}

function renderButtons(doc: Record<string, unknown> | undefined) {
  const callbacks = { onImport: vi.fn(), onRaw: vi.fn(), onReset: vi.fn() }
  render(<PermIOButtons file="u" label="user" doc={doc} {...callbacks} />)
  return callbacks
}

describe("PermIOButtons", () => {
  it("affiche le nombre de règles sur le bouton d'export", () => {
    renderButtons(documentWithRules)
    expect(
      screen.getByRole("button", { name: /Exporter/ }).textContent
    ).toContain("(3)")
  })

  it("désactive export, copie et réinitialisation sans règle", () => {
    renderButtons(undefined)
    expect(screen.getByRole("button", { name: /Exporter/ })).toHaveProperty(
      "disabled",
      true
    )
    expect(
      screen.getByRole("button", { name: /Réinitialiser/ })
    ).toHaveProperty("disabled", true)
  })

  it("garde la réinitialisation active quand il reste des dossiers supplémentaires", () => {
    renderButtons({ permissions: { additionalDirectories: ["/x"] } })
    expect(
      screen.getByRole("button", { name: /Réinitialiser/ })
    ).toHaveProperty("disabled", false)
  })

  it("relaie les clics Importer, Éditer et Réinitialiser", () => {
    const callbacks = renderButtons(documentWithRules)
    fireEvent.click(screen.getByRole("button", { name: /Importer/ }))
    fireEvent.click(screen.getByRole("button", { name: /Éditer en texte/ }))
    fireEvent.click(screen.getByRole("button", { name: /Réinitialiser/ }))
    expect([
      callbacks.onImport.mock.calls.length,
      callbacks.onRaw.mock.calls.length,
      callbacks.onReset.mock.calls.length,
    ]).toEqual([1, 1, 1])
  })

  it("copie le fragment des règles, defaultMode exclu", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    })
    renderButtons({
      permissions: { ...documentWithRules.permissions, defaultMode: "plan" },
    })
    fireEvent.click(screen.getByRole("button", { name: /Copier/ }))
    await waitFor(() => expect(writeText).toHaveBeenCalled())
    const valeurObtenue = JSON.parse(writeText.mock.calls[0][0])
    expect(valeurObtenue).toEqual({
      permissions: { allow: ["Bash(ls)", "Read"], deny: ["Bash(rm:*)"] },
    })
  })
})

describe("PermImportDialog", () => {
  function renderDialog(propose: () => Promise<string | null>) {
    const onClose = vi.fn()
    render(
      <PermImportDialog
        open
        file="u"
        label="user"
        propose={propose}
        onClose={onClose}
      />
    )
    return onClose
  }

  it("désactive l'aperçu tant que le texte est vide", () => {
    renderDialog(vi.fn().mockResolvedValue(null))
    expect(screen.getByRole("button", { name: "Aperçu" })).toHaveProperty(
      "disabled",
      true
    )
  })

  it("propose le texte collé dans le mode choisi puis ferme", async () => {
    const propose = vi.fn().mockResolvedValue(null)
    const onClose = renderDialog(propose)
    fireEvent.change(screen.getByLabelText("Règles à importer"), {
      target: { value: '{"permissions":{}}' },
    })
    fireEvent.click(screen.getByRole("radio", { name: /Remplacer/ }))
    fireEvent.click(screen.getByRole("button", { name: "Aperçu" }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(propose).toHaveBeenCalledWith("u", '{"permissions":{}}', "replace")
  })

  it("affiche l'erreur de proposition sans fermer", async () => {
    const onClose = renderDialog(vi.fn().mockResolvedValue("JSON invalide"))
    fireEvent.change(screen.getByLabelText("Règles à importer"), {
      target: { value: "x" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Aperçu" }))
    await screen.findByText("JSON invalide")
    expect(onClose).not.toHaveBeenCalled()
  })

  it("refuse un fichier de plus de 1 Mio", async () => {
    renderDialog(vi.fn().mockResolvedValue(null))
    const bigFile = new File(["x"], "gros.json")
    Object.defineProperty(bigFile, "size", { value: (1 << 20) + 1 })
    fireEvent.change(screen.getByLabelText(/Fichier/i), {
      target: { files: [bigFile] },
    })
    expect(await screen.findByText("Fichier trop gros")).toBeTruthy()
  })

  it("charge le contenu d'un fichier dans la zone de texte", async () => {
    renderDialog(vi.fn().mockResolvedValue(null))
    const smallFile = new File(['{"a":1}'], "p.json", {
      type: "application/json",
    })
    fireEvent.change(screen.getByLabelText(/Fichier/i), {
      target: { files: [smallFile] },
    })
    await waitFor(() =>
      expect(
        (screen.getByLabelText("Règles à importer") as HTMLTextAreaElement)
          .value
      ).toBe('{"a":1}')
    )
  })
})
