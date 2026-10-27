import { useState } from "react"
import { cn } from "@/lib/utils"
import { DIR_MIME } from "@/lib/dnd"

// Zone de dépôt d'un dossier glissé depuis la barre latérale ; chaque zone décide de ce que le dépôt fait.
export function DropZone({ onDir, children }: { onDir: (dir: string) => void; children: React.ReactNode }) {
  const [over, setOver] = useState(false)
  const has = (e: React.DragEvent) => e.dataTransfer.types.includes(DIR_MIME)
  return (
    <div
      className={cn("rounded-md transition-shadow", over && "ring-2 ring-ring/50")}
      onDragOver={(e) => {
        if (!has(e)) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
      }}
      onDrop={(e) => {
        if (!has(e)) return
        e.preventDefault()
        setOver(false)
        onDir(e.dataTransfer.getData(DIR_MIME))
      }}
    >
      {children}
    </div>
  )
}
