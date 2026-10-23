import { useEffect, useState } from "react"
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { api, type CfgIssue, type EffRow, type HookInfo, type McpServer } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"

const LV: Record<string, string> = { u: "utilisateur", p: "projet", l: "local" }
const KIND: Record<string, string> = { json: "JSON", hook: "Hook", permission: "Permission", plugin: "Plugin", secret: "Secret" }
const fileLabel = (id: string, home: string, projects: { dir: string; project: string }[]) =>
  id === "u" ? "~/.claude/settings.json" : `${id[0] === "p" ? "projet" : "local"} · ${tilde(projects.find((p) => p.dir === id.slice(2))?.project ?? id.slice(2), home)}`

// --- santé ---------------------------------------------------------------------------------------------------

export function HealthTab({ issues, home, projects }: { issues: CfgIssue[]; home: string; projects: { dir: string; project: string }[] }) {
  if (!issues.length)
    return (
      <p className="flex items-center gap-2 py-8 text-sm text-muted-foreground">
        <CheckCircle2 className="size-4 text-emerald-500" /> Rien à signaler : JSON valides, hooks trouvés, pas de règle en double ni de plugin manquant.
      </p>
    )
  return (
    <ul className="space-y-1">
      {issues.map((i, n) => (
        <li key={n} className="flex items-start gap-2 rounded-md border px-3 py-2 text-sm">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-500" />
          <span className="min-w-0">
            <span className="block text-xs font-medium text-muted-foreground">
              {KIND[i.kind] ?? i.kind} · {fileLabel(i.file, home, projects)}
            </span>
            <span className="break-words">{i.msg}</span>
          </span>
        </li>
      ))}
    </ul>
  )
}

// --- hooks ---------------------------------------------------------------------------------------------------

export function HooksTab({ hooks }: { hooks: HookInfo[] }) {
  if (!hooks.length) return <p className="py-8 text-sm text-muted-foreground">Aucun hook déclaré.</p>
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">Lecture seule : un hook s'édite dans le fichier de réglages. Un fichier ou un programme introuvable est signalé.</p>
      <ul className="space-y-1">
        {hooks.map((h, n) => (
          <li key={n} className="rounded-md border px-3 py-2 text-sm">
            <p className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{h.event}</span>
              {h.matcher && <code className="rounded bg-muted px-1 text-xs">{h.matcher}</code>}
              <span className="text-xs text-muted-foreground">{LV[h.scope]}</span>
              {h.missing && <span className="text-xs text-amber-600 dark:text-amber-400">introuvable : {h.missing}</span>}
            </p>
            <code className="mt-1 block text-xs break-all text-muted-foreground">{h.command || `(${h.type})`}</code>
          </li>
        ))}
      </ul>
    </div>
  )
}

// --- précédence ----------------------------------------------------------------------------------------------

export function PrecedenceTab({ projects, home }: { projects: { dir: string; project: string }[]; home: string }) {
  const [dir, setDir] = useState("")
  const [q, setQ] = useState("")
  const [rows, setRows] = useState<EffRow[] | null>(null)
  useEffect(() => {
    let live = true
    api.configEffective(dir).then((r) => live && setRows(r.rows))
    return () => {
      live = false
    }
  }, [dir])
  const f = q.toLowerCase()
  const shown = (rows ?? []).filter((r) => !f || r.path.toLowerCase().includes(f) || Object.values(r.values).some((v) => v.toLowerCase().includes(f)))
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm">Vu depuis</span>
        <Choose value={dir} onChange={setDir} options={[["", "l'utilisateur seul"], ...projects.map((p) => [p.dir, tilde(p.project, home)] as const)]} label="Projet" className="min-w-0 max-w-80" />
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filtrer…" className="h-8 w-48" aria-label="Filtrer les réglages" />
        <span className="text-xs text-muted-foreground">Local &gt; projet &gt; utilisateur ; les listes s'additionnent.</span>
      </div>
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full table-fixed text-sm">
          <colgroup>
            <col className="w-[28%]" />
            <col className="w-[24%]" />
            <col className="w-[24%]" />
            <col className="w-[24%]" />
          </colgroup>
          <thead className="bg-muted/50 text-left text-xs font-semibold text-muted-foreground">
            <tr>
              <th className="px-3 py-1.5">Réglage</th>
              <th className="px-3 py-1.5">Utilisateur</th>
              <th className="px-3 py-1.5">Projet</th>
              <th className="px-3 py-1.5">Local</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((r) => (
              <tr key={r.path} className="border-t align-top">
                <td className="px-3 py-1.5">
                  <span className="font-mono text-xs break-all">{r.path}</span>
                  {describeKey(r.path) && <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">{describeKey(r.path)}</span>}
                </td>
                {(["u", "p", "l"] as const).map((lv) => (
                  <td key={lv} title={r.values[lv]} className={cn("px-3 py-1.5 text-xs break-words", r.values[lv] !== undefined && (r.merged || r.winner === lv) ? "font-medium" : "text-muted-foreground line-through decoration-muted-foreground/40")}>
                    <span className="line-clamp-3">{r.values[lv] ?? ""}</span>
                  </td>
                ))}
              </tr>
            ))}
            {rows && shown.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-muted-foreground">
                  Rien ne correspond.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-muted-foreground">Une valeur barrée est écrasée par un niveau plus prioritaire. Les secrets sont masqués.</p>
    </div>
  )
}

// --- MCP (lecture seule) -------------------------------------------------------------------------------------

export function McpTab({ home }: { home: string }) {
  const [servers, setServers] = useState<McpServer[] | null>(null)
  const [status, setStatus] = useState<Record<string, string> | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.configMcp().then((r) => setServers(r.servers))
  }, [])
  const check = async () => {
    setBusy(true)
    const r = (await api.post("/config-mcp-status", {})) as { ok: boolean; out?: string; status?: Record<string, string> }
    setBusy(false)
    if (r.ok) setStatus(r.status ?? {})
    else toast.error(r.out ?? "Échec")
  }
  const known = new Set((servers ?? []).map((s) => s.name))
  const others = Object.keys(status ?? {}).filter((n) => !known.has(n))
  const ok = (s?: string) => s && /✔|Connected/.test(s)
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" onClick={check} disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <RefreshCw />} Vérifier l'état
        </Button>
        <span className="text-xs text-muted-foreground">Lecture seule : la configuration MCP se modifie avec `claude mcp`. Les secrets sont masqués.</span>
      </div>
      <ul className="space-y-1">
        {(servers ?? []).map((s, n) => (
          <li key={n} className="rounded-md border px-3 py-2 text-sm">
            <p className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{s.name}</span>
              <span className="text-xs text-muted-foreground">
                {s.type} · {s.scope === "user" ? "utilisateur" : s.scope === "project" ? ".mcp.json" : "local"}
                {s.project ? ` · ${tilde(s.project, home)}` : ""}
              </span>
              {status && <span className={cn("text-xs", ok(status[s.name]) ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>{status[s.name] ?? "état inconnu"}</span>}
            </p>
            <code className="mt-1 block text-xs break-all text-muted-foreground">{s.url || s.command}</code>
            {!!s.envKeys?.length && <p className="text-xs text-muted-foreground">variables : {s.envKeys.join(", ")}</p>}
          </li>
        ))}
        {servers?.length === 0 && <li className="py-4 text-sm text-muted-foreground">Aucun serveur déclaré dans ~/.claude.json ni dans un .mcp.json.</li>}
      </ul>
      {others.length > 0 && (
        <div>
          <p className="mb-1 text-xs font-medium text-muted-foreground">Autres serveurs (plugins, claude.ai)</p>
          <ul className="space-y-1">
            {others.map((n) => (
              <li key={n} className="flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm">
                <span className="min-w-0 flex-1 truncate">{n}</span>
                <span className={cn("shrink-0 text-xs", ok(status![n]) ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>{status![n]}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
