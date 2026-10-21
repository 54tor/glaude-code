import { useEffect, useState } from "react"
import { ChevronRight, ClipboardCopy, Globe, X } from "lucide-react"
import { Link } from "wouter"
import { api, type ProjectCfg } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { href } from "@/lib/route"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"

const KINDS = [
  ["allow", "Autorisées"],
  ["ask", "À confirmer"],
  ["deny", "Refusées"],
] as const

const Badge = ({ n, label }: { n: number; label: string }) =>
  n > 0 ? (
    <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
      {n} {label}
    </span>
  ) : null

// Ce que chaque projet configure en propre, en plus des réglages de l'utilisateur : réglages (projet et local), règles de
// permission, hooks, serveurs MCP, règles .claude/rules, skills et documentation. onEdit ouvre l'onglet qui permet de les modifier.
export function ProjectsTab({ home, version, gone, onEdit, onTransfer, onRemove }: { home: string; version: number; gone: (file: string, kind: string, rule: string) => boolean; onEdit: (file: string, tab: string) => void; onRemove: (file: string, kind: string, rule: string) => void; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void }) {
  const [list, setList] = useState<ProjectCfg[] | null>(null)
  const [all, setAll] = useState(false)
  const [q, setQ] = useState("")
  const [open, setOpen] = useState<Set<string>>(new Set())
  useEffect(() => {
    api.configProjects().then((r) => setList(r.projects))
  }, [version])
  if (!list) return <p className="py-8 text-sm text-muted-foreground">Chargement…</p>
  const f = q.toLowerCase()
  const shown = list.filter((p) => (all || p.specific > 0) && (!f || p.project.toLowerCase().includes(f)))
  const hidden = list.filter((p) => p.specific === 0).length
  const toggle = (d: string) =>
    setOpen((o) => {
      const n = new Set(o)
      if (n.has(d)) n.delete(d)
      else n.add(d)
      return n
    })
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filtrer les projets…" className="h-8 w-64" aria-label="Filtrer les projets" />
        {hidden > 0 && (
          <Button size="sm" variant={all ? "secondary" : "ghost"} onClick={() => setAll(!all)} aria-pressed={all}>
            {all ? "Masquer" : "Afficher aussi"} les {hidden} projet{hidden > 1 ? "s" : ""} sans réglage propre
          </Button>
        )}
        <span className="text-xs text-muted-foreground">Ce que chaque projet ajoute ou change par rapport à vos réglages personnels.</span>
      </div>
      <ul className="space-y-2">
        {shown.map((p) => {
          const isOpen = open.has(p.dir)
          const perms = Object.fromEntries(Object.entries(p.perms).map(([k, l]) => [k, l.filter((r) => !gone(`${r.scope}/${p.dir}`, k, r.rule))]))
          const nRules = Object.values(perms).reduce((n, l) => n + l.length, 0)
          const nSettings = p.files.reduce((n, x) => n + x.rows.filter((r) => !r.path.startsWith("permissions.") && r.path !== "hooks").length, 0)
          return (
            <li key={p.dir} className="rounded-lg border">
              <button onClick={() => toggle(p.dir)} className="flex w-full flex-wrap items-center gap-2 px-3 py-2 text-left" aria-expanded={isOpen}>
                <ChevronRight className={cn("size-4 shrink-0 transition-transform", isOpen && "rotate-90")} />
                <span className="min-w-0 flex-1 truncate text-sm font-medium" title={p.project}>
                  {tilde(p.project, home)}
                </span>
                <Badge n={nRules} label="règles de permission" />
                <Badge n={nSettings} label="réglages" />
                <Badge n={p.hooks.length} label="hooks" />
                <Badge n={p.mcp.length} label="MCP" />
                <Badge n={p.rules.length} label="règles .claude/rules" />
                <Badge n={p.skills.length} label="skills" />
                {p.specific === 0 && <span className="text-xs text-muted-foreground">rien de spécifique</span>}
              </button>
              {isOpen && (
                <div className="space-y-4 border-t px-4 py-3 text-sm">
                  <section>
                    <div className="mb-1 flex items-center gap-2">
                      <h3 className="text-xs font-medium text-muted-foreground">Règles de permission</h3>
                      <Button size="xs" variant="ghost" onClick={() => onEdit(p.files.find((x) => x.id[0] === "p")?.id ?? "p/" + p.dir, "permissions")}>
                        Gérer…
                      </Button>
                    </div>
                    {nRules === 0 ? (
                      <p className="text-xs text-muted-foreground">Aucune : ce projet suit vos règles personnelles.</p>
                    ) : (
                      KINDS.filter(([k]) => perms[k]?.length).map(([k, label]) => (
                        <div key={k} className="mb-2">
                          <p className="text-xs text-muted-foreground">{label}</p>
                          <ul className="space-y-px">
                            {perms[k].map((r, i) => (
                              <ContextMenu key={k + i}>
                              <ContextMenuTrigger asChild>
                              <li className="flex items-center gap-2">
                                <code className="min-w-0 flex-1 text-xs break-all">{r.rule}</code>
                                <span className="shrink-0 text-[11px] text-muted-foreground">{r.scope === "p" ? "projet" : "local"}</span>
                                <DropdownMenu>
                                  <DropdownMenuTrigger asChild>
                                    <Button size="xs" variant="ghost">
                                      <Globe /> Vers le global
                                    </Button>
                                  </DropdownMenuTrigger>
                                  <DropdownMenuContent align="end">
                                    <DropdownMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, false)}>Copier dans vos réglages</DropdownMenuItem>
                                    <DropdownMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, true)}>Déplacer (retirer du projet)</DropdownMenuItem>
                                  </DropdownMenuContent>
                                </DropdownMenu>
                                <Button size="icon-xs" variant="ghost" aria-label={`Supprimer ${r.rule}`} title="Supprimer cette règle" onClick={() => onRemove(`${r.scope}/${p.dir}`, k, r.rule)}>
                                  <X />
                                </Button>
                              </li>
                              </ContextMenuTrigger>
                              <ContextMenuContent className="w-60">
                                <ContextMenuItem onSelect={() => navigator.clipboard.writeText(r.rule).then(() => toast.success("Règle copiée"), () => toast.error("Copie impossible"))}>
                                  <ClipboardCopy /> Copier la règle
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, false)}>
                                  <Globe /> Copier vers le global
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, true)}>
                                  <Globe /> Déplacer vers le global
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem variant="destructive" onSelect={() => onRemove(`${r.scope}/${p.dir}`, k, r.rule)}>
                                  <X /> Supprimer
                                </ContextMenuItem>
                              </ContextMenuContent>
                              </ContextMenu>
                            ))}
                          </ul>
                        </div>
                      ))
                    )}
                  </section>
                  {p.files.map((fl) => {
                    const rows = fl.rows.filter((r) => !r.path.startsWith("permissions.") && r.path !== "hooks") // les hooks ont leur section
                    return (
                      <section key={fl.id}>
                        <div className="mb-1 flex items-center gap-2">
                          <h3 className="text-xs font-medium text-muted-foreground">
                            Réglages · <code>{fl.label}</code> {fl.id[0] === "p" ? "(versionné avec le projet)" : "(à vous seul)"}
                          </h3>
                          <Button size="xs" variant="ghost" onClick={() => onEdit(fl.id, "json")}>
                            JSON…
                          </Button>
                        </div>
                        {rows.length === 0 ? (
                          <p className="text-xs text-muted-foreground">Rien d'autre que des permissions.</p>
                        ) : (
                          <ul className="space-y-1">
                            {rows.map((r) => (
                              <li key={r.path} className="text-xs">
                                <code className="font-medium">{r.path}</code> <span className="text-muted-foreground">=</span> <span className="break-all">{r.value}</span>
                                {describeKey(r.path) && <span className="block text-[11px] text-muted-foreground">{describeKey(r.path)}</span>}
                              </li>
                            ))}
                          </ul>
                        )}
                      </section>
                    )
                  })}
                  {p.files.length === 0 && (
                    <p className="text-xs text-muted-foreground">
                      Pas de fichier de réglages propre.{" "}
                      <button className="underline" onClick={() => onEdit("l/" + p.dir, "reglages")}>
                        Créer un réglage local
                      </button>
                    </p>
                  )}
                  {p.hooks.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">Hooks</h3>
                      <ul className="space-y-1">
                        {p.hooks.map((h, i) => (
                          <li key={i} className="text-xs">
                            <span className="font-medium">{h.event}</span> {h.matcher && <code className="rounded bg-muted px-1">{h.matcher}</code>}{" "}
                            <code className="break-all text-muted-foreground">{h.command}</code>
                            {h.missing && <span className="ml-1 text-amber-600 dark:text-amber-400">introuvable : {h.missing}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {p.mcp.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">Serveurs MCP</h3>
                      <ul className="space-y-1">
                        {p.mcp.map((m) => (
                          <li key={m.name + m.scope} className="text-xs">
                            <span className="font-medium">{m.name}</span> <span className="text-muted-foreground">{m.type} · {m.scope === "project" ? ".mcp.json" : "local"}</span>{" "}
                            <code className="break-all text-muted-foreground">{m.url || m.command}</code>
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {p.rules.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">
                        Règles du projet · <code>.claude/rules</code> ·{" "}
                        <Link className="underline" href={href({ kind: "memory", key: `r/${p.dir}/${p.rules[0].name}` })}>
                          gérer, modifier, transférer vers le global
                        </Link>
                      </h3>
                      <ul className="space-y-1">
                        {p.rules.map((r) => (
                          <li key={r.name} className="text-xs">
                            <Link className="font-medium underline-offset-2 hover:underline" href={href({ kind: "memory", key: `r/${p.dir}/${r.name}` })}>
                              <code>{r.name}</code>
                            </Link>{" "}
                            <span className="text-muted-foreground">{r.paths?.length ? `chargée seulement pour ${r.paths.join(", ")}` : "chargée à chaque session"}</span>
                            {r.warn && <span className="ml-1 text-amber-600 dark:text-amber-400">{r.warn}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {(p.skills.length > 0 || p.docs.length > 0) && (
                    <p className="text-xs text-muted-foreground">
                      {p.skills.length > 0 && <>Skills : {p.skills.map((s) => "/" + s).join(", ")}. </>}
                      {p.docs.length > 0 && <>Documentation : {p.docs.join(", ")}.</>}
                    </p>
                  )}
                </div>
              )}
            </li>
          )
        })}
        {shown.length === 0 && <li className="py-6 text-center text-sm text-muted-foreground">Aucun projet{q ? " ne correspond" : " n'a de réglage propre"}.</li>}
      </ul>
    </div>
  )
}
