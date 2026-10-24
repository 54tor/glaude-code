import { describe, expect, it } from "vitest"
import { isDirty, reduce, type IdeState, type Tab } from "./ideState"

const tab = (path: string, content = "x", over: Partial<Tab> = {}): Tab => ({ path, content, saved: content, etag: "e1", ...over })
const two: IdeState = { tabs: [tab("a"), tab("b"), tab("c")], active: "b" }

describe("reduce", () => {
  it("open d'un fichier déjà ouvert le focalise sans doublon", () => {
    const s = reduce(two, { t: "open", tab: tab("a", "autre") })
    expect(s.tabs.map((t) => t.path)).toEqual(["a", "b", "c"])
    expect(s.tabs[0].content).toBe("x")
    expect(s.active).toBe("a")
    const n = reduce(two, { t: "open", tab: tab("d") })
    expect(n.tabs.map((t) => t.path)).toEqual(["a", "b", "c", "d"])
    expect(n.active).toBe("d")
  })

  it("edit rend l'onglet sale, saved le rend propre et met à jour l'etag", () => {
    const e = reduce(two, { t: "edit", path: "a", content: "y" })
    expect(isDirty(e.tabs[0])).toBe(true)
    const s = reduce(e, { t: "saved", path: "a", etag: "e2" })
    expect(isDirty(s.tabs[0])).toBe(false)
    expect(s.tabs[0].etag).toBe("e2")
  })

  it("edit n'a aucun effet sur un onglet en lecture seule", () => {
    const ro: IdeState = { tabs: [tab("a", "x", { readOnly: true })], active: "a" }
    expect(reduce(ro, { t: "edit", path: "a", content: "normalisé" })).toEqual(ro)
  })

  it("reloaded avec fins de ligne mixtes passe l'onglet en lecture seule", () => {
    const s = reduce(two, { t: "reloaded", path: "a", content: "a\r\nb\n", etag: "e9", notice: "mixedEol" })
    expect(s.tabs[0]).toMatchObject({ content: "a\r\nb\n", readOnly: true, notice: "mixedEol" })
  })

  it("saved avec le contenu envoyé garde sale une saisie faite pendant l'enregistrement", () => {
    let s = reduce(two, { t: "edit", path: "a", content: "y" })
    s = reduce(s, { t: "edit", path: "a", content: "yz" })
    s = reduce(s, { t: "saved", path: "a", etag: "e2", content: "y" })
    expect(s.tabs[0]).toMatchObject({ content: "yz", saved: "y", etag: "e2" })
    expect(isDirty(s.tabs[0])).toBe(true)
  })

  it("disk sur un onglet sale pose conflict et ne touche pas content", () => {
    const e = reduce(two, { t: "edit", path: "a", content: "mien" })
    const s = reduce(e, { t: "disk", path: "a", kind: "changed" })
    expect(s.tabs[0]).toMatchObject({ content: "mien", saved: "x", etag: "e1", conflict: "changed" })
    expect(reduce(e, { t: "disk", path: "a", kind: "deleted" }).tabs[0]).toMatchObject({ content: "mien", conflict: "deleted" })
  })

  it("disk « changed » sur un onglet propre n'est pas un conflit (le composant recharge)", () => {
    expect(reduce(two, { t: "disk", path: "a", kind: "changed" })).toEqual(two)
  })

  it("disk « deleted » sur un onglet propre le signale : il n'y a rien à recharger", () => {
    expect(reduce(two, { t: "disk", path: "a", kind: "deleted" }).tabs[0].conflict).toBe("deleted")
  })

  it("reloaded demandé (force) remplace contenu et etag et lève le conflit", () => {
    let s = reduce(two, { t: "edit", path: "a", content: "mien" })
    s = reduce(s, { t: "disk", path: "a", kind: "changed" })
    s = reduce(s, { t: "reloaded", path: "a", content: "disque", etag: "e9", force: true })
    expect(s.tabs[0]).toEqual({ path: "a", content: "disque", saved: "disque", etag: "e9" })
  })

  it("reloaded automatique sur un onglet devenu sale : contenu gardé, conflit", () => {
    const s = reduce(reduce(two, { t: "edit", path: "a", content: "mien" }), { t: "reloaded", path: "a", content: "disque", etag: "e9" })
    expect(s.tabs[0]).toMatchObject({ content: "mien", conflict: "changed" })
  })

  it("disk « changed » always : signale aussi un onglet propre", () => {
    expect(reduce(two, { t: "disk", path: "a", kind: "changed" }).tabs[0].conflict).toBeUndefined()
    expect(reduce(two, { t: "disk", path: "a", kind: "changed", always: true }).tabs[0].conflict).toBe("changed")
  })

  it("saved lève le conflit", () => {
    let s = reduce(two, { t: "edit", path: "a", content: "mien" })
    s = reduce(s, { t: "disk", path: "a", kind: "changed" })
    expect(reduce(s, { t: "saved", path: "a", etag: "e3" }).tabs[0].conflict).toBeUndefined()
  })

  it("close de l'onglet actif focalise le voisin", () => {
    expect(reduce(two, { t: "close", path: "b" })).toEqual({ tabs: [tab("a"), tab("c")], active: "c" })
    const last = { ...two, active: "c" }
    expect(reduce(last, { t: "close", path: "c" }).active).toBe("b")
    expect(reduce(two, { t: "close", path: "a" }).active).toBe("b")
    expect(reduce({ tabs: [tab("a")], active: "a" }, { t: "close", path: "a" })).toEqual({ tabs: [], active: "" })
  })

  it("focus change l'onglet actif", () => {
    expect(reduce(two, { t: "focus", path: "c" }).active).toBe("c")
  })

  it("renamed change le chemin d'un onglet et des fichiers d'un dossier renommé", () => {
    const s = reduce({ tabs: [tab("src/a.ts"), tab("src/b/c.ts"), tab("srcx")], active: "src/b/c.ts" }, { t: "renamed", from: "src", to: "lib" })
    expect(s.tabs.map((t) => t.path)).toEqual(["lib/a.ts", "lib/b/c.ts", "srcx"])
    expect(s.active).toBe("lib/b/c.ts")
    expect(reduce(two, { t: "renamed", from: "a", to: "z" }).tabs[0].path).toBe("z")
  })
})
