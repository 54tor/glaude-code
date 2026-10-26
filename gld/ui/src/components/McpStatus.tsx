import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { Loader2, Plug } from "lucide-react"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

type State = { name: string; status: string; ok: boolean }

// État des serveurs MCP d'un projet (`claude mcp list` lancé dans son dossier, gardé une minute côté serveur) : pastille verte si tous
// répondent, orange sinon, et la liste au clic.
export function McpStatus({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [res, setRes] = useState<{ dir: string; st: State[] | false } | null>(null) // st false : indisponible
  const st = res?.dir === dir ? res.st : null // null : relevé en cours
  useEffect(() => {
    let live = true
    api.post("/project-mcp", { dir }).then(
      (r) => live && setRes({ dir, st: r.ok ? ((r as unknown as { servers: State[] }).servers ?? []) : false }),
      () => live && setRes({ dir, st: false })
    )
    return () => {
      live = false
    }
  }, [dir])
  if (st === false || (st && st.length === 0)) return null
  const bad = st ? st.filter((s) => !s.ok).length : 0
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" title={t("shell.header.mcpTitle")}>
          {st ? <Plug className={bad ? "text-warning" : "text-success"} /> : <Loader2 className="animate-spin" />} MCP{st && ` ${st.length - bad}/${st.length}`}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-96 w-96 overflow-y-auto">
        <DropdownMenuLabel>{t("shell.header.mcpLabel")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {(st || []).map((s) => (
          <DropdownMenuItem key={s.name} onSelect={(e) => e.preventDefault()} className="flex-col items-start gap-0">
            <span className="font-medium">{s.name}</span>
            <span className={cn("text-xs", s.ok ? "text-success" : "text-warning")}>{s.status}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
