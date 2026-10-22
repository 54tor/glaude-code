import fc from "fast-check"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { Data, Group, Session } from "./api"
import { argAt, body } from "./commands"
import { describeKey } from "./configKeys"
import { ago, bucket, compact, size, tilde, tokTotal } from "./format"
import { withChoice } from "./models"
import { join, segs } from "./paths"
import { AUTO, buildView, cut, plural } from "./sessionsView"
import { buildTree, find, under } from "./tree"
import {
  cmpFiles,
  home,
  movable,
  newKey,
  split,
  toMemory,
  toRules,
} from "@/components/memoryLib"

const sess = (id: string, title: string, mtime = 0, kb = 1): Session => ({
  id,
  dir: "-d",
  title,
  named: false,
  cwd: "",
  mtime,
  kb,
  tok: { in: 0, out: 0, cr: 0, cw: 0 },
})
const group = (project: string, ...sessions: Session[]): Group => ({
  project,
  missing: false,
  sessions,
})

describe("paths", () => {
  it("segs : sous le home, ailleurs, Windows (casse ignorée), inconnu", () => {
    expect(segs("/home/u", "/home/u")).toEqual(["~"])
    expect(segs("/home/u/a/b", "/home/u")).toEqual(["~", "a", "b"])
    expect(segs("/home/uu/a", "/home/u")).toEqual(["/", "home", "uu", "a"])
    expect(segs("/srv//x/", "/home/u")).toEqual(["/", "srv", "x"])
    expect(segs("c:\\Users\\U\\p", "C:\\Users\\u")).toEqual(["~", "p"])
    expect(segs("D:\\data", "C:\\Users\\u")).toEqual(["D:", "data"])
    expect(segs("relatif", "/home/u")).toEqual(["?", "relatif"])
  })
  it("segs ne renvoie jamais de segment vide, et tout chemin sous le home commence par ~", () => {
    const name = fc.stringMatching(/^[a-zA-Z0-9 ._-]{1,8}$/)
    fc.assert(
      fc.property(fc.array(name, { maxLength: 5 }), (parts) => {
        const s = segs("/home/u/" + parts.join("/"), "/home/u")
        expect(s[0]).toBe("~")
        expect(s.every((x) => x !== "")).toBe(true)
      }),
      { seed: 1 }
    )
  })
  it("join", () => {
    expect(join("", "C:\\", "\\")).toBe("C:\\")
    expect(join("C:\\", "x", "\\")).toBe("C:\\x")
    expect(join("/a", "b", "/")).toBe("/a/b")
  })
})

describe("format", () => {
  const now = new Date(2026, 9, 22, 15, 0, 0)
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(now)
  })
  afterEach(() => vi.useRealTimers())
  const t = now.getTime() / 1000

  it("ago", () => {
    expect(ago(t - 30)).toBe("à l'instant")
    expect(ago(t - 120)).toBe("il y a 2 minutes")
    expect(ago(t - 7200)).toBe("il y a 2 heures")
    expect(ago(t - 86400)).toBe("hier")
  })
  it("ago : le nombre affiché n'atteint jamais la taille de l'unité suivante", () => {
    expect(ago(t - 3570)).toBe("il y a 1 heure")
    expect(ago(t - 86399)).toBe("hier")
    fc.assert(
      fc.property(fc.integer({ min: 60, max: 400 * 86400 }), (s) => {
        const m = /il y a (\d+) (minute|heure|jour)/.exec(ago(t - s))
        if (m)
          expect(Number(m[1])).toBeLessThan(
            { minute: 60, heure: 24, jour: 7 }[m[2]]!
          )
      }),
      { seed: 5, numRuns: 3000 }
    )
  })
  it("bucket : bornes des tranches", () => {
    const midnight = new Date(2026, 9, 22).getTime() / 1000
    expect(bucket(midnight)).toBe("Aujourd'hui")
    expect(bucket(midnight - 1)).toBe("Hier")
    expect(bucket(midnight - 86400)).toBe("Hier")
    expect(bucket(midnight - 86401)).toBe("Cette semaine")
    expect(bucket(midnight - 6 * 86400)).toBe("Cette semaine")
    expect(bucket(midnight - 29 * 86400)).toBe("Ce mois-ci")
    expect(bucket(midnight - 29 * 86400 - 1)).toBe("Plus ancien")
  })
  it("size, compact, tokTotal", () => {
    expect(size(1023)).toBe("1023 Ko")
    expect(size(1536)).toBe("1,5 Mo")
    expect(compact(999)).toBe("999")
    expect(compact(1500)).toBe("1,5 k")
    expect(compact(2_500_000)).toBe("2,5 M")
    expect(compact(999_949)).toBe("999,9 k")
    expect(compact(999_950)).toBe("1 M")
    expect(tokTotal({ in: 1, out: 2, cw: 3 })).toBe(6)
  })
  it("tilde", () => {
    expect(tilde("/home/u/x", "/home/u")).toBe("~/x")
    expect(tilde("/etc", "/home/u")).toBe("/etc")
  })
})

describe("tree", () => {
  it("regroupe sous ~, écrase les chaînes sans projet, retrouve et liste les sessions", () => {
    const a = group("/home/u/Projets/a", sess("1", "un"))
    const b = group("/home/u/Projets/a/sous/b", sess("2", "deux"))
    const c = group("/srv/c", sess("3", "trois"))
    const nodes = buildTree(
      [a, b, c].map((g) => ({ g, ss: g.sessions })),
      "/home/u"
    )
    // la racine « / » d'une chaîne écrasée n'est pas répétée : « srv/c », comme le fait buildTree
    expect(nodes.map((n) => n.name)).toEqual(["~/Projets/a", "srv/c"])
    const na = find(nodes, "~/Projets/a")!
    expect(na.g).toBe(a)
    expect([...na.kids.keys()]).toEqual(["sous"])
    expect(find(nodes, "~/Projets/a/sous/b")!.g).toBe(b)
    expect(under(na).map((x) => x.s.id)).toEqual(["1", "2"])
    expect(find(nodes, "absent")).toBeNull()
  })
})

describe("sessionsView", () => {
  const data = (groups: Group[]) => ({ groups }) as Data
  it("masque les revues automatiques, filtre par texte, étiquette et résultat de recherche, trie par date", () => {
    const g = group(
      "/p/Caddy",
      sess("1", "Déploie", 10, 2),
      sess("2", "Autre", 30),
      sess("3", AUTO[0] + " x", 20, 5),
      sess("4", "Rien", 5)
    )
    const v = buildView(data([g]), "", {}, null)
    expect(v.rows.map((r) => r.s.id)).toEqual(["2", "1", "4"])
    expect(v.autoItems).toEqual([{ dir: "-d", id: "3" }])
    expect(v.akb).toBe(5)
    expect(v.total).toBe(3)
    expect(
      buildView(data([g]), "déploie", {}, null).rows.map((r) => r.s.id)
    ).toEqual(["1"])
    expect(buildView(data([g]), "caddy", {}, null).rows).toHaveLength(3)
    expect(
      buildView(data([g]), "#PR", { "4": ["prod"] }, null).rows.map(
        (r) => r.s.id
      )
    ).toEqual(["4"])
    expect(
      buildView(
        data([g]),
        "introuvable",
        {},
        new Map([["2", {} as never]])
      ).rows.map((r) => r.s.id)
    ).toEqual(["2"])
    expect(buildView(null, "", {}, null).rows).toEqual([])
  })
  it("plural, cut", () => {
    expect(plural(1)).toBe("1 session")
    expect(plural(2, "fichier")).toBe("2 fichiers")
    expect(cut("abcdef", 3)).toBe("abc…")
    expect(cut("abc", 3)).toBe("abc")
  })
})

describe("memoryLib", () => {
  const f = (name: string, kb: number, mtime: number) => ({
    key: "m/-p/" + name,
    name,
    kb,
    mtime,
    path: "",
    tokens: 0,
  })
  it("tri par nom (numérique), taille et date, nom en départage", () => {
    const fs = [f("b10", 1, 3), f("b2", 1, 1), f("a", 2, 2)]
    expect([...fs].sort(cmpFiles("name", "asc")).map((x) => x.name)).toEqual([
      "a",
      "b2",
      "b10",
    ])
    expect([...fs].sort(cmpFiles("size", "desc")).map((x) => x.name)).toEqual([
      "a",
      "b10",
      "b2",
    ])
    expect([...fs].sort(cmpFiles("date", "desc")).map((x) => x.name)).toEqual([
      "b10",
      "a",
      "b2",
    ])
  })
  it("split du front matter", () => {
    expect(split("---\nname: x\n---\ncorps")).toEqual(["name: x", "corps"])
    expect(split("sans en-tête")).toEqual(["", "sans en-tête"])
  })
  it("destinations selon la clé", () => {
    expect([
      movable("m/-p/x.md"),
      movable("g/rules/x.md"),
      movable("r/-p/x.md"),
      movable("g/CLAUDE.md"),
    ]).toEqual([true, true, true, false])
    expect([
      home("m/-p/x.md"),
      home("r/-q/x.md"),
      home("g/rules/x.md"),
    ]).toEqual(["-p", "-q", "global"])
    expect([
      toRules("r/-p/x"),
      toRules("m/-p/x"),
      toMemory("r/-p/x"),
      toMemory("g/rules/x"),
    ]).toEqual([true, false, false, true])
  })
  it("newKey : nom simplifié, sans écraser un fichier existant", () => {
    const all = [f("revue-ete.md", 1, 1)].map((x) => ({
      ...x,
      key: "m/-p/revue-ete.md",
    }))
    expect(newKey(all, "-p", "---\nname: Revue été\n---\n")).toBe(
      "m/-p/revue-ete-2.md"
    )
    expect(newKey([], "global", "# Mon Titre !\n")).toBe("g/rules/mon-titre.md")
    expect(newKey([], "r:-q", "!!!")).toBe("r/-q/entree.md")
  })
  it("newKey ne produit que des clés sûres", () => {
    fc.assert(
      fc.property(fc.string(), (t) => {
        expect(newKey([], "-p", "name: " + t)).toMatch(
          /^m\/-p\/[a-z0-9-]+\.md$/
        )
      }),
      { seed: 3 }
    )
  })
})

describe("divers", () => {
  it("withChoice, commandes, clés de réglages", () => {
    expect(withChoice("--x", { model: "opus", effort: "" })).toBe(
      "--x --model=opus"
    )
    expect(withChoice("", { model: "", effort: "max" })).toBe("--effort=max")
    expect(body("---\ndescription: d\n---\n\n Fais $ARGUMENTS \n")).toBe(
      "Fais $ARGUMENTS"
    )
    expect(argAt("a $1 b $ARGUMENTS", 4)).toEqual({ at: 7, len: 10 })
    expect(argAt("rien")).toBeNull()
    expect(describeKey("env.FOO")).toContain("FOO")
    expect(describeKey("enabledPlugins.a@b")).toContain("a@b")
    expect(describeKey("permissions.allow[]")).toContain("autorisée")
    expect(describeKey("worktree.x")).toContain("worktree")
    expect(describeKey("inconnu.x")).toBeUndefined()
  })
})
