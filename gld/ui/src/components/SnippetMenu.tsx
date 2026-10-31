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
  const actions = useActions()
  if (actions.snippets.length === 0) return null
  const insertSnippet = async (snippetKey: string) => {
    const snippet = await api.snippet(snippetKey).catch((error) => void toast.error(errMsg(error) || t("skills.snippetMenu.unreadable")))
    if (snippet) onInsert(body(snippet.text))
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          <FileText /> {t("skills.snippetMenu.insert")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent onCloseAutoFocus={(focusEvent) => focusEvent.preventDefault()} align="start" className="max-h-72 w-72 overflow-y-auto">
        {actions.snippets.map((snippet) => (
          <DropdownMenuItem key={snippet.key} onSelect={() => insertSnippet(snippet.key)} title={snippet.description || snippet.path}>
            <FileText /> <span className="truncate">{snippet.command}</span>
            {snippet.scope && <span className="ml-auto truncate text-xs text-muted-foreground">{tilde(snippet.project, actions.home)}</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
