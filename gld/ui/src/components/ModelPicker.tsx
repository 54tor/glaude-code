import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"
import { efforts, models, type Choice } from "@/lib/models"
import { Choose } from "./Choose"

// Choix du modèle et de l'effort d'un prompt (Réglages, fenêtre de prompt, chat du lecteur).
export function ModelPicker({
  value,
  onChange,
  disabled,
  className,
}: {
  value: Choice
  onChange: (c: Choice) => void
  disabled?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <div className={cn("flex min-w-0 flex-wrap gap-2 max-md:flex-1 max-md:flex-nowrap", className)}>
      <Choose value={value.model} onChange={(model) => onChange({ ...value, model })} options={models()} label={t("conversation.modelPicker.model")} disabled={disabled} className="max-md:min-w-0 max-md:flex-1" />
      <Choose value={value.effort} onChange={(effort) => onChange({ ...value, effort })} options={efforts()} label={t("conversation.modelPicker.effort")} disabled={disabled} className="max-md:min-w-0 max-md:flex-1" />
    </div>
  )
}
