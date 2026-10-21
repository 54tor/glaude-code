import { cn } from "@/lib/utils"
import { EFFORTS, MODELS, type Choice } from "@/lib/models"

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
  const sel = "h-8 min-w-0 rounded-md border bg-background px-2 text-xs"
  return (
    <div className={cn("flex min-w-0 gap-2", className)}>
      <select
        value={value.model}
        onChange={(e) => onChange({ ...value, model: e.target.value })}
        disabled={disabled}
        className={sel}
        aria-label="Modèle"
      >
        {MODELS.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
      <select
        value={value.effort}
        onChange={(e) => onChange({ ...value, effort: e.target.value })}
        disabled={disabled}
        className={sel}
        aria-label="Effort"
      >
        {EFFORTS.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
    </div>
  )
}
