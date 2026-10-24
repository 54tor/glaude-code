// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsEntry, type FsRes } from "@/lib/api"
import { IdeView } from "./IdeView"

// Monaco ne tourne pas sous jsdom : l'éditeur simulé garde la valeur telle quelle (un textarea normaliserait les CRLF).
vi.mock("./CodeEditor", () => ({
  default: ({ path, value, readOnly, onChange, onSave }: { path: string; value: string; readOnly?: boolean; onChange(v: string): void; onSave(): void }) => (
    <div>
      <pre data-testid={`ed:${path}`}>{JSON.stringify(value)}</pre>
      <input
        aria-label={`type:${path}`}
        readOnly={readOnly}
        value=""
        onChange={(e) => onChange(value + e.target.value)}
        onKeyDown={(e) => {
          if (e.ctrlKey && e.key === "s") {
            e.preventDefault()
            onSave()
          }
        }}
      />
    </div>
  ),
}))
vi.mock("./ConflictDiff", () => ({
  ConflictDiff: ({ original, modified }: { original: string; modified: string }) => <div data-testid="diff">{JSON.stringify([original, modified])}</div>,
}))

const e = (name: string, type: FsEntry["type"]): FsEntry => ({ name, type, size: 1, mtime: 1 })
const res = <T,>(status: number, body: object): FsRes<T> => ({ status, ok: status < 300 && (body as { ok?: boolean }).ok !== false, body: body as FsRes<T>["body"] })

let file: MockInstance<typeof api.fsFile>
let save: MockInstance<typeof api.fsSave>
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  vi.spyOn(api, "fsTree").mockResolvedValue({ entries: [e("a.txt", "file"), e("b.txt", "file"), e("c.txt", "file"), e("lf.txt", "file"), e("mix.txt", "file"), e("cr.txt", "file"), e("big.bin", "file"), e("img.png", "file")], truncated: false })
  file = vi.spyOn(api, "fsFile").mockImplementation(async (_d, p) => {
    if (p === "big.bin") return res(413, { error: "trop gros" })
    if (p === "img.png") return res(415, { error: "binaire" })
    if (p === "lf.txt") return res(200, { content: "a\nb\n", etag: "e1", size: 4, mtime: 1 })
    if (p === "mix.txt") return res(200, { content: "a\r\nb\n", etag: "e1", size: 5, mtime: 1 })
    if (p === "cr.txt") return res(200, { content: "a\rb", etag: "e1", size: 3, mtime: 1 })
    return res(200, { content: "l1\r\nl2\r\n", etag: "e1", size: 8, mtime: 1 })
  })
  save = vi.spyOn(api, "fsSave").mockResolvedValue(res(200, { ok: true, etag: "e2" }))
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  setAuth({ state: "loading", rights: undefined, home: undefined })
})

const openA = async () => {
  render(<IdeView dir="/p" />)
  fireEvent.click(await screen.findByText("a.txt", { selector: "[role=treeitem] span" }))
  return screen.findByLabelText("type:a.txt")
}
const ctrlS = (el: Element) => fireEvent.keyDown(el, { key: "s", ctrlKey: true })
const openFile = async (name: string) => {
  fireEvent.click(await screen.findByText(name, { selector: "[role=treeitem] span" }))
  await screen.findByRole("tab", { name: new RegExp(name.replace(".", "\\.")) })
}
const deferred = <T,>() => {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

describe("IdeView", () => {
  it("ouvrir un fichier crée un onglet, un second clic ne le duplique pas", async () => {
    await openA()
    expect(file).toHaveBeenCalledWith("/p", "a.txt")
    expect(screen.getAllByRole("tab")).toHaveLength(1)
    fireEvent.click(screen.getByText("a.txt", { selector: "[role=treeitem] span" }))
    expect(screen.getAllByRole("tab")).toHaveLength(1)
    expect(file).toHaveBeenCalledTimes(1)
  })

  it("saisir rend l'onglet sale, Ctrl+S enregistre avec le contenu et l'etag exacts", async () => {
    const input = await openA()
    expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull()
    fireEvent.change(input, { target: { value: "x" } })
    expect(screen.getByLabelText("Modifications non enregistrées")).toBeTruthy()
    ctrlS(input)
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
    expect(save.mock.calls[0].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\nx", "e1"])
    expect(save.mock.calls[0][4]).toBeFalsy()
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
  })

  it("aller-retour CRLF : le contenu envoyé est celui reçu à l'octet près", async () => {
    const input = await openA()
    expect(screen.getByTestId("ed:a.txt").textContent).toBe(JSON.stringify("l1\r\nl2\r\n"))
    ctrlS(input)
    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0][2]).toBe("l1\r\nl2\r\n")
  })

  it("Ctrl+S hors de l'éditeur enregistre l'onglet actif et bloque l'enregistrement du navigateur", async () => {
    await openA()
    expect(ctrlS(screen.getByRole("tablist"))).toBe(false)
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
  })

  it("409 : bandeau à trois actions ; « Écraser » rappelle avec force", async () => {
    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: false, etag: "e5" }))
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    const bar = await screen.findByRole("alert")
    for (const n of ["Recharger", "Écraser", "Voir les différences"]) expect(within(bar).getByRole("button", { name: n })).toBeTruthy()
    expect(screen.getByTestId("ed:a.txt").textContent).toBe(JSON.stringify("l1\r\nl2\r\nx"))
    fireEvent.click(within(bar).getByRole("button", { name: "Écraser" }))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save.mock.calls[1]).toEqual(["/p", "a.txt", "l1\r\nl2\r\nx", "e1", true])
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
  })

  it("« Recharger » remplace le contenu par celui du disque", async () => {
    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: false, etag: "e5" }))
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    const bar = await screen.findByRole("alert")
    file.mockResolvedValueOnce(res(200, { content: "disque", etag: "e5", size: 6, mtime: 2 }))
    fireEvent.click(within(bar).getByRole("button", { name: "Recharger" }))
    await waitFor(() => expect(screen.getByTestId("ed:a.txt").textContent).toBe(JSON.stringify("disque")))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull()
  })

  it("« Voir les différences » compare la version du disque à la vôtre", async () => {
    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: false, etag: "e5" }))
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    const bar = await screen.findByRole("alert")
    file.mockResolvedValueOnce(res(200, { content: "disque", etag: "e5", size: 6, mtime: 2 }))
    fireEvent.click(within(bar).getByRole("button", { name: "Voir les différences" }))
    expect((await screen.findByTestId("diff")).textContent).toBe(JSON.stringify(["disque", "l1\r\nl2\r\nx"]))
  })

  it("fichier supprimé : « Enregistrer sous le même nom » recrée le fichier, « Fermer » ferme l'onglet", async () => {
    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: true, etag: "" }))
    const input = await openA()
    ctrlS(input)
    const bar = await screen.findByRole("alert")
    fireEvent.click(within(bar).getByRole("button", { name: "Enregistrer sous le même nom" }))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save.mock.calls[1].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\n", ""])
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())

    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: true, etag: "" }))
    ctrlS(input)
    fireEvent.click(within(await screen.findByRole("alert")).getByRole("button", { name: "Fermer" }))
    await waitFor(() => expect(screen.queryByRole("tab")).toBeNull())
  })

  it("fermer un onglet sale demande confirmation", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.click(screen.getByRole("button", { name: "Fermer a.txt" }))
    expect(await screen.findByText("Fermer « a.txt » sans enregistrer ?")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Annuler" }))
    await waitFor(() => expect(screen.queryByText("Fermer « a.txt » sans enregistrer ?")).toBeNull())
    expect(screen.getAllByRole("tab")).toHaveLength(1)
    fireEvent.click(screen.getByRole("button", { name: "Fermer a.txt" }))
    fireEvent.click(await screen.findByRole("button", { name: "Fermer sans enregistrer" }))
    await waitFor(() => expect(screen.queryByRole("tab")).toBeNull())
  })

  it("fermer un onglet propre ne demande rien", async () => {
    await openA()
    fireEvent.click(screen.getByRole("button", { name: "Fermer a.txt" }))
    expect(screen.queryByRole("tab")).toBeNull()
  })

  it("413 et 415 à l'ouverture : onglet en lecture seule avec un message", async () => {
    render(<IdeView dir="/p" />)
    fireEvent.click(await screen.findByText("big.bin", { selector: "[role=treeitem] span" }))
    expect(await screen.findByText(/trop volumineux/)).toBeTruthy()
    fireEvent.click(screen.getByText("img.png", { selector: "[role=treeitem] span" }))
    expect(await screen.findByText(/n'est pas un fichier texte/)).toBeTruthy()
    expect(screen.getAllByRole("tab")).toHaveLength(2)
    ctrlS(screen.getByRole("tablist"))
    expect(save).not.toHaveBeenCalled()
  })

  it("fins de ligne mixtes (ou CR isolé) : lecture seule avec message, jamais sale, Ctrl+S sans effet", async () => {
    render(<IdeView dir="/p" />)
    for (const name of ["mix.txt", "cr.txt"]) {
      await openFile(name)
      const input = await screen.findByLabelText(`type:${name}`)
      expect((input as HTMLInputElement).readOnly).toBe(true)
      expect(screen.getAllByText(/mélange des fins de ligne/).length).toBeGreaterThan(0)
      fireEvent.change(input, { target: { value: "x" } })
      expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull()
      ctrlS(input)
    }
    expect(save).not.toHaveBeenCalled()
  })

  it("fins de ligne uniformes LF : éditable et aller-retour à l'octet près", async () => {
    render(<IdeView dir="/p" />)
    await openFile("lf.txt")
    const input = await screen.findByLabelText("type:lf.txt")
    expect((input as HTMLInputElement).readOnly).toBe(false)
    expect(screen.queryByText(/mélange des fins de ligne/)).toBeNull()
    ctrlS(input)
    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save.mock.calls[0][2]).toBe("a\nb\n")
  })

  it("utilisateur fs_read seul : onglet en lecture seule, Ctrl+S sans effet, pas de création à la racine", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } })
    const input = await openA()
    expect((input as HTMLInputElement).readOnly).toBe(true)
    ctrlS(input)
    ctrlS(screen.getByRole("tablist"))
    expect(save).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "Nouveau fichier" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Nouveau dossier" })).toBeNull()
  })

  it("deux Ctrl+S rapprochés : la seconde attend la première et part avec le nouvel etag, sans conflit", async () => {
    const first = deferred<FsRes<{ etag: string; conflict: boolean; deleted: boolean }>>()
    save.mockReturnValueOnce(first.promise).mockResolvedValueOnce(res(200, { ok: true, etag: "e3" }))
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    fireEvent.change(input, { target: { value: "y" } })
    ctrlS(input)
    ctrlS(input)
    expect(save).toHaveBeenCalledTimes(1)
    first.resolve(res(200, { ok: true, etag: "e2" }))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save.mock.calls[1].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\nxy", "e2"])
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
    expect(screen.queryByRole("alert")).toBeNull()
    expect(save).toHaveBeenCalledTimes(2)
  })

  it("une seconde demande sans saisie entre-temps est abandonnée", async () => {
    const first = deferred<FsRes<{ etag: string; conflict: boolean; deleted: boolean }>>()
    save.mockReturnValueOnce(first.promise)
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    ctrlS(input)
    first.resolve(res(200, { ok: true, etag: "e2" }))
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
    expect(save).toHaveBeenCalledTimes(1)
  })

  it("onglets au clavier : flèches, Début, Fin activent l'onglet ; Suppr le ferme", async () => {
    render(<IdeView dir="/p" />)
    for (const n of ["a.txt", "b.txt", "c.txt"]) await openFile(n)
    const tab = (n: string) => screen.getByRole("tab", { name: new RegExp(n.replace(".", "\\.")) })
    const selected = () => screen.getAllByRole("tab").filter((t) => t.getAttribute("aria-selected") === "true").map((t) => t.getAttribute("title"))
    expect(selected()).toEqual(["c.txt"])
    expect(screen.getAllByRole("tab").filter((t) => t.tabIndex === 0)).toHaveLength(1)
    tab("c.txt").focus()
    fireEvent.keyDown(tab("c.txt"), { key: "ArrowLeft" })
    expect(selected()).toEqual(["b.txt"])
    expect(document.activeElement).toBe(tab("b.txt"))
    expect(tab("b.txt").tabIndex).toBe(0)
    fireEvent.keyDown(tab("b.txt"), { key: "Home" })
    expect(selected()).toEqual(["a.txt"])
    fireEvent.keyDown(tab("a.txt"), { key: "ArrowLeft" })
    expect(selected()).toEqual(["c.txt"])
    fireEvent.keyDown(tab("c.txt"), { key: "ArrowRight" })
    expect(selected()).toEqual(["a.txt"])
    fireEvent.keyDown(tab("a.txt"), { key: "End" })
    expect(selected()).toEqual(["c.txt"])
    fireEvent.keyDown(tab("c.txt"), { key: "Delete" })
    await waitFor(() => expect(screen.getAllByRole("tab")).toHaveLength(2))
    expect(selected()).toEqual(["b.txt"])
    expect(document.activeElement).toBe(tab("b.txt"))
  })
})
