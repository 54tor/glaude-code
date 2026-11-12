import { useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// Déclencheur (children) qui ouvre l'image en grand dans une modale, au lieu d'un nouvel onglet.
export function ImageModal({ src, name, className, children }: { src: string; name?: string; className?: string; children: ReactNode }) {
  const { t } = useTranslation()
  const [isOpen, setIsOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setIsOpen(true)} title={t("conversation.message.openImage")} className={`cursor-zoom-in ${className ?? ""}`}>
        {children}
      </button>
      <Dialog open={isOpen} onOpenChange={setIsOpen}>
        <DialogContent className="flex max-h-[92vh] min-w-0 flex-col sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle className="text-sm break-all">{name ?? t("conversation.message.imageAlt")}</DialogTitle>
            <DialogDescription className="sr-only">{t("conversation.message.imageAlt")}</DialogDescription>
          </DialogHeader>
          <img src={src} alt={name ?? t("conversation.message.imageAlt")} className="min-h-0 max-h-[78vh] w-full rounded-lg object-contain" />
        </DialogContent>
      </Dialog>
    </>
  )
}
