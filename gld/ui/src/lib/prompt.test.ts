/* eslint-disable no-irregular-whitespace -- captures réelles du terminal (espaces insécables) */
import { describe, expect, it } from "vitest"
import { parsePrompt } from "./prompt"

const real = {
  // Captures d'une vraie session Claude Code 2.1.307 (tmux), 2026-10-22.
  permission: `
  ⎿  · Quelle couleur préfères-tu ? → turquoise
● Tu as répondu « turquoise », une option hors liste. Je lance maintenant la commande demandée.
  Supprime le chemin de test /tmp/gldprobe-inexistant
  ⎿  $ rm -rf /tmp/gldprobe-inexistant
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 Bash command
 Supprime le chemin de test /tmp/gldprobe-inexistant
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 rm -rf /tmp/gldprobe-inexistant
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Ask rule Bash(rm *) overrides auto mode for this command.
 /permissions to let auto mode decide
 Do you want to proceed?
 ❯ 1. Yes
   2. No
 Esc to cancel · Tab to amend`,
  multi: `
←  ☐ Couleurs  ✔ Submit  →
Quelles couleurs veux-tu sélectionner ?
❯ 1. [ ] Rouge
         Couleur chaude, forte visibilité.
  2. [ ] Bleu
         Couleur froide, neutre et apaisante.
  3. [ ] Vert
         Couleur associée à la nature et au succès.
  4. [ ] Type something
     Submit
────────────────────────────────────────────────────────────────────────────────────────────
  5. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel`,
  multiMid: `
←  ☒ Couleurs  ✔ Submit  →
Quelles couleurs veux-tu sélectionner ?
  1. [✔] Rouge
         Couleur chaude, forte visibilité.
❯ 2. [✔] Bleu
         Couleur froide, neutre et apaisante.
  3. [ ] Vert
         Couleur associée à la nature et au succès.
  4. [ ] Type something
     Submit
────────────────────────────────────────────────────────────────────────────────────────────
  5. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel`,
  single: ` ☐ Couleur
Quelle couleur préfères-tu ?
❯ 1. Rouge
     Couleur chaude, forte visibilité.
  2. Bleu
     Couleur froide, neutre et apaisante.
  3. Vert
     Couleur associée à la nature et au succès.
  4. Type something.
────────────────────────────────────────────────────────────────────────────────────────────
  5. Chat about this
Enter to select · ↑/↓ to navigate · Esc to cancel`,
  plan: `
  ⎿  /plan to preview
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  ────────────────────────────────────────
   Ready to code?
   Here is Claude's plan:
  ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
   Plan : créer /tmp/gldprobe-x.txt
   1. Créer le fichier vide /tmp/gldprobe-x.txt (par exemple avec touch).
   2. Vérifier son existence avec ls -l /tmp/gldprobe-x.txt.
   3. Rien d'autre : aucun autre fichier touché, aucun commit.
  ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
  ────────────────────────────────────────
   Claude has written up a plan and is ready to execute. Would you like to proceed?
   ❯ 1. Yes, and use auto mode
     2. Yes, manually approve edits
     3. Tell Claude what to change
        shift+tab to approve with this feedback
   ctrl+g to edit in VS Code · ~/.claude/plans/sans-explorer-le-code-groovy-pumpkin.md`,
  edit: `
────────────────────────────────────────
 Edit file
 ../../../../tmp/claude-1000/-home-u-proj/b32f1960-1cff-4384-9aa3-1ef6c92fba6a/scratchpad/probe.txt
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 1  une ligne
 2 -autre ligne
 2 +ligne modifiée
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to make this edit to probe.txt?
 ❯ 1. Yes
   2. Yes, and switch to accept edits (auto-approve file edits and common file commands) for this session; Yes, and always allow access to
      /tmp/claude-1000/-home-u-proj/b32f1960-1cff-4384-9aa3-1ef6c92fba6a/scratchpad for this session (shift+tab)
   3. No
 Esc to cancel · Tab to amend`,
  tabs: `
←  ☐ Couleur  ☐ Saveur  ✔ Submit  →
Quelle couleur préférez-vous ?
❯ 1. Bleu
     Teinte froide et sobre.
  2. Vert
     Teinte naturelle.
  3. Type something.
────────────────────────────────────────────────────────────────────────────────────────────
  4. Chat about this
Enter to select · Tab/Arrow keys to navigate · Esc to cancel`,
  review: `←  ☒ Couleurs  ✔ Submit  →
Review your answers
 ● Quelles couleurs veux-tu sélectionner ?
   → Rouge, Bleu, violet
Ready to submit your answers?
❯ 1. Submit answers
  2. Cancel`,
}

const screens = {
  permission: `
 Bash command

   rm -rf build
   Remove the build folder

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend`,
  boxed: `
╭──────────────────────────────────╮
│ Do you want to make this edit?   │
│ ❯ 1. Yes                         │
│   2. No                          │
╰──────────────────────────────────╯
 Esc to cancel`,
  ask: `
 Côté guru, qui termine le HTTPS ?

 ❯ 1. Guru termine le TLS (Recommandé)
      Certificats gérés sur guru
   2. Un reverse proxy dédié
   3. Type something.
 ────────────────────────────────
   4. Chat about this

 Enter to select · ↑/↓ to navigate · Esc to cancel`,
  review: `
 Review your answers

 Ready to submit your answers?

 ❯ 1. Submit answers
   2. Cancel`,
  select: `
 Select a model

 ❯ Opus
   Sonnet
   Haiku

 Enter to confirm · Esc to exit`,
  busy: `
 ❯ 1. un exemple dans la réponse
   2. autre ligne

 ✻ Réfléchit… (esc to interrupt)`,
  prose: `
Voici une liste :
1. premier
2. second

Voilà.
 ❯ 
 ⏵⏵ auto mode on`,
}

describe("parsePrompt : captures réelles", () => {
  it("permission d'une commande risquée", () => {
    const p = parsePrompt(real.permission)!
    expect(p.question).toBe("Do you want to proceed?")
    expect(p.options.map((o) => o.keys)).toEqual([["1"], ["2"]])
    expect(p.submit).toBeUndefined()
  })
  it("choix multiples : cases, accès au champ libre, validation", () => {
    const p = parsePrompt(real.multi)!
    expect(p.question).toBe("Quelles couleurs veux-tu sélectionner ?")
    expect(p.options.filter((o) => o.checked !== undefined).map((o) => [o.label, o.checked, o.keys[0]])).toEqual([
      ["Rouge", false, "1"],
      ["Bleu", false, "2"],
      ["Vert", false, "3"],
      ["Type something", false, "4"],
    ])
    const free = p.options.find((o) => o.text && o.checked !== undefined)!
    expect(free.reach).toEqual(["Down", "Down", "Down"])
    expect(p.submit).toEqual(["Down", "Down", "Down", "Down", "Enter"])
    expect(p.options.at(-1)).toMatchObject({ chat: true, keys: ["5", "Enter"] })
  })
  it("choix multiples : du texte avant l'en-tête ne remplace pas la question", () => {
    const p = parsePrompt("Récapitulatif de Claude :\n- UI-04 : focus ;\n" + real.multi)!
    expect(p.question).toBe("Quelles couleurs veux-tu sélectionner ?")
    expect(p.context).toBe("Récapitulatif de Claude :\n- UI-04 : focus ;")
  })
  it("capture réelle à onglets : récapitulatif, question, descriptions, Submit", () => {
    const screen = [
      "● Voici les défauts :",
      "  UI-02, barre d'outils du lecteur",
      "  - A : autoriser le retour à la ligne.",
      "",
      "  Limites de l'audit",
      "  - Pas de test au lecteur d'écran.",
      "────────────────────────────────────────",
      "←  ☒ UI-01  ☒ UI-02  ☐ Périmètre  ✔ Submit  →",
      "",
      "Quel périmètre pour le premier lot ?",
      "",
      "❯ 1. [ ] UI-01, UI-02",
      "         Les défauts confirmés.",
      "  2. [ ] UI-04 et UI-05",
      "         Accessibilité.",
      "  3. [ ] Type something",
      "     Submit",
      "────────────────────────────────────────",
      "  4. Chat about this",
      "",
      "Enter to select · Tab/Arrow keys to navigate · Esc to cancel",
    ].join("\n")
    const p = parsePrompt(screen)!
    expect(p.question).toBe("Quel périmètre pour le premier lot ?")
    expect(p.context).toBe("● Voici les défauts :\n  UI-02, barre d'outils du lecteur\n  - A : autoriser le retour à la ligne.\n  Limites de l'audit\n  - Pas de test au lecteur d'écran.")
    expect(p.options.map((o) => o.detail)).toEqual(["Les défauts confirmés.", "Accessibilité.", undefined, undefined])
    expect(p.submit).toEqual(["Down", "Down", "Down", "Enter"])
    expect(p.tabs).toEqual([{ label: "UI-01", done: true }, { label: "UI-02", done: true }, { label: "Périmètre", done: false }])
  })
  it("choix multiples : la description de chaque case est conservée", () => {
    const o = parsePrompt(real.multi)!.options
    expect(o[0].detail).toBe("Couleur chaude, forte visibilité.")
    expect(o[3].detail).toBeUndefined()
  })
  it("choix multiples : curseur au milieu, cases cochées", () => {
    const p = parsePrompt(real.multiMid)!
    expect(p.options.slice(0, 2).map((o) => o.checked)).toEqual([true, true])
    expect(p.submit).toEqual(["Down", "Down", "Down", "Enter"])
    expect(p.options[3].reach).toEqual(["Down", "Down"])
    expect(p.options[0].reach).toEqual(["Up"])
  })
  it("choix unique avec champ libre et discussion", () => {
    const p = parsePrompt(real.single)!
    expect(p.question).toBe("Quelle couleur préfères-tu ?")
    expect(p.options.map((o) => [o.keys, !!o.text, !!o.chat])).toEqual([[["1"], false, false], [["2"], false, false], [["3"], false, false], [["4"], true, false], [["5", "Enter"], true, true]])
    expect(p.submit).toBeUndefined()
  })
  it("relecture des réponses", () => {
    const p = parsePrompt(real.review)!
    expect(p.options.map((o) => o.keys[0])).toEqual(["1", "2"])
  })
})

describe("parsePrompt : autres dialogues réels", () => {
  it("approbation d'un plan : la 3e option est une réponse libre", () => {
    const p = parsePrompt(real.plan)!
    expect(p.question).toMatch(/Would you like to proceed/)
    expect(p.options.map((o) => [o.keys[0], !!o.text])).toEqual([["1", false], ["2", false], ["3", true]])
  })
  it("édition d'un fichier : l'option 2 tient sur deux lignes", () => {
    const p = parsePrompt(real.edit)!
    expect(p.question).toBe("Do you want to make this edit to probe.txt?")
    expect(p.options).toHaveLength(3)
    expect(p.options[1].label).toMatch(/always allow access/)
    expect(p.options[1].detail).toMatch(/scratchpad for this session/)
  })
  it("plusieurs questions : un onglet à la fois, la touche chiffre répond et passe à la suivante", () => {
    const p = parsePrompt(real.tabs)!
    expect(p.question).toBe("Quelle couleur préférez-vous ?")
    expect(p.submit).toBeUndefined()
    expect(p.options.map((o) => o.keys[0])).toEqual(["1", "2", "3", "4"])
  })
})

describe("parsePrompt : faux dialogues", () => {
  it("des cases imitées dans du texte, au-dessus d'une permission, ne donnent pas de « Valider »", () => {
    const fake = `Voici une liste :
1. [ ] a
2. [ ] b
   Submit
${real.permission}`
    const p = parsePrompt(fake)
    expect(p?.submit).toBeUndefined()
  })
  it("des cases sans l'en-tête de la vraie question sont ignorées", () => {
    const noHead = real.multi.split("\n").filter((l) => !l.includes("←")).join("\n")
    expect(parsePrompt(noHead)?.submit).toBeUndefined()
  })
})

describe("parsePrompt", () => {
  it("permission : question et trois options numérotées, la dernière à réponse libre", () => {
    const p = parsePrompt(screens.permission)!
    expect(p.question).toBe("Do you want to proceed?")
    expect(p.options.map((o) => o.keys)).toEqual([["1"], ["2"], ["3"]])
    expect(p.options[2].text).toBe(true)
  })
  it("question d'un onglet : tout le paragraphe, même s'il ne finit pas par un point d'interrogation", () => {
    const screen = [
      "←  ☐ Code mort  ☐ shadcn  ✔ Submit  →",
      "",
      "│ Code mort confirmé par deadcode (aucun appelant en production, vérifié sur linux, windows et darwin, ni appel dynamique ni route) :",
      "│ que faire de recentSessions (open.go) et de memMove (memory.go) ? providerByID est gardée dans tous les cas : elle sert au",
      "│ multi-fournisseur prévu dans todo.local.md.",
      "",
      "❯ 1. Supprimer les deux (Recommandé)",
      "     recentSessions et son test partent.",
      "  2. Supprimer recentSessions seule",
      "  3. Tout garder",
      "  4. Type something.",
      "─────────────────────────────",
      "  5. Chat about this",
      "",
      "Enter to select · Tab/Arrow keys to navigate",
    ].join("\n")
    const p = parsePrompt(screen)!
    expect(p.question).toMatch(/^Code mort confirmé/)
    expect(p.question).toMatch(/todo\.local\.md\.$/)
  })
  it("une question longue repliée sur plusieurs lignes est lue en entier", () => {
    const screen = [
      "│ CODE-A4-1 (High) — À chaque item servi, Torznab fait tourner le jeton de téléchargement (ApiKeySnatchAuthorizer.php:37,58 :",
      "│ rotate). Tout lien déjà émis répond donc 404 dès qu'une autre recherche ressert le même torrent sur la même clé (Sonarr +",
      "│ Prowlarr, grab différé). Le même chemin coûte un SELECT et un flush par item (PERF-A4-1). Quelle correction ?",
      "",
      "❯ 1. Jeton déterministe HMAC (Recommandé)",
      "     Le clair dérive de HMAC(secret, id du jeton).",
      "  2. Plusieurs jetons par triplet",
      "  3. Laisser",
      "",
      "Enter to select · ↑/↓ to navigate · Esc to cancel",
    ].join("\n")
    const p = parsePrompt(screen)!
    expect(p.question).toContain("CODE-A4-1")
    expect(p.question).toContain("rotate). Tout lien")
    expect(p.question).toMatch(/Quelle correction \?$/)
  })
  it("autorisation : la commande et son titre accompagnent la question", () => {
    const sep = "╌".repeat(60)
    const screen = [
      " Bash command · from the general-purpose agent                                  1 of 2",
      " Clean temp file and verify commits",
      sep,
      " │ cd /home/u/cap-tortuga; rm /tmp/x/upload-full.php; git log --format='%h %s' -1 6e17c34",
      sep,
      " Ask rule Bash(rm *) overrides auto mode for this command.",
      " /permissions to let auto mode decide",
      "",
      " Do you want to proceed?",
      " ❯ 1. Yes",
      "   2. No",
      "",
      " Esc to cancel · Tab to amend",
    ].join("\n")
    const p = parsePrompt(screen)!
    expect(p.question).toBe("Do you want to proceed?")
    expect(p.context).toContain("rm /tmp/x/upload-full.php")
    expect(p.context).toContain("Clean temp file and verify commits")
    expect(p.context).toContain("Bash command")
    expect(p.context).toContain("Ask rule Bash(rm *)")
  })
  it("dialogue encadré", () => {
    const p = parsePrompt(screens.boxed)!
    expect(p.options).toHaveLength(2)
    expect(p.options[0].label).toBe("1. Yes")
  })
  it("question à choix : « Type something » et « Chat about this » sont des réponses libres", () => {
    const p = parsePrompt(screens.ask)!
    expect(p.question).toMatch(/HTTPS/)
    expect(p.options.filter((o) => o.text).map((o) => o.keys[0])).toEqual(["3", "4"])
  })
  it("écran de relecture des réponses", () => {
    const p = parsePrompt(screens.review)!
    expect(p.question).toBe("Ready to submit your answers?")
    expect(p.options.map((o) => o.keys[0])).toEqual(["1", "2"])
  })
  it("liste à sélection : flèches puis Entrée", () => {
    const p = parsePrompt(screens.select)!
    expect(p.options.map((o) => o.keys)).toEqual([["Enter"], ["Down", "Enter"], ["Down", "Down", "Enter"]])
  })
  it("rien en pleine réflexion ni sur du texte ordinaire", () => {
    expect(parsePrompt(screens.busy)).toBeNull()
    expect(parsePrompt(screens.prose)).toBeNull()
  })
})
