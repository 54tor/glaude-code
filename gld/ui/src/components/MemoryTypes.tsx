import { BookMarked, FolderKanban, MessageSquareQuote, User, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { i18n } from "@/i18n"
import { cn } from "@/lib/utils"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// --- types d'entrée -------------------------------------------------------------------------------------

// Les quatre types de mémoire prévus par Claude Code : valeur du champ metadata.type, icône et aide.
// Les textes sont lus à l'affichage (accesseurs) : la langue peut changer après le chargement du module.
export const TYPES: readonly { key: string; label: string; icon: LucideIcon; color: string; what: string; example: string }[] = [
  {
    key: "user",
    get label() {
      return i18n.t("memory.types.user.label")
    },
    icon: User,
    color: "text-info",
    get what() {
      return i18n.t("memory.types.user.what")
    },
    get example() {
      return i18n.t("memory.types.user.example")
    },
  },
  {
    key: "feedback",
    get label() {
      return i18n.t("memory.types.feedback.label")
    },
    icon: MessageSquareQuote,
    color: "text-warning",
    get what() {
      return i18n.t("memory.types.feedback.what")
    },
    get example() {
      return i18n.t("memory.types.feedback.example")
    },
  },
  {
    key: "project",
    get label() {
      return i18n.t("memory.types.project.label")
    },
    icon: FolderKanban,
    color: "text-success",
    get what() {
      return i18n.t("memory.types.project.what")
    },
    get example() {
      return i18n.t("memory.types.project.example")
    },
  },
  {
    key: "reference",
    get label() {
      return i18n.t("memory.types.reference.label")
    },
    icon: BookMarked,
    color: "text-info",
    get what() {
      return i18n.t("memory.types.reference.what")
    },
    get example() {
      return i18n.t("memory.types.reference.example")
    },
  },
]

export const typeOf = (t?: string) => TYPES.find((x) => x.key === t)

export function TypeHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("memory.typeHelp.title")}</DialogTitle>
          <DialogDescription>
            {t("memory.typeHelp.descBefore")} <code>metadata.type</code> {t("memory.typeHelp.descAfter")}
          </DialogDescription>
        </DialogHeader>
        <ul className="space-y-3">
          {TYPES.map((x) => (
            <li key={x.key} className="flex gap-3">
              <x.icon className={cn("mt-0.5 size-5 shrink-0", x.color)} />
              <div className="min-w-0 text-sm">
                <p className="font-medium">
                  {x.label} <code className="text-xs font-normal text-muted-foreground">{x.key}</code>
                </p>
                <p>{x.what}</p>
                <p className="text-xs text-muted-foreground">{t("memory.typeHelp.example", { text: x.example })}</p>
              </div>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">{t("memory.typeHelp.footer")}</p>
      </DialogContent>
    </Dialog>
  )
}
