// Édition en texte des règles de permission : une règle par ligne sous [allow], [ask] ou [deny], un chemin par ligne sous
// [additionalDirectories], « # » pour un commentaire.
// parseRaw lit le texte et le vérifie ligne à ligne : une erreur bloque l'enregistrement, un avertissement non.

export const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const
export type Kind = (typeof KINDS)[number]
export type RawPerms = Record<Kind, string[]>
export type Lint = { line: number; level: "error" | "warn"; msg: string }

const TOOLS = new Set(["Bash", "Read", "Edit", "Write", "MultiEdit", "NotebookEdit", "Glob", "Grep", "WebFetch", "WebSearch", "Task", "Agent", "Skill", "TodoWrite", "SlashCommand", "BashOutput", "KillShell"])
const RULE = /^([A-Za-z_*][\w*.-]*)(?:\((.*)\))?$/

export function formatRaw(p: RawPerms): string {
  return KINDS.map((k) => `[${k}]\n${p[k].map((r) => r + "\n").join("")}`).join("\n")
}

export function parseRaw(text: string): { perms: RawPerms; lint: Lint[] } {
  const perms: RawPerms = { allow: [], ask: [], deny: [], additionalDirectories: [] }
  const lint: Lint[] = []
  const at = new Map<string, { kind: Kind; line: number }[]>()
  let kind: Kind | null = null
  text.split("\n").forEach((raw, i) => {
    const line = i + 1
    const s = raw.trim()
    if (!s || s.startsWith("#")) return
    const head = /^\[(.*)\]$/.exec(s)
    if (head) {
      kind = (KINDS as readonly string[]).includes(head[1]) ? (head[1] as Kind) : null
      if (!kind) lint.push({ line, level: "error", msg: `Section inconnue « ${head[1]} » : allow, ask, deny ou additionalDirectories` })
      return
    }
    if (!kind) return void lint.push({ line, level: "error", msg: "Règle hors d'une section ([allow], [ask], [deny] ou [additionalDirectories])" })
    const err = (msg: string) => lint.push({ line, level: "error", msg })
    const warn = (msg: string) => lint.push({ line, level: "warn", msg })
    if (kind === "additionalDirectories") {
      if (s.length > 500) return err("Chemin de plus de 500 caractères")
      if (perms[kind].includes(s)) return warn("Dossier en double")
      perms[kind].push(s)
      return
    }
    if (s.length > 300) return err("Règle de plus de 300 caractères")
    const m = RULE.exec(s)
    if (!m) return err("Syntaxe attendue : Outil ou Outil(motif)")
    const [, tool, arg] = m
    if (!tool.startsWith("mcp__") && !tool.includes("*") && !TOOLS.has(tool)) warn(`Outil « ${tool} » inconnu : faute de frappe ?`)
    if (arg === "") warn("Parenthèses vides : écris l'outil seul pour tout autoriser")
    if (tool === "Bash" && arg !== undefined) {
      const star = arg.indexOf(":*")
      if (star >= 0 && star !== arg.length - 2) return err("« :* » n'est valable qu'en fin de motif Bash")
      if (arg === "*" || arg === ":*") warn("Bash(*) équivaut à toutes les commandes")
    }
    if (tool === "Bash" && arg === undefined && kind === "allow") warn("« Bash » seul autorise toutes les commandes")
    if (perms[kind].includes(s)) return warn("Règle en double dans cette section")
    perms[kind].push(s)
    at.set(s, [...(at.get(s) ?? []), { kind, line }])
  })
  for (const [rule, where] of at) {
    const kinds = new Set(where.map((w) => w.kind))
    if (kinds.size > 1) lint.push({ line: where[where.length - 1].line, level: "warn", msg: `« ${rule} » est dans ${[...kinds].join(" et ")} : deny l'emporte, puis ask` })
  }
  return { perms, lint: lint.sort((a, b) => a.line - b.line) }
}
