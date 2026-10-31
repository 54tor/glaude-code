import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

// Radix interdit la valeur "" pour une option : on la remplace par celle-ci le temps de l'affichage.
const EMPTY = "__défaut__"

// Liste déroulante shadcn à partir de paires [valeur, libellé] ; la valeur "" (« par défaut ») est permise.
export function Choose({
  value,
  onChange,
  options,
  label,
  disabled,
  size = "default",
  className,
}: {
  value: string
  onChange: (localValue: string) => void
  options: readonly (readonly [string, string])[]
  label: string
  disabled?: boolean
  size?: "xs" | "sm" | "default"
  className?: string
}) {
  return (
    <Select value={value || EMPTY} onValueChange={(selectedValue) => onChange(selectedValue === EMPTY ? "" : selectedValue)} disabled={disabled}>
      <SelectTrigger size={size} className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map(([optionValue, optionLabel]) => (
          <SelectItem key={optionValue || EMPTY} value={optionValue || EMPTY}>
            {optionLabel}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
