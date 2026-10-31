// Édition en texte des règles de permission : une règle par ligne sous [allow], [ask] ou [deny], un chemin par ligne sous
// [additionalDirectories], « # » pour un commentaire.
// parseRaw lit le texte et le vérifie ligne à ligne : une erreur bloque l'enregistrement, un avertissement non.

import { i18n } from "@/i18n"

export const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const
export type Kind = (typeof KINDS)[number]
export type RawPerms = Record<Kind, string[]>
export type Lint = { line: number; level: "error" | "warn"; msg: string }

const TOOLS = new Set(["Bash", "Read", "Edit", "Write", "MultiEdit", "NotebookEdit", "Glob", "Grep", "WebFetch", "WebSearch", "Task", "Agent", "Skill", "TodoWrite", "SlashCommand", "BashOutput", "KillShell"])
const RULE = /^([A-Za-z_*][\w*.-]*)(?:\((.*)\))?$/
const SECTION_HEADER = /^\[(.*)\]$/
const MAX_DIRECTORY_LENGTH = 500
const MAX_RULE_LENGTH = 300
const BASH_PREFIX_WILDCARD = ":*"

export function formatRaw(perms: RawPerms): string {
  const formatSection = (kind: Kind) => {
    const ruleLines = perms[kind].map((rule) => rule + "\n").join("")
    return `[${kind}]\n${ruleLines}`
  }
  return KINDS.map(formatSection).join("\n")
}

const isKnownKind = (name: string): name is Kind => (KINDS as readonly string[]).includes(name)

export function parseRaw(text: string): { perms: RawPerms; lint: Lint[] } {
  const perms: RawPerms = { allow: [], ask: [], deny: [], additionalDirectories: [] }
  const lint: Lint[] = []
  const occurrencesByRule = new Map<string, { kind: Kind; line: number }[]>()
  let currentKind: Kind | null = null
  text.split("\n").forEach((rawLine, lineIndex) => {
    const line = lineIndex + 1
    const content = rawLine.trim()
    const isBlankOrComment = !content || content.startsWith("#")
    if (isBlankOrComment) return
    const error = (msg: string) => lint.push({ line, level: "error", msg })
    const warn = (msg: string) => lint.push({ line, level: "warn", msg })

    const sectionHeader = SECTION_HEADER.exec(content)
    if (sectionHeader) {
      const sectionName = sectionHeader[1]
      currentKind = isKnownKind(sectionName) ? sectionName : null
      if (!currentKind) error(i18n.t("config.lint.unknownSection", { name: sectionName }))
      return
    }
    if (!currentKind) {
      error(i18n.t("config.lint.outside"))
      return
    }
    const kind: Kind = currentKind

    if (kind === "additionalDirectories") {
      if (content.length > MAX_DIRECTORY_LENGTH) {
        error(i18n.t("config.lint.pathTooLong"))
        return
      }
      if (perms[kind].includes(content)) {
        warn(i18n.t("config.lint.dupDir"))
        return
      }
      perms[kind].push(content)
      return
    }

    if (content.length > MAX_RULE_LENGTH) {
      error(i18n.t("config.lint.ruleTooLong"))
      return
    }
    const ruleMatch = RULE.exec(content)
    if (!ruleMatch) {
      error(i18n.t("config.lint.syntax"))
      return
    }
    const [, tool, argument] = ruleMatch
    const isKnownTool = tool.startsWith("mcp__") || tool.includes("*") || TOOLS.has(tool)
    if (!isKnownTool) warn(i18n.t("config.lint.unknownTool", { tool }))
    if (argument === "") warn(i18n.t("config.lint.emptyParens"))
    if (tool === "Bash" && argument !== undefined) {
      const wildcardIndex = argument.indexOf(BASH_PREFIX_WILDCARD)
      const isWildcardMisplaced = wildcardIndex >= 0 && wildcardIndex !== argument.length - BASH_PREFIX_WILDCARD.length
      if (isWildcardMisplaced) {
        error(i18n.t("config.lint.starEnd"))
        return
      }
      if (argument === "*" || argument === BASH_PREFIX_WILDCARD) warn(i18n.t("config.lint.bashStar"))
    }
    if (tool === "Bash" && argument === undefined && kind === "allow") warn(i18n.t("config.lint.bashAlone"))
    if (perms[kind].includes(content)) {
      warn(i18n.t("config.lint.dupRule"))
      return
    }
    perms[kind].push(content)
    occurrencesByRule.set(content, [...(occurrencesByRule.get(content) ?? []), { kind, line }])
  })
  for (const [rule, occurrences] of occurrencesByRule) {
    const kinds = new Set(occurrences.map((occurrence) => occurrence.kind))
    if (kinds.size <= 1) continue
    const lastLine = occurrences[occurrences.length - 1].line
    const kindList = [...kinds].join(i18n.t("config.lint.and"))
    lint.push({ line: lastLine, level: "warn", msg: i18n.t("config.lint.conflict", { rule, kinds: kindList }) })
  }
  return { perms, lint: lint.sort((left, right) => left.line - right.line) }
}
