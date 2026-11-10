import { i18n } from "@/i18n"

// Modèle et effort des prompts : alias de claude --model (toujours la dernière version), versions précises
// (identifiants complets acceptés par --model) et niveaux de --effort. "" : la valeur par défaut de Claude Code.
export const models = () =>
  [
    ["", i18n.t("conversation.models.default")],
    ["fable", "Fable"],
    ["opus", "Opus"],
    ["sonnet", "Sonnet"],
    ["haiku", "Haiku"],
    ["claude-opus-4-5", "Opus 4.5"],
    ["claude-opus-4-1", "Opus 4.1"],
    ["claude-opus-4-0", "Opus 4"],
    ["claude-sonnet-4-5", "Sonnet 4.5"],
    ["claude-sonnet-4-0", "Sonnet 4"],
    ["claude-haiku-4-5", "Haiku 4.5"],
  ] as const

// Entrée de models() correspondant au nom de modèle d'une session (ex. « claude-opus-4-1-20250805 ») :
// la plus longue clé gagne, pour que « opus » ne masque pas « claude-opus-4-1 ».
export const modelKeyOf = (modelName?: string) => {
  const lowerName = modelName?.toLowerCase() ?? ""
  const matchingKeys = models()
    .map(([modelKey]) => modelKey)
    .filter((modelKey) => modelKey && lowerName.includes(modelKey))
  return matchingKeys.sort((firstKey, secondKey) => secondKey.length - firstKey.length)[0] ?? ""
}

export const efforts = () =>
  [
    ["", i18n.t("conversation.efforts.default")],
    ["low", i18n.t("conversation.efforts.low")],
    ["medium", i18n.t("conversation.efforts.medium")],
    ["high", i18n.t("conversation.efforts.high")],
    ["xhigh", i18n.t("conversation.efforts.xhigh")],
    ["max", i18n.t("conversation.efforts.max")],
  ] as const

export type Choice = { model: string; effort: string }

// Options de claude complétées du modèle et de l'effort choisis (validées côté serveur comme les autres).
export const withChoice = (args: string, choice: Choice) => {
  const modelOption = choice.model && `--model=${choice.model}`
  const effortOption = choice.effort && `--effort=${choice.effort}`
  return [args, modelOption, effortOption].filter(Boolean).join(" ")
}
