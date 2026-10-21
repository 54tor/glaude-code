import { BookMarked, FolderKanban, MessageSquareQuote, User, type LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// --- types d'entrée -------------------------------------------------------------------------------------

// Les quatre types de mémoire prévus par Claude Code : valeur du champ metadata.type, icône et aide.
export const TYPES: readonly { key: string; label: string; icon: LucideIcon; color: string; what: string; example: string }[] = [
  {
    key: "user",
    label: "utilisateur",
    icon: User,
    color: "text-sky-500",
    what: "Qui vous êtes : rôle, compétences, façon de travailler. Aide Claude à adapter ses explications.",
    example: "Développeur Go senior, découvre React ; préfère des réponses courtes.",
  },
  {
    key: "feedback",
    label: "retour",
    icon: MessageSquareQuote,
    color: "text-amber-500",
    what: "Une consigne ou une correction que vous avez donnée, avec la raison (Why) et la façon de l'appliquer (How to apply).",
    example: "Ne jamais ajouter de ligne d'attribution dans les commits.",
  },
  {
    key: "project",
    label: "projet",
    icon: FolderKanban,
    color: "text-emerald-500",
    what: "Un travail en cours, une décision ou une contrainte qui ne se déduit pas du code ni de git. Dates en absolu.",
    example: "Gel des fusions jusqu'au 5 novembre : release mobile en cours.",
  },
  {
    key: "reference",
    label: "référence",
    icon: BookMarked,
    color: "text-violet-500",
    what: "Où retrouver une information hors du dépôt : tableau de bord, ticket, document, canal.",
    example: "Les bugs de l'ingestion sont suivis dans le projet Linear « INGEST ».",
  },
]

export const typeOf = (t?: string) => TYPES.find((x) => x.key === t)

export function TypeHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Les quatre types de mémoire</DialogTitle>
          <DialogDescription>
            Champ <code>metadata.type</code> du front matter d'une entrée. Il sert à ranger et retrouver les entrées ; il ne change pas ce que Claude charge.
          </DialogDescription>
        </DialogHeader>
        <ul className="space-y-3">
          {TYPES.map((t) => (
            <li key={t.key} className="flex gap-3">
              <t.icon className={cn("mt-0.5 size-5 shrink-0", t.color)} />
              <div className="min-w-0 text-sm">
                <p className="font-medium">
                  {t.label} <code className="text-xs font-normal text-muted-foreground">{t.key}</code>
                </p>
                <p>{t.what}</p>
                <p className="text-xs text-muted-foreground">Exemple : {t.example}</p>
              </div>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">
          À ne pas mémoriser : ce que le code, git ou un CLAUDE.md disent déjà, et ce qui ne vaut que pour la conversation en cours.
        </p>
      </DialogContent>
    </Dialog>
  )
}
