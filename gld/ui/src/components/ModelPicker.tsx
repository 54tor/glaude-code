import { cn } from "@/lib/utils"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { efforts, models, type Choice } from "@/lib/models"
import { Choose } from "./Choose"

// Modèle que claude utilisera sans --model : "model" des réglages utilisateur, sinon ANTHROPIC_MODEL.
let userModel: Promise<string> | undefined
const defaultModel = () =>
  (userModel ??= api
    .config()
    .then((ov) => {
      const d = ov.files.find((f) => f.id === "u")?.data as { model?: string; env?: Record<string, string> } | undefined
      return d?.model || d?.env?.ANTHROPIC_MODEL || ""
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
  onChange: (c: Choice) => void
  disabled?: boolean
  size?: "sm" | "default"
  className?: string
}) {
  const { t } = useTranslation()
  const [def, setDef] = useState("")
  useEffect(() => void defaultModel().then(setDef), [])
  const opts = models().map(([v, l]) => (v || !def ? [v, l] : [v, t("conversation.models.defaultIs", { model: models().find(([a]) => a && def.toLowerCase().includes(a))?.[1] ?? def })])) as unknown as ReturnType<typeof models>
  return (
    <div className={cn("flex min-w-0 flex-wrap gap-2 max-md:flex-1 max-md:flex-nowrap", className)}>
      <Choose value={value.model} onChange={(model) => onChange({ ...value, model })} options={opts} label={t("conversation.modelPicker.model")} disabled={disabled} size={size} className="max-md:min-w-0 max-md:flex-1" />
      <Choose value={value.effort} onChange={(effort) => onChange({ ...value, effort })} options={efforts()} label={t("conversation.modelPicker.effort")} disabled={disabled} size={size} className="max-md:min-w-0 max-md:flex-1" />
    </div>
  )
}
