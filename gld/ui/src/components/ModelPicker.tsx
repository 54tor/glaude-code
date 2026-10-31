import { cn } from "@/lib/utils"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { efforts, models, type Choice } from "@/lib/models"
import { Choose } from "./Choose"

// Modèle que claude utilisera sans --model : "model" des réglages utilisateur, sinon ANTHROPIC_MODEL.
let userModel: Promise<string> | undefined
const USER_SETTINGS_FILE_ID = "u"
type UserSettings = { model?: string; env?: Record<string, string> }

const defaultModel = () =>
  (userModel ??= api
    .config()
    .then((overview) => {
      const userSettings = overview.files.find((file) => file.id === USER_SETTINGS_FILE_ID)?.data as UserSettings | undefined
      return userSettings?.model || userSettings?.env?.ANTHROPIC_MODEL || ""
    })
    .catch(() => ""))

// Choix du modèle et de l'effort d'un prompt (Réglages, fenêtre de prompt, chat du lecteur).
export function ModelPicker({
  value,
  onChange,
  disabled,
  size,
  className,
}: {
  value: Choice
  onChange: (choice: Choice) => void
  disabled?: boolean
  size?: "sm" | "default"
  className?: string
}) {
  const { t } = useTranslation()
  const [defaultModelName, setDefaultModelName] = useState("")
  useEffect(() => void defaultModel().then(setDefaultModelName), [])
  const defaultModelLabel = () => {
    const matchingModel = models().find(([modelValue]) => modelValue && defaultModelName.toLowerCase().includes(modelValue))
    return matchingModel?.[1] ?? defaultModelName
  }
  const labelOption = ([modelValue, modelLabel]: ReturnType<typeof models>[number]) => {
    const isDefaultOption = !modelValue && defaultModelName
    if (!isDefaultOption) return [modelValue, modelLabel]
    return [modelValue, t("conversation.models.defaultIs", { model: defaultModelLabel() })]
  }
  const opts = models().map(labelOption) as unknown as ReturnType<typeof models>
  return (
    <div className={cn("flex min-w-0 flex-wrap gap-2 max-md:flex-1 max-md:flex-nowrap", className)}>
      <Choose value={value.model} onChange={(model) => onChange({ ...value, model })} options={opts} label={t("conversation.modelPicker.model")} disabled={disabled} size={size} className="max-md:min-w-0 max-md:flex-1" />
      <Choose value={value.effort} onChange={(effort) => onChange({ ...value, effort })} options={efforts()} label={t("conversation.modelPicker.effort")} disabled={disabled} size={size} className="max-md:min-w-0 max-md:flex-1" />
    </div>
  )
}
