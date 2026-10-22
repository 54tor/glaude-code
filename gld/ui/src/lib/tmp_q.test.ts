import { expect, it } from "vitest"
import { parsePrompt } from "./prompt"

it("capture collée", () => {
  const screen = [
    "Deux décisions vous reviennent pendant que la mutation tourne.",
    "────────────────────────────────────────",
    "←  ☐ Code mort  ☐ shadcn  ✔ Submit  →",
    "",
    "│ Code mort confirmé par deadcode (aucun appelant en production) : que faire de recentSessions (open.go) ? providerByID est gardée dans tous les cas : elle sert au multi-fournisseur prévu dans todo.local.md.",
    "",
    "❯ 1. Supprimer les deux (Recommandé)",
    "     recentSessions et son test partent ; les tests de memMove appellent memTransfer(..., false) directement. Pour : moins de code. Contre : il faudra les réécrire.",
    "  2. Supprimer recentSessions seule",
    "     memMove reste comme raccourci pour les tests.",
    "  3. Tout garder",
    "     Constat documenté dans le rapport.",
    "  4. Type something.",
    "────────────────────────────────────────",
    "  5. Chat about this",
    "",
    "Enter to select · Tab/Arrow keys to navigate",
  ].join("\n")
  const p = parsePrompt(screen)
  console.log(JSON.stringify(p, null, 1))
  expect(p?.question).toMatch(/^Code mort/)
})
