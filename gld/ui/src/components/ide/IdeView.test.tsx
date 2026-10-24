// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsEntry, type FsRes } from "@/lib/api"
import { IdeView } from "./IdeView"

const feed = vi.hoisted(() => ({ setTsLib: vi.fn(), clearTsLibs: vi.fn() }))
const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn() }))
vi.mock("sonner", async (orig) => ({ ...(await orig<typeof import("sonner")>()), toast }))

// Monaco ne tourne pas sous jsdom : l'éditeur simulé garde la valeur telle quelle (un textarea normaliserait les CRLF).
vi.mock("./CodeEditor", () => ({
  ...feed,
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

class FakeES {
  static all: FakeES[] = []
  onopen: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  closed = false
  url: string
  constructor(url: string) {
    this.url = url
    FakeES.all.push(this)
  }
  close() {
    this.closed = true
  }
  open() {
    act(() => this.onopen?.(new Event("open")))
  }
  emit(data: object | string) {
    act(() => this.onmessage?.({ data: typeof data === "string" ? data : JSON.stringify(data) } as MessageEvent))
  }
}
const stream = async () => {
  await waitFor(() => expect(FakeES.all.filter((x) => !x.closed)).toHaveLength(1))
  return FakeES.all.find((x) => !x.closed)!
}

let file: MockInstance<typeof api.fsFile>
let save: MockInstance<typeof api.fsSave>
let index: MockInstance<typeof api.fsIndex>
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  FakeES.all = []
  vi.stubGlobal("EventSource", FakeES)
  feed.setTsLib.mockReset()
  feed.clearTsLibs.mockReset()
  toast.mockReset()
  index = vi.spyOn(api, "fsIndex").mockResolvedValue(res(200, { state: "ready", files: [] }))
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
  vi.unstubAllGlobals()
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

const sleep = (ms: number) => act(() => new Promise((r) => setTimeout(r, ms)))
const disk = (over: Record<string, FsRes<{ content: string; etag: string }>>) => {
  const base = file.getMockImplementation()!
  file.mockImplementation(async (d, p) => over[p] ?? base(d, p))
}
const ed = (p: string) => screen.getByTestId(`ed:${p}`).textContent

describe("IdeView, évènements disque", () => {
  it("write sur un onglet propre : relu et remplacé sans bandeau, toast discret", async () => {
    await openA()
    const es = await stream()
    disk({ "a.txt": res(200, { content: "neuf", etag: "e9" }) })
    es.emit({ path: "a.txt", type: "write" })
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("neuf")))
    expect(screen.queryByRole("alert")).toBeNull()
    await waitFor(() => expect(toast).toHaveBeenCalledTimes(1))
    expect(toast.mock.calls[0][0]).toContain("a.txt")
  })

  it("write sur un onglet sale : bandeau de conflit, contenu intact", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    const es = await stream()
    disk({ "a.txt": res(200, { content: "neuf", etag: "e9" }) })
    es.emit({ path: "a.txt", type: "write" })
    expect(await screen.findByText(/modifié sur le disque/)).toBeTruthy()
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nx"))
  })

  it("remove d'un fichier ouvert : bandeau « supprimé »", async () => {
    await openA()
    const es = await stream()
    disk({ "a.txt": res(404, { ok: false, out: "introuvable" }) })
    es.emit({ path: "a.txt", type: "remove" })
    expect(await screen.findByText(/supprimé du disque/)).toBeTruthy()
  })

  it("écho de notre propre enregistrement : ni rechargement, ni conflit, ni toast", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
    const es = await stream()
    disk({ "a.txt": res(200, { content: "l1\r\nl2\r\nx", etag: "e2" }) })
    const calls = file.mock.calls.length
    es.emit({ path: "a.txt", type: "write" })
    await waitFor(() => expect(file.mock.calls.length).toBe(calls + 1))
    await sleep(500)
    expect(screen.queryByRole("alert")).toBeNull()
    expect(toast).not.toHaveBeenCalled()
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nx"))
  })

  it("plusieurs rechargements rapprochés : un seul toast", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    await openFile("b.txt")
    const es = await stream()
    disk({ "a.txt": res(200, { content: "A", etag: "e9" }), "b.txt": res(200, { content: "B", etag: "e9" }) })
    es.emit({ path: "a.txt", type: "write" })
    es.emit({ path: "b.txt", type: "write" })
    await waitFor(() => expect(ed("b.txt")).toBe(JSON.stringify("B")))
    await sleep(600)
    expect(toast).toHaveBeenCalledTimes(1)
    expect(toast.mock.calls[0][0]).toContain("2")
  })

  it("create dans un dossier déplié : ce dossier est relu ; l'index est resondé une fois", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    const tree = vi.mocked(api.fsTree)
    render(<IdeView dir="/p" />)
    const es = await stream()
    await waitFor(() => expect(index).toHaveBeenCalledTimes(1))
    const n = (p: string) => tree.mock.calls.filter((c) => c[1] === p).length
    const before = { src: n("src"), root: n(""), other: n("other") }
    es.emit({ path: "src/new.ts", type: "create" })
    es.emit({ path: "x.ts", type: "create" })
    es.emit({ path: "other/y.ts", type: "create" })
    await waitFor(() => expect(n("src")).toBe(before.src + 1))
    await waitFor(() => expect(n("")).toBe(before.root + 1))
    expect(n("other")).toBe(before.other)
    await waitFor(() => expect(index).toHaveBeenCalledTimes(2), { timeout: 2500 })
    await sleep(1200)
    expect(index).toHaveBeenCalledTimes(2)
  })

  it("JSON malformé : ignoré", async () => {
    await openA()
    const es = await stream()
    es.emit("{pas du json")
    es.emit({ nope: 1 })
    await sleep(300)
    expect(file).toHaveBeenCalledTimes(1)
  })

  it("resync : onglet propre relu, onglet sale en conflit, fichier disparu signalé, dossiers et index relus", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    const tree = vi.mocked(api.fsTree)
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    await openFile("c.txt")
    await openFile("b.txt")
    fireEvent.change(screen.getByLabelText("type:b.txt"), { target: { value: "x" } })
    const es = await stream()
    await waitFor(() => expect(index).toHaveBeenCalledTimes(1))
    const n = (p: string) => tree.mock.calls.filter((c) => c[1] === p).length
    const before = { src: n("src"), root: n("") }
    disk({ "a.txt": res(200, { content: "A", etag: "e9" }), "b.txt": res(200, { content: "B", etag: "e9" }), "c.txt": res(404, { ok: false }) })
    es.emit({ path: "", type: "resync" })
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("A")))
    expect(await screen.findByText(/modifié sur le disque/)).toBeTruthy()
    expect(ed("b.txt")).toBe(JSON.stringify("l1\r\nl2\r\nx"))
    fireEvent.click(screen.getByRole("tab", { name: /c\.txt/ }))
    expect(await screen.findByText(/supprimé du disque/)).toBeTruthy()
    await waitFor(() => expect(n("src")).toBe(before.src + 1))
    expect(n("")).toBe(before.root + 1)
    await waitFor(() => expect(index).toHaveBeenCalledTimes(2), { timeout: 2500 })
  })

  it("une réouverture après coupure vaut resync, pas la première ouverture", async () => {
    await openA()
    const es = await stream()
    es.open()
    await sleep(300)
    expect(file).toHaveBeenCalledTimes(1)
    disk({ "a.txt": res(200, { content: "neuf", etag: "e9" }) })
    es.open()
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("neuf")))
  })

  it("dossiers déclarés : racine et dépliés, plafonnés à 200 en gardant les moins profonds", async () => {
    const deep = Array.from({ length: 150 }, (_, i) => `d/sub${i}`)
    const shallow = Array.from({ length: 100 }, (_, i) => `top${i}`)
    localStorage.setItem("gld.ide./p.open", JSON.stringify([...deep, ...shallow, "a b,c"]))
    render(<IdeView dir="/p" />)
    const es = await stream()
    const u = new URL(es.url, "http://x")
    expect(u.pathname).toBe("/fs-watch")
    expect(u.searchParams.get("dir")).toBe("/p")
    const paths = u.searchParams.getAll("paths")
    expect(paths).toHaveLength(200)
    expect(paths[0]).toBe("")
    for (const s of [...shallow, "a b,c"]) expect(paths).toContain(s)
  })

  it("dossiers déclarés : la requête reste sous 6000 octets, les plus profonds sont écartés d'abord", async () => {
    const deep = Array.from({ length: 100 }, (_, i) => `deep/${"x".repeat(60)}${i}`)
    const shallow = Array.from({ length: 50 }, (_, i) => `s${i}`)
    localStorage.setItem("gld.ide./p.open", JSON.stringify([...deep, ...shallow]))
    render(<IdeView dir="/p" />)
    const es = await stream()
    const u = new URL(es.url, "http://x")
    expect(u.search.length).toBeLessThanOrEqual(6000)
    const paths = u.searchParams.getAll("paths")
    expect(paths.length).toBeGreaterThan(51)
    expect(paths.length).toBeLessThan(151)
    for (const s of ["", ...shallow]) expect(paths).toContain(s)
  })

  it("le flux est rouvert quand les dossiers dépliés changent, fermé au démontage", async () => {
    vi.mocked(api.fsTree).mockImplementation(async (_d, p) => ({ entries: p === "" ? [e("src", "dir")] : [e("b.txt", "file")], truncated: false }))
    const r = render(<IdeView dir="/p" />)
    const first = await stream()
    expect(new URL(first.url, "http://x").searchParams.getAll("paths")).toEqual([""])
    fireEvent.click(await screen.findByLabelText("Déplier src"))
    await waitFor(() => expect(first.closed).toBe(true))
    const second = await stream()
    expect(new URL(second.url, "http://x").searchParams.getAll("paths")).toEqual(["", "src"])
    r.unmount()
    expect(second.closed).toBe(true)
  })
})

describe("IdeView, ouverture rapide et alimentation TypeScript", () => {
  it("Ctrl+P dans l'espace ouvre la liste, Entrée ouvre le fichier ; hors de l'espace rien n'est bloqué", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.txt", size: 1, mtime: 1 }, { path: "src/b.txt", size: 1, mtime: 1 }] }))
    render(<IdeView dir="/p" />)
    await screen.findByText("a.txt", { selector: "[role=treeitem] span" })
    expect(fireEvent.keyDown(document.body, { key: "p", ctrlKey: true })).toBe(true)
    expect(screen.queryByRole("combobox")).toBeNull()
    expect(fireEvent.keyDown(screen.getByRole("tree"), { key: "p", ctrlKey: true })).toBe(false)
    const input = await screen.findByRole("combobox")
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(2))
    fireEvent.change(input, { target: { value: "b" } })
    fireEvent.keyDown(input, { key: "Enter" })
    await screen.findByRole("tab", { name: /b\.txt/ })
    expect(file).toHaveBeenCalledWith("/p", "src/b.txt")
    expect(screen.queryByRole("combobox")).toBeNull()
  })

  it("alimente le service TypeScript avec les seuls fichiers de l'index, vidé au démontage", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 3, mtime: 1 }, { path: "b.json", size: 2, mtime: 1 }, { path: "x.go", size: 1, mtime: 1 }] }))
    const files = vi.spyOn(api, "fsFiles").mockImplementation(async (_d, ps) => res(200, { files: ps.map((path) => ({ path, content: "c:" + path })) }))
    const r = render(<IdeView dir="/p" />)
    await waitFor(() => expect(feed.setTsLib).toHaveBeenCalledTimes(2))
    expect(files.mock.calls[0][1].sort()).toEqual(["a.ts", "b.json"])
    expect(feed.setTsLib).toHaveBeenCalledWith("a.ts", "c:a.ts")
    r.unmount()
    await waitFor(() => expect(feed.clearTsLibs).toHaveBeenCalled())
  })

  it("l'alimentation s'arrête au démontage", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 3, mtime: 1 }] }))
    const pending = deferred<FsRes<{ files: { path: string; content: string }[] }>>()
    const files = vi.spyOn(api, "fsFiles").mockReturnValue(pending.promise)
    const r = render(<IdeView dir="/p" />)
    await waitFor(() => expect(files).toHaveBeenCalled())
    r.unmount()
    pending.resolve(res(200, { files: [{ path: "a.ts", content: "x" }] }))
    await sleep(50)
    expect(feed.setTsLib).not.toHaveBeenCalled()
  })

  it("un fichier alimenté qui change est remplacé, un fichier disparu retiré", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 3, mtime: 1 }, { path: "b.ts", size: 3, mtime: 1 }] }))
    vi.spyOn(api, "fsFiles").mockImplementation(async (_d, ps) => res(200, { files: ps.map((path) => ({ path, content: "v" + path })) }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(feed.setTsLib).toHaveBeenCalledTimes(2))
    const es = await stream()
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 4, mtime: 2 }] }))
    es.emit({ path: "a.ts", type: "write" })
    await waitFor(() => expect(feed.setTsLib).toHaveBeenCalledWith("b.ts", null), { timeout: 2500 })
    expect(feed.setTsLib.mock.calls.filter((c) => c[0] === "a.ts")).toHaveLength(2)
  })
})

describe("IdeView, évènement pendant une sauvegarde", () => {
  const inFlight = async () => {
    const pending = deferred<FsRes<{ etag: string; conflict: boolean; deleted: boolean }>>()
    save.mockReturnValueOnce(pending.promise)
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    const es = await stream()
    return { input, es, done: () => act(async () => pending.resolve(res(200, { ok: true, etag: "e2" }))) }
  }

  it("changement extérieur pendant la sauvegarde, onglet propre ensuite : rechargé une fois la sauvegarde finie", async () => {
    const { es, done } = await inFlight()
    disk({ "a.txt": res(200, { content: "ext", etag: "e7" }) })
    es.emit({ path: "a.txt", type: "write" })
    await sleep(400)
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nx"))
    await done()
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("ext")))
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("changement extérieur pendant la sauvegarde, onglet sale ensuite : conflit, contenu intact", async () => {
    const { input, es, done } = await inFlight()
    fireEvent.change(input, { target: { value: "y" } })
    disk({ "a.txt": res(200, { content: "ext", etag: "e7" }) })
    es.emit({ path: "a.txt", type: "write" })
    await sleep(400)
    await done()
    expect(await screen.findByText(/modifié sur le disque/)).toBeTruthy()
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nxy"))
  })

  it("seul l'écho de la sauvegarde en cours : rien", async () => {
    const { es, done } = await inFlight()
    disk({ "a.txt": res(200, { content: "l1\r\nl2\r\nx", etag: "e2" }) })
    es.emit({ path: "a.txt", type: "write" })
    await sleep(400)
    await done()
    await sleep(700)
    expect(screen.queryByRole("alert")).toBeNull()
    expect(toast).not.toHaveBeenCalled()
    expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull()
  })

  it("réponses dans le désordre : une lecture plus ancienne n'écrase pas la plus récente", async () => {
    await openA()
    const es = await stream()
    const old = deferred<FsRes<{ content: string; etag: string }>>()
    const base = file.getMockImplementation()!
    let n = 0
    file.mockImplementation(async (d, p) => (p !== "a.txt" ? base(d, p) : ++n === 1 ? old.promise : res(200, { content: "v2", etag: "e9" })))
    es.emit({ path: "a.txt", type: "write" })
    await waitFor(() => expect(n).toBe(1))
    es.emit({ path: "", type: "resync" })
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("v2")))
    await act(async () => old.resolve(res(200, { content: "v1", etag: "e8" })))
    await sleep(100)
    expect(ed("a.txt")).toBe(JSON.stringify("v2"))
  })
})

describe("IdeView : onglets restaurés au rechargement", () => {
  const KEY = "gld.ide./p.tabs"
  const tabs = () => screen.queryAllByRole("tab").map((x) => x.getAttribute("data-path"))
  const activeTab = () => screen.getAllByRole("tab").find((x) => x.getAttribute("aria-selected") === "true")?.getAttribute("data-path")
  const stored = () => JSON.parse(localStorage.getItem(KEY) ?? "null")

  it("rouvre les onglets mémorisés dans l'ordre, par le flux normal, et active le bon", async () => {
    localStorage.setItem(KEY, JSON.stringify({ paths: ["b.txt", "mix.txt", "big.bin", "a.txt"], active: "mix.txt" }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(tabs()).toEqual(["b.txt", "mix.txt", "big.bin", "a.txt"]))
    expect(activeTab()).toBe("mix.txt")
    expect(screen.getByText(/mélange des fins de ligne/)).toBeTruthy()
    expect(document.activeElement).toBe(document.body)
  })

  it("un fichier disparu est sauté sans message", async () => {
    file.mockImplementation(async (_d, p) => (p === "gone.txt" ? res(404, { error: "absent" }) : res(200, { content: "x", etag: "e1", size: 1, mtime: 1 })))
    localStorage.setItem(KEY, JSON.stringify({ paths: ["gone.txt", "c.txt"], active: "gone.txt" }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(tabs()).toEqual(["c.txt"]))
    expect(activeTab()).toBe("c.txt")
    expect(toast.error).not.toHaveBeenCalled()
    await waitFor(() => expect(stored()).toEqual({ paths: ["c.txt"], active: "c.txt" }))
  })

  it("un stockage illisible est ignoré", async () => {
    for (const junk of ["{", "42", JSON.stringify({ paths: "a.txt" }), JSON.stringify({ paths: [1, null, "a.txt"], active: 3 })]) {
      localStorage.setItem(KEY, junk)
      render(<IdeView dir="/p" />)
      await screen.findByText("a.txt", { selector: "[role=treeitem] span" })
      if (junk.includes('"a.txt"]')) await waitFor(() => expect(tabs()).toEqual(["a.txt"]))
      else expect(tabs()).toEqual([])
      cleanup()
    }
  })

  it("la liste est bornée à 30 chemins", async () => {
    const many = Array.from({ length: 50 }, (_, i) => `f${i}.txt`)
    localStorage.setItem(KEY, JSON.stringify({ paths: many, active: "f45.txt" }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(tabs()).toHaveLength(30))
    expect(file).toHaveBeenCalledTimes(30)
    expect(stored().paths).toEqual(many.slice(0, 30))
  })

  it("ouvrir et fermer mettent à jour la liste ; le contenu, même non enregistré, n'est jamais mémorisé", async () => {
    const input = await openA()
    await openFile("b.txt")
    await waitFor(() => expect(stored()).toEqual({ paths: ["a.txt", "b.txt"], active: "b.txt" }))
    fireEvent.click(screen.getByRole("tab", { name: /a\.txt/ }))
    fireEvent.change(input, { target: { value: "SECRET" } })
    await waitFor(() => expect(stored()).toEqual({ paths: ["a.txt", "b.txt"], active: "a.txt" }))
    fireEvent.click(screen.getByLabelText("Fermer b.txt"))
    await waitFor(() => expect(stored()).toEqual({ paths: ["a.txt"], active: "a.txt" }))
    expect(Object.values(localStorage).join("")).not.toContain("SECRET")
    expect(Object.values(localStorage).join("")).not.toContain("l1")
  })
})
