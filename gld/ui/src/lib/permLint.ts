// Édition en texte des règles de permission : une règle par ligne sous [allow], [ask] ou [deny], un chemin par ligne sous
// [additionalDirectories], « # » pour un commentaire.
// parseRaw lit le texte et le vérifie ligne à ligne : une erreur bloque l'enregistrement, un avertissement non.

import { i18n } from "@/i18n"

export const KINDS =["allow", "ask", "deny", "additionalDirectories"] as const
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
      if (!kind) lint.push({ line, level: "error", msg: i18n.t("config.lint.unknownSection", { name: head[1] }) })
      return
    }
    if (!kind) return void lint.push({ line, level: "error", msg: i18n.t("config.lint.outside") })
    const err = (msg: string) => lint.push({ line, level: "error", msg })
    const warn = (msg: string) => lint.push({ line, level: "warn", msg })
    if (kind === "additionalDirectories") {
      if (s.length > 500) return err(i18n.t("config.lint.pathTooLong"))
      if (perms[kind].includes(s)) return warn(i18n.t("config.lint.dupDir"))
      perms[kind].push(s)
      return
    }
    if (s.length > 300) return err(i18n.t("config.lint.ruleTooLong"))
    const m = RULE.exec(s)
    if (!m) return err(i18n.t("config.lint.syntax"))
    const [, tool, arg] = m
    if (!tool.startsWith("mcp__") && !tool.includes("*") && !TOOLS.has(tool)) warn(i18n.t("config.lint.unknownTool", { tool }))
    if (arg === "") warn(i18n.t("config.lint.emptyParens"))
    if (tool === "Bash" && arg !== undefined) {
      const star = arg.indexOf(":*")
      if (star >= 0 && star !== arg.length - 2) return err(i18n.t("config.lint.starEnd"))
      if (arg === "*" || arg === ":*") warn(i18n.t("config.lint.bashStar"))
    }
    if (tool === "Bash" && arg === undefined && kind === "allow") warn(i18n.t("config.lint.bashAlone"))
    if (perms[kind].includes(s)) return warn(i18n.t("config.lint.dupRule"))
    perms[kind].push(s)
    at.set(s, [...(at.get(s) ?? []), { kind, line }])
  })
  for (const [rule, where] of at) {
    const kinds = new Set(where.map((w) => w.kind))
    if (kinds.size > 1) lint.push({ line: where[where.length - 1].line, level: "warn", msg: i18n.t("config.lint.conflict", { rule, kinds: [...kinds].join(i18n.t("config.lint.and")) }) })
  }
  return { perms, lint: lint.sort((a, b) => a.line - b.line) }
}
