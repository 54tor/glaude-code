import { useState } from "react"
import { cn } from "@/lib/utils"
import { DIR_MIME } from "@/lib/dnd"

// Zone de dépôt d'un dossier glissé depuis la barre latérale ; chaque zone décide de ce que le dépôt fait.
export function DropZone({ onDir, children }: { onDir: (dir: string) => void; children: React.ReactNode }) {
  const [isDraggedOver, setIsDraggedOver] = useState(false)
  const carriesDirectory = (dragEvent: React.DragEvent) => dragEvent.dataTransfer.types.includes(DIR_MIME)
  return (
    <div
      className={cn("rounded-md transition-shadow", isDraggedOver && "ring-2 ring-ring/80")}
      onDragOver={(dragEvent) => {
        if (!carriesDirectory(dragEvent)) return
        dragEvent.preventDefault()
        setIsDraggedOver(true)
      }}
      onDragLeave={(dragEvent) => {
        const isLeavingZone = !dragEvent.currentTarget.contains(dragEvent.relatedTarget as Node | null)
        if (isLeavingZone) setIsDraggedOver(false)
      }}
      onDrop={(dragEvent) => {
        if (!carriesDirectory(dragEvent)) return
        dragEvent.preventDefault()
        setIsDraggedOver(false)
        onDir(dragEvent.dataTransfer.getData(DIR_MIME))
      }}
    >
      {children}
    </div>
  )
}
