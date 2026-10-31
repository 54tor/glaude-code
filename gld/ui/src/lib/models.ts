import { i18n } from "@/i18n"

// Modèle et effort des prompts : alias de claude --model (toujours la dernière version) et niveaux de --effort.
// "" : la valeur par défaut de Claude Code.
export const models = () =>
  [
    ["", i18n.t("conversation.models.default")],
    ["fable", "Fable"],
    ["opus", "Opus"],
    ["sonnet", "Sonnet"],
    ["haiku", "Haiku"],
  ] as const

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
