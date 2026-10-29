import { FileText } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { body } from "@/lib/commands"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"

// Menu « Insérer un snippet » : lit la commande choisie et rend son corps (sans en-tête) à onInsert.
export function SnippetMenu({ onInsert }: { onInsert: (text: string) => void }) {
  const { t } = useTranslation()
  const a = useActions()
  if (a.snippets.length === 0) return null
  const pick = async (key: string) => {
    const r = await api.snippet(key).catch((e) => void toast.error(errMsg(e) || t("skills.snippetMenu.unreadable")))
    if (r) onInsert(body(r.text))
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          <FileText /> {t("skills.snippetMenu.insert")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()} align="start" className="max-h-72 w-72 overflow-y-auto">
        {a.snippets.map((s) => (
          <DropdownMenuItem key={s.key} onSelect={() => pick(s.key)} title={s.description || s.path}>
            <FileText /> <span className="truncate">{s.command}</span>
            {s.scope && <span className="ml-auto truncate text-xs text-muted-foreground">{tilde(s.project, a.home)}</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
