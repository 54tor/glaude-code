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
  size = "sm",
  className,
}: {
  value: string
  onChange: (v: string) => void
  options: readonly (readonly [string, string])[]
  label: string
  disabled?: boolean
  size?: "sm" | "default"
  className?: string
}) {
  return (
    <Select value={value || EMPTY} onValueChange={(v) => onChange(v === EMPTY ? "" : v)} disabled={disabled}>
      <SelectTrigger size={size} className={className} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map(([v, l]) => (
          <SelectItem key={v || EMPTY} value={v || EMPTY}>
            {l}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
