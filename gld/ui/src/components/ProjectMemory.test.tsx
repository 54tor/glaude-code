// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type MemFile } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { href } from "@/lib/route"
import { ProjectMemory } from "./ProjectMemory"

class ResizeObserverStub { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

const DIR = "-home-bob-p"
const flush = () => act(async () => {})
const file = (key: string, name: string, overrides: Partial<MemFile> = {}): MemFile => ({ key, name, kb: 1, mtime: 1, path: `/home/bob/.claude/${name}`, tokens: 100, ...overrides })
const memoryReply = (files: MemFile[], dir = DIR) => ({ global: [], projects: [{ dir, project: "/home/bob/p", files }], targets: [] }) as never

const mount = async (files: MemFile[]) => {
  vi.spyOn(api, "memory").mockResolvedValue(memoryReply(files))
  const view = render(<ProjectMemory dir={DIR} />)
  await flush()
  return view
}
const unfold = async () => {
  fireEvent.click(screen.getByRole("button", { name: /Mémoire du projet/ }))
  await flush()
}
const openMenu = async (name: string) => {
  fireEvent.contextMenu(screen.getByText(name))
  await flush()
}
const choose = async (label: string | RegExp) => {
  fireEvent.click(await screen.findByText(label))
  await flush()
}

beforeEach(() => {
  window.history.replaceState(null, "", "/")
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("ProjectMemory", () => {
  it("rien sans fichier ni pour un autre projet", async () => {
    const { container } = await mount([])
    expect(container.textContent).toBe("")
    cleanup()
    vi.spyOn(api, "memory").mockResolvedValue(memoryReply([file("m/x/a.md", "a.md")]))
    const other = render(<ProjectMemory dir="-home-bob-autre" />)
    await flush()
    expect(other.container.textContent).toBe("")
  })

  it("résumé replié : nombre de fichiers et tokens des fichiers chargés à chaque session", async () => {
    await mount([file("m/x/a.md", "a.md", { always: true, tokens: 1200 }), file("m/x/b.md", "b.md", { tokens: 5000 })])
    const header = screen.getByRole("button", { name: /Mémoire du projet/ })
    expect(header.getAttribute("aria-expanded")).toBe("false")
    expect(header.textContent).toContain("2 fichiers")
    expect(header.textContent).toMatch(/1\s200 tokens/)
    expect(screen.queryByText("a.md")).toBeNull()
  })

  it("aucun fichier chargé en permanence : pas de mention de tokens", async () => {
    await mount([file("m/x/a.md", "a.md")])
    expect(screen.getByRole("button", { name: /Mémoire du projet/ }).textContent).not.toContain("tokens chargés")
  })

  it("dépliée : les fichiers toujours chargés d'abord, puis par nom", async () => {
    await mount([file("r/x/a.md", "a-regle.md"), file("m/x/b.md", "b.md"), file("m/x/z.md", "z-toujours.md", { always: true })])
    await unfold()
    const names = screen.getAllByRole("listitem").map((item) => item.querySelector("span.truncate")?.textContent)
    expect(names).toEqual(["z-toujours.md", "a-regle.md", "b.md"])
  })

  it("règle scopée et règle désactivée sont signalées", async () => {
    await mount([file("r/x/s.md", "s.md", { paths: ["src/**"] }), file("r/x/o.md", "o.md", { off: true })])
    await unfold()
    expect(screen.getByText("scopée")).toBeTruthy()
    expect(screen.getByText("o.md").className).toContain("line-through")
  })

  it("relue au retour sur l'onglet", async () => {
    const memorySpy = vi.spyOn(api, "memory").mockResolvedValue(memoryReply([file("m/x/a.md", "a.md")]))
    render(<ProjectMemory dir={DIR} />)
    await flush()
    memorySpy.mockResolvedValue(memoryReply([file("m/x/a.md", "a.md"), file("m/x/b.md", "b.md")]))
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible")
    await act(async () => void document.dispatchEvent(new Event("visibilitychange")))
    expect(screen.getByRole("button", { name: /Mémoire du projet/ }).textContent).toContain("2 fichiers")
  })

  it("la flèche ouvre la page Mémoire sur ce fichier", async () => {
    await mount([file("m/x/a.md", "a.md")])
    await unfold()
    fireEvent.click(screen.getByRole("button", { name: "Ouvrir a.md dans la page Mémoire" }))
    expect(window.location.pathname + window.location.search).toBe(href({ kind: "memory", key: "m/x/a.md" }))
  })

  describe("fenêtre d'édition", () => {
    it("un clic affiche l'aperçu mis en forme, front matter à part", async () => {
      vi.spyOn(api, "memoryFile").mockResolvedValue({ text: "---\nname: a\n---\n# Titre du corps" })
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      fireEvent.click(screen.getByText("a.md"))
      await flush()
      expect(api.memoryFile).toHaveBeenCalledWith("m/x/a.md")
      expect(screen.getByText(/name: a/).tagName).toBe("PRE")
      expect(screen.getByRole("heading", { name: "Titre du corps" })).toBeTruthy()
    })

    it("Modifier passe au texte brut ; Enregistrer envoie le texte saisi puis revient à l'aperçu et relit la liste", async () => {
      vi.spyOn(api, "memoryFile").mockResolvedValue({ text: "ancien" })
      const saveSpy = vi.spyOn(api, "memorySave").mockResolvedValue({ message: "Enregistré" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      fireEvent.click(screen.getByText("a.md"))
      await flush()
      fireEvent.click(screen.getByRole("button", { name: /Modifier/ }))
      const editor = screen.getByLabelText("Contenu de a.md") as HTMLTextAreaElement
      expect(editor.value).toBe("ancien")
      fireEvent.change(editor, { target: { value: "nouveau" } })
      const memoryCallsBefore = vi.mocked(api.memory).mock.calls.length
      fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }))
      await flush()
      expect(saveSpy).toHaveBeenCalledWith("m/x/a.md", "nouveau")
      expect(screen.queryByLabelText("Contenu de a.md")).toBeNull()
      expect(vi.mocked(api.memory).mock.calls.length).toBeGreaterThan(memoryCallsBefore)
    })

    it("Ctrl+Entrée enregistre", async () => {
      vi.spyOn(api, "memoryFile").mockResolvedValue({ text: "t" })
      const saveSpy = vi.spyOn(api, "memorySave").mockResolvedValue({ message: "ok" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      await choose("Modifier ici")
      fireEvent.keyDown(screen.getByLabelText("Contenu de a.md"), { key: "Enter", ctrlKey: true })
      await flush()
      expect(saveSpy).toHaveBeenCalledWith("m/x/a.md", "t")
    })

    it("échec d'enregistrement : reste en édition", async () => {
      vi.spyOn(api, "memoryFile").mockResolvedValue({ text: "t" })
      vi.spyOn(api, "memorySave").mockRejectedValue(new ApiError(500, "boom", "disque plein"))
      vi.spyOn(toast, "error").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      await choose("Modifier ici")
      fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }))
      await flush()
      expect(screen.getByLabelText("Contenu de a.md")).toBeTruthy()
    })

    it("fichier illisible : toast et fermeture", async () => {
      vi.spyOn(api, "memoryFile").mockRejectedValue(new ApiError(500, "boom", "x"))
      const toastError = vi.spyOn(toast, "error").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      fireEvent.click(screen.getByText("a.md"))
      await flush()
      expect(toastError).toHaveBeenCalledWith("Fichier illisible")
      expect(screen.queryByRole("dialog")).toBeNull()
    })
  })

  describe("menu contextuel", () => {
    it("changer le type d'une entrée de mémoire", async () => {
      const typeSpy = vi.spyOn(api, "memoryType").mockResolvedValue({ message: "ok" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md", { type: "user" })])
      await unfold()
      await openMenu("a.md")
      fireEvent.click(await screen.findByText("Type"))
      await choose(/^feedback/)
      expect(typeSpy).toHaveBeenCalledWith("m/x/a.md", "feedback")
    })

    it("« Sans type » efface le type", async () => {
      const typeSpy = vi.spyOn(api, "memoryType").mockResolvedValue({ message: "ok" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      fireEvent.click(await screen.findByText("Type"))
      await choose(/Sans type/)
      expect(typeSpy).toHaveBeenCalledWith("m/x/a.md", "")
    })

    it("pas de choix de type pour l'index MEMORY.md ni pour une règle", async () => {
      await mount([file("m/x/MEMORY.md", "MEMORY.md"), file("r/x/regle.md", "regle.md")])
      await unfold()
      await openMenu("MEMORY.md")
      await screen.findByText("Afficher")
      expect(screen.queryByText("Type")).toBeNull()
      cleanup()
      await mount([file("r/x/regle.md", "regle.md")])
      await unfold()
      await openMenu("regle.md")
      await screen.findByText("Afficher")
      expect(screen.queryByText("Type")).toBeNull()
    })

    it("déplacer vers les règles globales : move sans copie ; copier : avec copie", async () => {
      const moveSpy = vi.spyOn(api, "memoryMove").mockResolvedValue({ message: "ok", key: "g/rules/a.md" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("r/x/regle.md", "regle.md")])
      await unfold()
      await openMenu("regle.md")
      await choose("Déplacer vers les règles globales")
      expect(moveSpy).toHaveBeenLastCalledWith("r/x/regle.md", "global", false)
      await openMenu("regle.md")
      await choose("Copier vers les règles globales")
      expect(moveSpy).toHaveBeenLastCalledWith("r/x/regle.md", "global", true)
    })

    it("l'index MEMORY.md et un CLAUDE.md ne se déplacent pas", async () => {
      await mount([file("m/x/MEMORY.md", "MEMORY.md"), file("c/x/CLAUDE.md", "CLAUDE.md")])
      await unfold()
      await openMenu("MEMORY.md")
      await screen.findByText("Afficher")
      expect(screen.queryByText("Déplacer vers les règles globales")).toBeNull()
      cleanup()
      await mount([file("c/x/CLAUDE.md", "CLAUDE.md")])
      await unfold()
      await openMenu("CLAUDE.md")
      await screen.findByText("Afficher")
      expect(screen.queryByText("Déplacer vers les règles globales")).toBeNull()
    })

    it("ouvrir dans la page Mémoire", async () => {
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      await choose("Ouvrir dans la page Mémoire")
      expect(window.location.pathname + window.location.search).toBe(href({ kind: "memory", key: "m/x/a.md" }))
    })

    it("suppression : confirmation d'abord, annulation sans appel", async () => {
      const removeSpy = vi.spyOn(api, "memoryRemove").mockResolvedValue({ message: "ok" } as never)
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      await choose("Supprimer")
      expect(screen.getByText("Supprimer a.md ?")).toBeTruthy()
      expect(screen.getByText(/Sa ligne dans MEMORY.md est retirée/)).toBeTruthy()
      fireEvent.click(screen.getByRole("button", { name: "Annuler" }))
      await flush()
      expect(removeSpy).not.toHaveBeenCalled()
    })

    it("suppression confirmée : supprime la clé et relit la liste ; message adapté à un fichier hors mémoire", async () => {
      const removeSpy = vi.spyOn(api, "memoryRemove").mockResolvedValue({ message: "ok" } as never)
      vi.spyOn(toast, "success").mockReturnValue("")
      await mount([file("r/x/regle.md", "regle.md")])
      await unfold()
      await openMenu("regle.md")
      await choose("Supprimer")
      expect(screen.getByText(/une copie reste dans la corbeille/)).toBeTruthy()
      const memoryCallsBefore = vi.mocked(api.memory).mock.calls.length
      fireEvent.click(screen.getByRole("button", { name: "Supprimer" }))
      await flush()
      expect(removeSpy).toHaveBeenCalledWith(["r/x/regle.md"])
      expect(vi.mocked(api.memory).mock.calls.length).toBeGreaterThan(memoryCallsBefore)
    })

    it("copier le contenu lit le fichier ; échec de lecture : toast d'erreur", async () => {
      vi.spyOn(api, "memoryFile").mockRejectedValue(new ApiError(500, "boom", "illisible"))
      const toastError = vi.spyOn(toast, "error").mockReturnValue("")
      await mount([file("m/x/a.md", "a.md")])
      await unfold()
      await openMenu("a.md")
      await choose("Copier le contenu")
      expect(api.memoryFile).toHaveBeenCalledWith("m/x/a.md")
      expect(toastError).toHaveBeenCalledWith(expect.stringContaining("illisible"))
    })
  })
})
