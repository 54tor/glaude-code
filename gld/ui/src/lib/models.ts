// Modèle et effort des prompts : alias de claude --model (toujours la dernière version) et niveaux de --effort.
// "" : la valeur par défaut de Claude Code.
export const MODELS = [
  ["", "Modèle par défaut"],
  ["fable", "Fable"],
  ["opus", "Opus"],
  ["sonnet", "Sonnet"],
  ["haiku", "Haiku"],
] as const

export const EFFORTS = [
  ["", "Effort par défaut"],
  ["low", "Effort bas"],
  ["medium", "Effort moyen"],
  ["high", "Effort élevé"],
  ["xhigh", "Effort très élevé"],
  ["max", "Effort maximal"],
] as const

export type Choice = { model: string; effort: string }

// Options de claude complétées du modèle et de l'effort choisis (validées côté serveur comme les autres).
export const withChoice = (args: string, c: Choice) =>
  [args, c.model && `--model=${c.model}`, c.effort && `--effort=${c.effort}`].filter(Boolean).join(" ")
