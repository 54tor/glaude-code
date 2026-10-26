import { useTranslation } from "react-i18next"
import { useState } from "react"
import { Loader2, Plug } from "lucide-react"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

type State = { name: string; status: string; ok: boolean }
type Res = { dir: string; servers: State[]; error?: string }

// État des serveurs MCP d'un projet. Le relevé (`claude mcp list` dans son dossier) démarre les serveurs du projet : il ne part qu'au clic,
// et le serveur le refuse pour un dossier dont Claude Code n'a pas accepté la confiance. Gardé une minute côté serveur.
export function McpStatus({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [res, setRes] = useState<Res | null>(null)
  const [busy, setBusy] = useState(false)
  const cur = res?.dir === dir ? res : null
  const load = async () => {
    setBusy(true)
    try {
      const r = await api.post("/project-mcp", { dir })
      setRes({ dir, servers: (r as unknown as { servers?: State[] }).servers ?? [], error: r.ok ? undefined : r.out })
    } catch {
      setRes({ dir, servers: [], error: t("shell.header.mcpFailed") })
    }
    setBusy(false)
  }
  const bad = cur ? cur.servers.filter((s) => !s.ok).length : 0
  return (
    <DropdownMenu onOpenChange={(o) => o && !busy && load()}>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" title={t("shell.header.mcpTitle")}>
          {busy ? <Loader2 className="animate-spin" /> : <Plug className={cur && !cur.error ? (bad ? "text-warning" : "text-success") : undefined} />} MCP
          {cur && !cur.error && ` ${cur.servers.length - bad}/${cur.servers.length}`}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-96 w-96 overflow-y-auto">
        <DropdownMenuLabel>{t("shell.header.mcpLabel")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {!cur && <p className="px-2 py-3 text-xs text-muted-foreground">{t("shell.header.mcpChecking")}</p>}
        {cur?.error && <p className="px-2 py-3 text-xs text-warning">{cur.error}</p>}
        {cur?.servers.map((s) => (
          <DropdownMenuItem key={s.name} onSelect={(e) => e.preventDefault()} className="flex-col items-start gap-0">
            <span className="font-medium">{s.name}</span>
            <span className={cn("text-xs", s.ok ? "text-success" : "text-warning")}>{s.status}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
