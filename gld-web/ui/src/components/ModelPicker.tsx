import { cn } from "@/lib/utils"
import { EFFORTS, MODELS, type Choice } from "@/lib/models"
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
  return (
    <div className={cn("flex min-w-0 gap-2", className)}>
      <Choose value={value.model} onChange={(model) => onChange({ ...value, model })} options={MODELS} label="Modèle" disabled={disabled} />
      <Choose value={value.effort} onChange={(effort) => onChange({ ...value, effort })} options={EFFORTS} label="Effort" disabled={disabled} />
    </div>
  )
}
