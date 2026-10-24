// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type FsEntry, type FsRes } from "@/lib/api"
import { IdeView } from "./IdeView"

const feed = vi.hoisted(() => ({ setTsLib: vi.fn(), clearTsLibs: vi.fn() }))
// Une poignée simulée par chemin : on vérifie que les commandes visent l'éditeur de l'onglet actif.
const handles = vi.hoisted(() => {
  const all = new Map<string, Record<string, ReturnType<typeof vi.fn>>>()
  const of = (p: string) => {
    if (!all.has(p)) all.set(p, { undo: vi.fn(), redo: vi.fn(), find: vi.fn(), replace: vi.fn(), gotoLine: vi.fn(), palette: vi.fn(), focus: vi.fn(), zoom: vi.fn(), subscribe: vi.fn(() => () => {}) })
    return all.get(p)!
  }
  return { all, of }
})
vi.mock("../FullTerminal", () => ({
  default: ({ id, takeHand }: { id: string; takeHand?: boolean }) => <div data-testid="term" data-take={String(takeHand)}>{id}</div>,
}))
const toast = vi.hoisted(() => Object.assign(vi.fn(), { error: vi.fn() }))
vi.mock("sonner", async (orig) => ({ ...(await orig<typeof import("sonner")>()), toast }))

// Monaco ne tourne pas sous jsdom : l'éditeur simulé garde la valeur telle quelle (un textarea normaliserait les CRLF).
// focus : demande de focus de l'espace, honorée comme le ferait Monaco.
vi.mock("./CodeEditor", async () => {
  const { useEffect, useRef } = await import("react")
  const Editor = ({ path, value, readOnly, focus, editorRef, onChange, onSave }: { path: string; value: string; readOnly?: boolean; focus?: number; editorRef?: (h: unknown) => void; onChange(v: string): void; onSave(): void }) => {
    const el = useRef<HTMLInputElement>(null)
    useEffect(() => {
      if (focus) el.current?.focus()
    }, [focus])
    useEffect(() => {
      if (!editorRef) return
      editorRef(handles.of(path))
      return () => editorRef(null)
    }, [editorRef, path])
    return (
    <div>
      <pre data-testid={`ed:${path}`}>{JSON.stringify(value)}</pre>
      <input
        ref={el}
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
    )
  }
  return { ...feed, default: Editor }
})
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
    disk({ "a.txt": res(200, { content: "disque", etag: "e5" }) })
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
    disk({ "a.txt": res(200, { content: "disque", etag: "e5" }) })
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
    disk({ "a.txt": res(200, { content: "disque", etag: "e5" }) })
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
    const bar = await screen.findByRole("alert", {}, { timeout: 2500 })
    fireEvent.click(within(bar).getByRole("button", { name: "Enregistrer sous le même nom" }))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2), { timeout: 2500 })
    expect(save.mock.calls[1].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\n", ""])
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull(), { timeout: 2500 })

    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: true, etag: "" }))
    ctrlS(input)
    fireEvent.click(within(await screen.findByRole("alert", {}, { timeout: 2500 })).getByRole("button", { name: "Fermer" }))
    await waitFor(() => expect(screen.queryByRole("tab")).toBeNull(), { timeout: 2500 })
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
    expect(screen.getByRole("button", { name: "Nouveau fichier" }).getAttribute("aria-disabled")).toBe("true")
    expect(screen.getByRole("button", { name: "Nouveau dossier" }).getAttribute("aria-disabled")).toBe("true")
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
  it("Ctrl+P dans l'espace ouvre la liste, Entrée ouvre le fichier", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.txt", size: 1, mtime: 1 }, { path: "src/b.txt", size: 1, mtime: 1 }] }))
    render(<IdeView dir="/p" />)
    await screen.findByText("a.txt", { selector: "[role=treeitem] span" })
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

describe("IdeView : Ctrl+P écouté sur le document, focus rendu à l'espace", () => {
  // fireEvent renvoie false quand le comportement par défaut (impression) a été empêché.
  const ctrlP = (el: Element = document.body) => fireEvent.keyDown(el, { key: "p", ctrlKey: true })
  const root = () => screen.getByRole("tree").closest<HTMLElement>('[tabindex="-1"]')!
  beforeEach(() => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.txt", size: 1, mtime: 1 }, { path: "b.txt", size: 1, mtime: 1 }] }))
  })

  it("sans focus dans l'espace, Ctrl+P ouvre ; choisir rend le focus à l'éditeur ; les Ctrl+P suivants restent pris", async () => {
    render(<IdeView dir="/p" />)
    await screen.findByRole("tree")
    expect(ctrlP()).toBe(false)
    const box = await screen.findByRole("combobox")
    fireEvent.change(box, { target: { value: "b.txt" } })
    fireEvent.keyDown(box, { key: "Enter" })
    const ed = await screen.findByLabelText("type:b.txt")
    await waitFor(() => expect(document.activeElement).toBe(ed))
    expect(root().contains(document.activeElement)).toBe(true)
    expect(ctrlP(ed)).toBe(false)
    fireEvent.keyDown(await screen.findByRole("combobox"), { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("combobox")).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(ed))
    expect(ctrlP()).toBe(false)
    expect(await screen.findByRole("combobox")).toBeTruthy()
  })

  it("Échap sans onglet : focus sur la racine de l'espace", async () => {
    render(<IdeView dir="/p" />)
    await screen.findByRole("tree")
    expect(ctrlP()).toBe(false)
    fireEvent.keyDown(await screen.findByRole("combobox"), { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("combobox")).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(root()))
    expect(ctrlP(root())).toBe(false)
  })

  it("Ctrl+P fenêtre ouverte : elle reste ouverte et la saisie reprend le focus", async () => {
    render(<IdeView dir="/p" />)
    await screen.findByRole("tree")
    ctrlP()
    const box = await screen.findByRole("combobox")
    box.blur()
    expect(ctrlP()).toBe(false)
    expect(screen.getByRole("combobox")).toBe(box)
    expect(document.activeElement).toBe(box)
  })

  it("démonté : Ctrl+P n'est plus pris", async () => {
    const { unmount } = render(<IdeView dir="/p" />)
    await screen.findByRole("tree")
    unmount()
    expect(ctrlP()).toBe(true)
  })
})

describe("IdeView : restauration silencieuse", () => {
  const KEY = "gld.ide./p.tabs"
  it("aucun message pour un échec quelconque (403, 500, réseau) ; seuls les onglets lisibles reviennent", async () => {
    file.mockImplementation(async (_d, p) => {
      if (p === "lien.txt") return res(403, { error: "lien" })
      if (p === "panne.txt") return res(500, { error: "panne" })
      if (p === "reseau.txt") throw new Error("réseau")
      return res(200, { content: "x", etag: "e1", size: 1, mtime: 1 })
    })
    localStorage.setItem(KEY, JSON.stringify({ paths: ["lien.txt", "panne.txt", "reseau.txt", "c.txt"], active: "lien.txt" }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(screen.queryAllByRole("tab").map((x) => x.getAttribute("data-path"))).toEqual(["c.txt"]))
    expect(toast.error).not.toHaveBeenCalled()
    expect(toast).not.toHaveBeenCalled()
  })

  it("sans fs_read : aucune requête de restauration", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: [] } })
    localStorage.setItem(KEY, JSON.stringify({ paths: ["a.txt"], active: "a.txt" }))
    render(<IdeView dir="/p" />)
    await act(async () => {})
    expect(file).not.toHaveBeenCalled()
  })
})

describe("IdeView : brouillons des onglets non enregistrés (sessionStorage)", () => {
  const DKEY = "gld.ide./p.drafts"
  const TKEY = "gld.ide./p.tabs"
  const drafts = () => JSON.parse(sessionStorage.getItem(DKEY) ?? "null")
  const dirty = () => screen.queryByLabelText("Modifications non enregistrées")
  const leave = () => {
    const ev = new Event("beforeunload", { cancelable: true })
    window.dispatchEvent(ev)
    return ev.defaultPrevented
  }
  beforeEach(() => sessionStorage.clear())

  it("onglet sale : brouillon écrit en sessionStorage après le délai, jamais en localStorage", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    expect(drafts()).toBeNull()
    await waitFor(() => expect(drafts()).toEqual({ "a.txt": { content: "l1\r\nl2\r\nx", baseEtag: "e1" } }), { timeout: 1500 })
    expect(Object.values(localStorage).join("")).not.toContain("l1")
  })

  it("enregistré : brouillon retiré", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    await waitFor(() => expect(drafts()).not.toBeNull(), { timeout: 1500 })
    ctrlS(input)
    await waitFor(() => expect(drafts()).toBeNull(), { timeout: 1500 })
  })

  it("pagehide et démontage écrivent tout de suite ; au remontage le brouillon revient, onglet sale", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    window.dispatchEvent(new Event("pagehide"))
    expect(drafts()).toEqual({ "a.txt": { content: "l1\r\nl2\r\nx", baseEtag: "e1" } })
    fireEvent.change(input, { target: { value: "y" } })
    cleanup()
    expect(drafts()["a.txt"].content).toBe("l1\r\nl2\r\nxy")
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nxy")))
    expect(dirty()).toBeTruthy()
    ctrlS(screen.getByLabelText("type:a.txt"))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1))
    expect(save.mock.calls[0].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\nxy", "e1"])
  })

  it("le fichier a changé sur le disque depuis : brouillon abandonné avec un toast", async () => {
    localStorage.setItem(TKEY, JSON.stringify({ paths: ["a.txt"], active: "a.txt" }))
    sessionStorage.setItem(DKEY, JSON.stringify({ "a.txt": { content: "brouillon", baseEtag: "e0" } }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\n")))
    expect(dirty()).toBeNull()
    await waitFor(() => expect(toast).toHaveBeenCalled())
    expect(toast.mock.calls[0][0]).toContain("a.txt")
    await waitFor(() => expect(drafts()).toBeNull(), { timeout: 1500 })
  })

  it("stockage illisible : ignoré sans erreur", async () => {
    localStorage.setItem(TKEY, JSON.stringify({ paths: ["a.txt"], active: "a.txt" }))
    for (const junk of ["{", "42", "[1]", JSON.stringify({ "a.txt": { content: 1, baseEtag: "e1" } })]) {
      sessionStorage.setItem(DKEY, junk)
      render(<IdeView dir="/p" />)
      await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\n")))
      expect(dirty()).toBeNull()
      cleanup()
    }
  })

  it("beforeunload n'est empêché que s'il y a un onglet sale", async () => {
    const input = await openA()
    expect(leave()).toBe(false)
    fireEvent.change(input, { target: { value: "x" } })
    expect(leave()).toBe(true)
    ctrlS(input)
    await waitFor(() => expect(dirty()).toBeNull())
    // Le retrait de l'écouteur est un effet passif : il peut suivre de peu le rendu « propre ».
    await waitFor(() => expect(leave()).toBe(false))
  })

  it("onglet en lecture seule : aucun brouillon écrit ni réappliqué", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } })
    localStorage.setItem(TKEY, JSON.stringify({ paths: ["a.txt"], active: "a.txt" }))
    sessionStorage.setItem(DKEY, JSON.stringify({ "a.txt": { content: "brouillon", baseEtag: "e1" } }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\n")))
    fireEvent.change(screen.getByLabelText("type:a.txt"), { target: { value: "x" } })
    await sleep(700)
    expect(drafts()).toBeNull()
    expect(leave()).toBe(false)
  })

  it("au-delà de ~2 Mio cumulés : le brouillon de trop est sauté, la garde de sortie reste", async () => {
    const input = await openA()
    await openFile("b.txt")
    fireEvent.change(input, { target: { value: "A".repeat(1_500_000) } })
    fireEvent.change(screen.getByLabelText("type:b.txt"), { target: { value: "B".repeat(1_000_000) } })
    await waitFor(() => expect(drafts()).not.toBeNull(), { timeout: 1500 })
    expect(Object.keys(drafts())).toEqual(["a.txt"])
    expect(leave()).toBe(true)
  })
})

describe("IdeView : sauvegarde et relectures concurrentes", () => {
  it("Ctrl+S sur onglet propre, frappe pendant la requête, 409 « modifié » : le texte tapé survit, bandeau ; « Recharger » l'écarte exprès", async () => {
    const pending = deferred<FsRes<{ etag: string; conflict: boolean; deleted: boolean }>>()
    save.mockReturnValueOnce(pending.promise)
    const input = await openA()
    ctrlS(input)
    fireEvent.change(input, { target: { value: "y" } })
    disk({ "a.txt": res(200, { content: "disque", etag: "e5" }) })
    await act(async () => pending.resolve(res(409, { ok: false, conflict: true, deleted: false, etag: "e5" })))
    const bar = await screen.findByRole("alert")
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\ny"))
    fireEvent.click(within(bar).getByRole("button", { name: "Recharger" }))
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("disque")))
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("409 alors que le disque est identique (simple touch) : etag adopté, enregistrement relancé, aucun conflit", async () => {
    save.mockResolvedValueOnce(res(409, { ok: false, conflict: true, deleted: false, etag: "e7" })).mockResolvedValueOnce(res(200, { ok: true, etag: "e8" }))
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    disk({ "a.txt": res(200, { content: "l1\r\nl2\r\n", etag: "e7" }) })
    ctrlS(input)
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save.mock.calls[1].slice(0, 4)).toEqual(["/p", "a.txt", "l1\r\nl2\r\nx", "e7"])
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("une relecture lancée avant une sauvegarde n'applique jamais la version d'avant", async () => {
    await openA()
    const es = await stream()
    const old = deferred<FsRes<{ content: string; etag: string }>>()
    const base = file.getMockImplementation()!
    let n = 0
    file.mockImplementation(async (d, p) => (p === "a.txt" && ++n === 1 ? old.promise : base(d, p)))
    es.emit({ path: "a.txt", type: "write" })
    await waitFor(() => expect(n).toBe(1))
    const input = screen.getByLabelText("type:a.txt")
    fireEvent.change(input, { target: { value: "x" } })
    ctrlS(input)
    await waitFor(() => expect(screen.queryByLabelText("Modifications non enregistrées")).toBeNull())
    await act(async () => old.resolve(res(200, { content: "vieux", etag: "e8" })))
    await sleep(100)
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\nx"))
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("relecture en 413 ou 500 : signalé « modifié » au lieu de rester périmé ; panne réseau : ignorée", async () => {
    await openA()
    const es = await stream()
    disk({ "a.txt": { status: 0, ok: false, body: {} } })
    es.emit({ path: "a.txt", type: "write" })
    await sleep(400)
    expect(screen.queryByRole("alert")).toBeNull()
    for (const status of [413, 500]) {
      disk({ "a.txt": res(status, { error: "x" }) })
      es.emit({ path: "a.txt", type: "write" })
      expect(await screen.findByText(/modifié sur le disque/)).toBeTruthy()
    }
    expect(ed("a.txt")).toBe(JSON.stringify("l1\r\nl2\r\n"))
  })

  it("onglet en lecture seule en conflit : ni « Écraser » ni « Enregistrer sous le même nom »", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } })
    await openA()
    const es = await stream()
    disk({ "a.txt": res(413, { error: "x" }) })
    es.emit({ path: "a.txt", type: "write" })
    const bar = await screen.findByRole("alert")
    expect(within(bar).queryByRole("button", { name: "Écraser" })).toBeNull()
    expect(within(bar).getByRole("button", { name: "Recharger" })).toBeTruthy()
    disk({ "a.txt": res(404, { error: "x" }) })
    es.emit({ path: "a.txt", type: "remove" })
    expect(await screen.findByText(/supprimé du disque/)).toBeTruthy()
    expect(within(screen.getByRole("alert")).queryByRole("button", { name: "Enregistrer sous le même nom" })).toBeNull()
    expect(within(screen.getByRole("alert")).getByRole("button", { name: "Fermer" })).toBeTruthy()
  })
})

describe("IdeView : flux, droits, TypeScript", () => {
  it("page refusée : aucun flux, aucun sondage d'index, Ctrl+P laissé au navigateur", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: [] } })
    render(<IdeView dir="/p" />)
    await sleep(400)
    expect(FakeES.all).toHaveLength(0)
    expect(index).not.toHaveBeenCalled()
    expect(api.fsTree).not.toHaveBeenCalled()
    expect(fireEvent.keyDown(document.body, { key: "p", ctrlKey: true })).toBe(true)
  })

  it("réouverture du flux après un changement de dossiers dépliés : resync implicite", async () => {
    vi.mocked(api.fsTree).mockImplementation(async (_d, p) => ({ entries: p === "" ? [e("src", "dir"), e("a.txt", "file")] : [e("b.txt", "file")], truncated: false }))
    await openA()
    const first = await stream()
    first.open()
    fireEvent.click(await screen.findByLabelText("Déplier src"))
    await waitFor(() => expect(first.closed).toBe(true))
    const second = await stream()
    disk({ "a.txt": res(200, { content: "neuf", etag: "e9" }) })
    second.open()
    await waitFor(() => expect(ed("a.txt")).toBe(JSON.stringify("neuf")))
  })

  it("alimentation TypeScript : une taille qui change à mtime égal relit le fichier", async () => {
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 3, mtime: 1 }] }))
    vi.spyOn(api, "fsFiles").mockImplementation(async (_d, ps) => res(200, { files: ps.map((path) => ({ path, content: "v" })) }))
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(feed.setTsLib).toHaveBeenCalledTimes(1))
    const es = await stream()
    index.mockResolvedValue(res(200, { state: "ready", files: [{ path: "a.ts", size: 9, mtime: 1 }] }))
    es.emit({ path: "a.ts", type: "write" })
    await waitFor(() => expect(feed.setTsLib).toHaveBeenCalledTimes(2), { timeout: 2500 })
  })
})

describe("IdeView : barres d'outils et commandes", () => {
  beforeEach(() => handles.all.clear())
  const tool = (name: string) => within(screen.getByRole("toolbar", { name: "Actions de l'éditeur" })).getByRole("button", { name })

  it("la poignée suit l'onglet actif : la barre agit sur l'éditeur de l'onglet affiché", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    await openFile("b.txt")
    await waitFor(() => expect(tool("Annuler").getAttribute("aria-disabled")).toBeNull())
    fireEvent.click(tool("Annuler"))
    expect(handles.of("b.txt").undo).toHaveBeenCalledTimes(1)
    expect(handles.of("a.txt").undo).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("tab", { name: /a\.txt/ }))
    fireEvent.click(tool("Aller à la ligne"))
    expect(handles.of("a.txt").gotoLine).toHaveBeenCalledTimes(1)
    expect(handles.of("b.txt").gotoLine).not.toHaveBeenCalled()
  })

  it("« Enregistrer » de la barre passe par le flux d'enregistrement existant", async () => {
    const input = await openA()
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.click(tool("Enregistrer"))
    await waitFor(() => expect(save).toHaveBeenCalledWith("/p", "a.txt", "l1\r\nl2\r\nx", "e1", undefined))
  })

  it("« Tout enregistrer » enregistre chaque onglet sale en série", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    fireEvent.change(screen.getByLabelText("type:a.txt"), { target: { value: "x" } })
    await openFile("b.txt")
    fireEvent.change(screen.getByLabelText("type:b.txt"), { target: { value: "y" } })
    fireEvent.click(tool("Tout enregistrer"))
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save.mock.calls.map((c) => c[1])).toEqual(["a.txt", "b.txt"])
  })

  it("« Fermer tous les onglets » demande confirmation si un onglet est sale", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    await openFile("b.txt")
    fireEvent.change(screen.getByLabelText("type:b.txt"), { target: { value: "y" } })
    fireEvent.click(tool("Fermer tous les onglets"))
    expect(await screen.findByText("Fermer tous les onglets ?")).toBeTruthy()
    expect(screen.getAllByRole("tab", { hidden: true })).toHaveLength(2)
    fireEvent.click(screen.getByRole("button", { name: "Fermer sans enregistrer" }))
    await waitFor(() => expect(screen.queryByRole("tab")).toBeNull())
  })

  it("« Fermer tous les onglets » : onglet sale en arrière-plan, actif propre : confirmation quand même", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    fireEvent.change(screen.getByLabelText("type:a.txt"), { target: { value: "x" } })
    await openFile("b.txt")
    expect(screen.getByRole("tab", { name: /b\.txt/ }).getAttribute("aria-selected")).toBe("true")
    fireEvent.click(tool("Fermer tous les onglets"))
    expect(await screen.findByText("Fermer tous les onglets ?")).toBeTruthy()
    expect(screen.getAllByRole("tab", { hidden: true })).toHaveLength(2)
  })

  it("« Fermer tous les onglets » sans onglet sale : aucune confirmation", async () => {
    render(<IdeView dir="/p" />)
    await openFile("a.txt")
    fireEvent.click(tool("Fermer tous les onglets"))
    expect(screen.queryByRole("tab")).toBeNull()
    expect(screen.queryByText("Fermer tous les onglets ?")).toBeNull()
  })

  it("nouveau fichier de la barre de l'arbre : création à la racine par le flux existant", async () => {
    const fsNew = vi.spyOn(api, "fsNew").mockResolvedValue(res(200, { ok: true }))
    render(<IdeView dir="/p" />)
    await screen.findByText("a.txt", { selector: "[role=treeitem] span" })
    fireEvent.click(within(screen.getByRole("toolbar", { name: "Actions de l'arborescence" })).getByRole("button", { name: "Nouveau dossier" }))
    fireEvent.change(await screen.findByPlaceholderText("Nom"), { target: { value: "lib" } })
    fireEvent.click(screen.getByRole("button", { name: "Valider" }))
    await waitFor(() => expect(fsNew).toHaveBeenCalledWith("/p", "lib", "dir"))
  })

  it("arborescence masquée par la préférence : ni arbre ni barre de l'arbre", async () => {
    localStorage.setItem("gld.ide.view.showTree", "0")
    render(<IdeView dir="/p" />)
    await screen.findByRole("toolbar", { name: "Actions de l'éditeur" })
    expect(screen.queryByRole("tree")).toBeNull()
    expect(screen.queryByRole("toolbar", { name: "Actions de l'arborescence" })).toBeNull()
    fireEvent.click(tool("Afficher l'arborescence"))
    expect(await screen.findByRole("tree")).toBeTruthy()
  })
})

describe("IdeView : commandes du terminal", () => {
  beforeEach(() => {
    setAuth({ state: "open" } as never)
    vi.spyOn(api, "termInfo").mockResolvedValue({ enabled: true })
    vi.spyOn(api, "data").mockResolvedValue({ live: ["s1"], groups: [{ project: "p", missing: false, sessions: [{ id: "s1", cwd: "/p", mtime: 1 }] }] } as never)
    localStorage.setItem("gld.ide.view.showTerminalChosen", "1")
    localStorage.setItem("gld.ide.view.showTerminal", "1")
  })
  const tool = (name: string) => within(screen.getByRole("toolbar", { name: "Actions de l'éditeur" })).getByRole("button", { name })

  it("jamais ouverte : rien ne s'attache au chargement ; la première bascule l'ouvre et le choix est mémorisé", async () => {
    localStorage.removeItem("gld.ide.view.showTerminalChosen")
    localStorage.removeItem("gld.ide.view.showTerminal")
    render(<IdeView dir="/p" />)
    await screen.findByText("1 session en direct — ouvrir le terminal")
    expect(screen.queryByTestId("term")).toBeNull()
    await waitFor(() => expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-disabled")).toBeNull())
    expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(tool("Ouvrir ou replier le terminal"))
    expect((await screen.findByTestId("term")).dataset.take).toBe("false")
    expect(localStorage.getItem("gld.ide.view.showTerminalChosen")).toBe("1")
    expect(localStorage.getItem("gld.ide.view.showTerminal")).toBe("1")
  })

  it("aucune session vivante, aucun choix : le premier clic sur la bascule ouvre visiblement la zone", async () => {
    localStorage.removeItem("gld.ide.view.showTerminalChosen")
    localStorage.removeItem("gld.ide.view.showTerminal")
    vi.spyOn(api, "data").mockResolvedValue({ live: [], groups: [{ project: "p", missing: false, sessions: [{ id: "s1", cwd: "/p", mtime: 1 }] }] } as never)
    render(<IdeView dir="/p" />)
    await screen.findByText(/lancez `claude` dans ce dossier/)
    await waitFor(() => expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-disabled")).toBeNull())
    expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-pressed")).toBe("false")
    fireEvent.click(tool("Ouvrir ou replier le terminal"))
    expect(await screen.findByText(/le terminal se branche sur une session Claude/)).toBeTruthy()
    expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-pressed")).toBe("true")
  })

  it("Affichage › zone terminal désactivée sans exec, avec la raison", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read", "fs_write"] } })
    render(<IdeView dir="/p" />)
    act(() => document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "a", altKey: true, bubbles: true })))
    const el = within(await screen.findByRole("menu")).getByRole("menuitemcheckbox", { name: "Afficher la zone terminal" })
    expect(el.getAttribute("aria-disabled")).toBe("true")
    expect(document.getElementById(el.getAttribute("aria-describedby")!)?.textContent).toMatch(/exec/)
    fireEvent.click(el)
    expect(localStorage.getItem("gld.ide.view.showTerminal")).toBe("1")
  })

  it("terminal.toggle replie puis rouvre la zone ; view.toggleTerminal par le bouton de la zone", async () => {
    render(<IdeView dir="/p" />)
    await screen.findByTestId("term")
    await waitFor(() => expect(tool("Ouvrir ou replier le terminal").getAttribute("aria-disabled")).toBeNull())
    fireEvent.click(tool("Ouvrir ou replier le terminal"))
    await waitFor(() => expect(screen.queryByTestId("term")).toBeNull())
    fireEvent.click(tool("Ouvrir ou replier le terminal"))
    expect(await screen.findByTestId("term")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Masquer le panneau du bas" }))
    await waitFor(() => expect(screen.queryByTestId("term")).toBeNull())
    fireEvent.click(screen.getByRole("button", { name: "Afficher le panneau du bas" }))
    expect(await screen.findByTestId("term")).toBeTruthy()
  })

  it("terminal.focusSession ouvre la zone repliée et donne le focus, sans prendre la main", async () => {
    localStorage.setItem("gld.ide.view.showTerminalChosen", "1")
    localStorage.setItem("gld.ide.view.showTerminal", "0")
    render(<IdeView dir="/p" />)
    await waitFor(() => expect(tool("Aller au terminal de session").getAttribute("aria-disabled")).toBeNull())
    expect(screen.queryByTestId("term")).toBeNull()
    fireEvent.click(tool("Aller au terminal de session"))
    const term = await screen.findByTestId("term")
    expect(term.dataset.take).toBe("false")
    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText("Terminal de session")))
  })

  it("sans exec : commandes du terminal désactivées avec la raison", async () => {
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read", "fs_write"] } })
    render(<IdeView dir="/p" />)
    const b = await waitFor(() => tool("Aller au terminal de session"))
    expect(b.getAttribute("aria-disabled")).toBe("true")
    expect(document.getElementById(b.getAttribute("aria-describedby")!)?.textContent).toMatch(/exec/)
    expect(api.termInfo).not.toHaveBeenCalled()
  })
})

describe("IdeView : menu et barre d'état", () => {
  const status = (line: number) => ({ line, col: 1, selected: 0, eol: "CRLF" as const, language: "plaintext", indent: "spaces:4", readOnly: false })
  const bar = () => screen.getByRole("group", { name: "Barre d'état" })
  afterEach(() => {
    for (const p of ["a.txt", "b.txt"]) handles.of(p).subscribe.mockImplementation(() => () => {})
  })

  it("menu au-dessus, barre d'état qui suit l'onglet actif et se vide sans onglet", async () => {
    const offA = vi.fn()
    handles.of("a.txt").subscribe.mockImplementation((l: (s: unknown) => void) => (l(status(1)), offA))
    handles.of("b.txt").subscribe.mockImplementation((l: (s: unknown) => void) => (l(status(2)), () => {}))
    render(<IdeView dir="/p" />)
    expect(screen.getByRole("menubar", { name: "Menu de l'espace de travail" })).toBeTruthy()
    expect(within(bar()).queryByText(/Ligne/)).toBeNull()
    await openFile("a.txt")
    expect(await within(bar()).findByText("Ligne 1, colonne 1")).toBeTruthy()
    expect(within(bar()).getByText("Espaces : 4")).toBeTruthy()
    await openFile("b.txt")
    expect(await within(bar()).findByText("Ligne 2, colonne 1")).toBeTruthy()
    expect(offA).toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("type:b.txt"), { target: { value: "x" } })
    expect(within(bar()).getByText("1 onglet modifié")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Fermer a.txt" }))
    fireEvent.click(screen.getByRole("button", { name: "Fermer b.txt" }))
    fireEvent.click(await screen.findByRole("button", { name: "Fermer sans enregistrer" }))
    await waitFor(() => expect(within(bar()).queryByText(/Ligne/)).toBeNull())
  })

  it("état du flux dans la barre ; la préférence masque la barre", async () => {
    render(<IdeView dir="/p" />)
    const es = await stream()
    es.open()
    expect(within(bar()).getByRole("status").textContent).toContain("Surveillance active")
    act(() => document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "a", altKey: true, bubbles: true })))
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitemcheckbox", { name: "Afficher la barre d'état" }))
    await waitFor(() => expect(screen.queryByRole("group", { name: "Barre d'état" })).toBeNull())
  })
})

describe("IdeView : bouton du terminal de la barre d'état", () => {
  beforeEach(() => {
    setAuth({ state: "open" } as never)
    vi.spyOn(api, "termInfo").mockResolvedValue({ enabled: true })
    vi.spyOn(api, "data").mockResolvedValue({ live: ["s1"], groups: [{ project: "p", missing: false, sessions: [{ id: "s1", cwd: "/p", mtime: 1 }] }] } as never)
    localStorage.setItem("gld.ide.view.showTerminalChosen", "1")
    localStorage.setItem("gld.ide.view.showTerminal", "1")
  })
  it("replie puis rouvre la zone par le même chemin que terminal.toggle", async () => {
    render(<IdeView dir="/p" />)
    await screen.findByTestId("term")
    const b = () => within(screen.getByRole("group", { name: "Barre d'état" })).getByRole("button", { name: "Ouvrir ou replier le terminal" })
    await waitFor(() => expect(b().getAttribute("aria-disabled")).toBeNull())
    fireEvent.click(b())
    await waitFor(() => expect(screen.queryByTestId("term")).toBeNull())
    expect(b().getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(b())
    expect(await screen.findByTestId("term")).toBeTruthy()
  })
})
