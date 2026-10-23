import { useState } from "react"
import { Check, ClipboardCopy, FolderOpen, Globe, Pencil, Plus, RotateCcw, Sparkles, Trash2, X } from "lucide-react"
import type { BackupInfo, CfgEdit, CfgOverview, PermSuggest } from "@/lib/api"
import { api } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Checkbox } from "@/components/ui/checkbox"
import { PermImportDialog, PermIOButtons } from "./ConfigPermIO"
import { PermRawDialog } from "./ConfigPermRaw"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"
import { useActions } from "./actions"
import { toast } from "sonner"
import { useEffect } from "react"

type Doc = Record<string, unknown> | undefined
type Propose = (e: CfgEdit) => Promise<void>

const at = (d: Doc, ...path: string[]): unknown => path.reduce<unknown>((c, k) => (c && typeof c === "object" ? (c as Record<string, unknown>)[k] : undefined), d)
const list = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [])

// --- permissions ---------------------------------------------------------------------------------------------

const KINDS = [
  ["allow", "Autorisées", "Lancées sans demander."],
  ["ask", "À confirmer", "Demandent toujours."],
  ["deny", "Refusées", "Jamais lancées ; l'emportent sur les autres."],
  ["additionalDirectories", "Dossiers supplémentaires", "Claude peut y lire et y travailler en plus du dossier de la session (~/ et chemins relatifs au projet acceptés)."],
] as const
const RULE_KINDS = KINDS.slice(0, 3) // catégories entre lesquelles une règle de permission peut passer
const DIRS = "additionalDirectories"
// Affichage trié (le fichier garde son ordre) : par outil (Bash, Edit, Read…), puis alphabétique, sans tenir compte de la casse.
export const byRule = (a: string, b: string) => a.localeCompare(b, "fr", { sensitivity: "base", numeric: true })

export function PermsTab({ file, label, doc, propose, proposePerms, gone, onTransfer, added, moveRule }: { added: (file: string, kind: string) => string[]; moveRule: (file: string, from: string, rule: string, to: string, text: string) => Promise<void>; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void; gone: (file: string, kind: string, rule: string) => boolean; file: string; label: string; doc: Doc; propose: Propose; proposePerms: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null> }) {
  const a = useActions()
  const home = a.home
  const [importing, setImporting] = useState(false)
  const [raw, setRaw] = useState(false)
  // Explication de Haiku par règle (clé : catégorie + règle) ; « … » pendant la demande.
  const [help, setHelp] = useState<Record<string, string>>({})
  const explain = async (kind: string, rule: string) => {
    const k = kind + "\0" + rule
    if (help[k]) return setHelp((h) => Object.fromEntries(Object.entries(h).filter(([x]) => x !== k))) // deuxième clic : replie
    setHelp((h) => ({ ...h, [k]: "…" }))
    const r = await api.post("/config-perm-explain", { kind, rule })
    setHelp((h) => ({ ...h, [k]: r.ok ? r.out : "Explication impossible : " + r.out }))
  }
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [sug, setSug] = useState<PermSuggest[] | null>(null)
  useEffect(() => {
    let live = true
    api.configPermSuggest().then((r) => live && setSug(r.suggestions))
    return () => {
      live = false
    }
  }, [doc])
  const [edit, setEdit] = useState<{ kind: string; rule: string; text: string; to: string } | null>(null)
  // Modifier une règle : texte et catégorie. La nouvelle est ajoutée avant que l'ancienne soit retirée, pour ne jamais tout perdre.
  const saveEdit = async () => {
    if (!edit) return
    const text = edit.text.trim()
    const same = text === edit.rule && edit.to === edit.kind
    if (!text || same) return setEdit(null)
    setEdit(null)
    await moveRule(file, edit.kind, edit.rule, edit.to, text)
  }
  const add = (kind: string, rule: string) => propose({ file, op: "add", path: ["permissions", kind], value: rule })
  return (
    <div className="space-y-5">
      <PermIOButtons file={file} label={label} doc={doc} onImport={() => setImporting(true)} onRaw={() => setRaw(true)} />
      <PermRawDialog open={raw} file={file} label={label} doc={doc} propose={proposePerms} onClose={() => setRaw(false)} />
      <PermImportDialog open={importing} file={file} label={label} propose={proposePerms} onClose={() => setImporting(false)} />
      {KINDS.map(([kind, label, hint]) => {
        const rules = [...list(at(doc, "permissions", kind)), ...added(file, kind)].filter((r, i, all) => all.indexOf(r) === i && !gone(file, kind, r)).sort(byRule)
        return (
          <section key={kind}>
            <h3 className="text-sm font-medium">
              {label} <span className="text-xs font-normal text-muted-foreground">· {hint}</span>
            </h3>
            <ul className="mt-1 space-y-px">
              {rules.map((r) =>
                edit?.kind === kind && edit.rule === r ? (
                  <li key={r}>
                    <form
                      className="flex items-center gap-2 px-2 py-1"
                      onSubmit={(e) => {
                        e.preventDefault()
                        saveEdit()
                      }}
                    >
                      <Input value={edit.text} onChange={(e) => setEdit({ ...edit, text: e.target.value })} onKeyDown={(e) => e.key === "Escape" && setEdit(null)} autoFocus className="h-8 flex-1 font-mono text-xs" aria-label={`Modifier ${r}`} />
                      {kind !== DIRS && <Choose value={edit.to} onChange={(to) => setEdit({ ...edit, to })} options={RULE_KINDS.map(([k, l]) => [k, l] as const)} label="Catégorie" className="w-36" />}
                      <Button size="icon-xs" variant="ghost" type="submit" aria-label="Enregistrer la règle">
                        <Check />
                      </Button>
                      <Button size="icon-xs" variant="ghost" type="button" aria-label="Annuler" onClick={() => setEdit(null)}>
                        <X />
                      </Button>
                    </form>
                  </li>
                ) : (
                <ContextMenu key={r}>
                <ContextMenuTrigger asChild>
                <li className="group flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted" onDoubleClick={() => setEdit({ kind, rule: r, text: r, to: kind })}>
                  <code className="min-w-0 flex-1 text-xs break-all">{r}</code>
                  {kind !== DIRS && (
                    <Button variant="ghost" size="icon-xs" aria-label={`Expliquer ${r}`} title="Explication par Haiku" onClick={() => explain(kind, r)}>
                      <Sparkles />
                    </Button>
                  )}
                  <Button variant="ghost" size="icon-xs" aria-label={`Modifier ${r}`} title="Modifier (ou double-clic)" onClick={() => setEdit({ kind, rule: r, text: r, to: kind })}>
                    <Pencil />
                  </Button>
                  <Button variant="ghost" size="icon-xs" aria-label={`Retirer ${r}`} onClick={() => propose({ file, op: "remove", path: ["permissions", kind], value: r })}>
                    <X />
                  </Button>
                  {help[kind + "\0" + r] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help[kind + "\0" + r]}</p>}
                </li>
                </ContextMenuTrigger>
                <ContextMenuContent className="w-64">
                  <ContextMenuItem onSelect={() => setEdit({ kind, rule: r, text: r, to: kind })}>
                    <Pencil /> Modifier
                  </ContextMenuItem>
                  <ContextMenuItem onSelect={() => navigator.clipboard.writeText(r).then(() => toast.success("Règle copiée"), () => toast.error("Copie impossible"))}>
                    <ClipboardCopy /> Copier la règle
                  </ContextMenuItem>
                  {kind !== DIRS && (
                  <ContextMenuSub>
                    <ContextMenuSubTrigger>Changer de catégorie</ContextMenuSubTrigger>
                    <ContextMenuSubContent>
                      {RULE_KINDS.filter(([k]) => k !== kind).map(([k, l]) => (
                        <ContextMenuItem
                          key={k}
                          onSelect={() => moveRule(file, kind, r, k, r)}
                        >
                          {l}
                        </ContextMenuItem>
                      ))}
                    </ContextMenuSubContent>
                  </ContextMenuSub>
                  )}
                  {file !== "u" && kind !== DIRS && (
                    <>
                      <ContextMenuSeparator />
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, r, false)}>
                        <Globe /> Copier vers le global
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, r, true)}>
                        <Globe /> Déplacer vers le global
                      </ContextMenuItem>
                    </>
                  )}
                  <ContextMenuSeparator />
                  <ContextMenuItem variant="destructive" onSelect={() => propose({ file, op: "remove", path: ["permissions", kind], value: r })}>
                    <X /> Supprimer
                  </ContextMenuItem>
                </ContextMenuContent>
                </ContextMenu>
                )
              )}
              {rules.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">Aucune.</li>}
            </ul>
            <form
              className="mt-1 flex gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (draft[kind]?.trim()) add(kind, draft[kind]).then(() => setDraft({ ...draft, [kind]: "" }))
              }}
            >
              <Input value={draft[kind] ?? ""} onChange={(e) => setDraft({ ...draft, [kind]: e.target.value })} placeholder={kind === DIRS ? "/chemin/du/dossier" : "Bash(git status:*)"} className="h-8 font-mono text-xs" aria-label={`Nouvelle règle ${label}`} />
              {kind === DIRS && (
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  onClick={async () => {
                    const p = await a.pickDir("Ajouter un dossier supplémentaire", draft[kind] || home)
                    if (p) add(kind, p)
                  }}
                >
                  <FolderOpen /> Parcourir…
                </Button>
              )}
              <Button size="sm" variant="outline" type="submit">
                <Plus /> Ajouter
              </Button>
            </form>
          </section>
        )
      })}
      <section>
        <h3 className="text-sm font-medium">
          Proposées <span className="text-xs font-normal text-muted-foreground">· commandes Bash simples et serveurs MCP revenus au moins 3 fois dans vos 40 dernières sessions, pas encore autorisés. Une règle Bash(cmd:*) autorise toutes les options de cmd, une règle mcp__serveur tous les outils du serveur ; « lecture seule » ne vaut que pour une courte liste de commandes sûres.</span>
        </h3>
        <ul className="mt-1 space-y-px">
          {(sug ?? []).map((s) => (
            <li key={s.rule} className="flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="min-w-0 flex-1 text-xs break-all" title={s.example}>
                {s.rule}
              </code>
              <Button variant="ghost" size="icon-xs" aria-label={`Expliquer ${s.rule}`} title="Explication par Haiku" onClick={() => explain("allow", s.rule)}>
                <Sparkles />
              </Button>
              <span className="shrink-0 text-xs text-muted-foreground">{s.count} fois</span>
              <span className={cn("shrink-0 text-xs", s.readOnly ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>{s.readOnly ? "lecture seule" : "à vérifier"}</span>
              <Button size="xs" variant="outline" onClick={() => add("allow", s.rule)}>
                Autoriser
              </Button>
              {help["allow\0" + s.rule] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help["allow\0" + s.rule]}</p>}
            </li>
          ))}
          {sug?.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">Rien à proposer.</li>}
        </ul>
      </section>
    </div>
  )
}

// --- réglages simples ----------------------------------------------------------------------------------------

type Opt = readonly [string, string] // valeur, libellé
type Field = { key: string; path?: string[]; label: string; kind: "bool" | "number" | "enum"; options?: Opt[] }

// Valeurs connues d'après la documentation de Claude Code. Une valeur déjà présente dans le fichier mais inconnue d'ici
// reste affichée et choisie (voir Row) ; « (par défaut) » retire le réglage.
const MODELS: Opt[] = [
  ["fable", "fable : le plus capable, tâches longues"],
  ["opus", "opus : raisonnement complexe"],
  ["sonnet", "sonnet : codage courant"],
  ["haiku", "haiku : rapide, tâches simples"],
  ["best", "best : le plus capable disponible"],
  ["opusplan", "opusplan : opus en plan, sonnet à l'exécution"],
  ["opus[1m]", "opus avec 1 M de contexte"],
  ["sonnet[1m]", "sonnet avec 1 M de contexte"],
]
const EFFORTS: Opt[] = [["low", "low : rapide"], ["medium", "medium"], ["high", "high"], ["xhigh", "xhigh"], ["max", "max : le plus poussé"]]
const LANGUAGES: Opt[] = ["French", "English", "Spanish", "German", "Italian", "Portuguese", "Dutch", "Japanese", "Chinese", "Korean"].map((l) => [l, l] as const)
const THEMES: Opt[] = [["auto", "auto : suit le terminal"], ["dark", "sombre"], ["light", "clair"], ["dark-daltonized", "sombre, daltonien"], ["light-daltonized", "clair, daltonien"], ["dark-ansi", "sombre, couleurs ANSI"], ["light-ansi", "clair, couleurs ANSI"]]
const NOTIF: Opt[] = [["auto", "auto"], ["terminal_bell", "sonnerie du terminal"], ["iterm2", "iTerm2"], ["iterm2_with_bell", "iTerm2 et sonnerie"], ["kitty", "kitty"], ["ghostty", "Ghostty"], ["notifications_disabled", "aucune notification"]]
const MODES: Opt[] = [["default", "default : demande"], ["acceptEdits", "acceptEdits : accepte les modifications de fichiers"], ["plan", "plan : propose sans modifier"], ["auto", "auto : le classificateur décide"], ["bypassPermissions", "bypassPermissions : tout sans demander (risqué)"]]

const FIELDS: { title: string; fields: Field[] }[] = [
  {
    title: "Modèle et comportement",
    fields: [
      { key: "model", label: "Modèle par défaut", kind: "enum", options: MODELS },
      { key: "effortLevel", label: "Effort", kind: "enum", options: EFFORTS },
      { key: "language", label: "Langue des réponses", kind: "enum", options: LANGUAGES },
      { key: "autoCompactEnabled", label: "Compactage automatique", kind: "bool" },
      { key: "idleCompaction", label: "Compactage à l'inactivité", kind: "bool" },
      { key: "fileCheckpointingEnabled", label: "Points de contrôle des fichiers", kind: "bool" },
      { key: "switchModelsOnFlag", label: "Changer de modèle sur signalement", kind: "bool" },
      { key: "cleanupPeriodDays", label: "Conservation des transcripts (jours)", kind: "number" },
    ],
  },
  {
    title: "Interface",
    fields: [
      { key: "theme", label: "Thème", kind: "enum", options: THEMES },
      { key: "editorMode", label: "Mode d'édition", kind: "enum", options: [["normal", "normal"], ["vim", "vim"]] },
      { key: "promptSuggestionEnabled", label: "Suggestions de prompt", kind: "bool" },
      { key: "awaySummaryEnabled", label: "Résumé au retour", kind: "bool" },
      { key: "preferredNotifChannel", label: "Canal de notification", kind: "enum", options: NOTIF },
      { key: "inputNeededNotifEnabled", label: "Notifier quand une saisie est attendue", kind: "bool" },
      { key: "agentPushNotifEnabled", label: "Notifications push des agents", kind: "bool" },
      { key: "teammateMode", label: "Mode des coéquipiers", kind: "enum", options: [["auto", "auto"], ["in-process", "dans le terminal principal"], ["tmux", "volets tmux"]] },
      { key: "feedbackDrafts", label: "Brouillons de retour", kind: "enum", options: [["notify", "notify : avec notification"], ["quiet", "quiet : sans notification"], ["off", "off : désactivés"]] },
    ],
  },
  {
    title: "Permissions et sécurité",
    fields: [
      { key: "permissions.defaultMode", path: ["permissions", "defaultMode"], label: "Mode de permissions au démarrage", kind: "enum", options: MODES },
      { key: "crossSessionInbound", label: "Messages venant d'autres sessions", kind: "enum", options: [["accept", "accept : les accepter"], ["hold", "hold : attendre votre accord"], ["refuse", "refuse : les refuser"]] },
      { key: "dialogExpiry", label: "Délai des approbations à distance", kind: "enum", options: [["1m", "1 minute"], ["5m", "5 minutes"], ["10m", "10 minutes"], ["30m", "30 minutes"], ["1h", "1 heure"]] },
      { key: "skipDangerousModePermissionPrompt", label: "Ne pas avertir du mode sans permission", kind: "bool" },
      { key: "skipWorkflowUsageWarning", label: "Ne pas avertir de l'usage des workflows", kind: "bool" },
      { key: "workflowKeywordTriggerEnabled", label: "Déclenchement des workflows par mot-clé", kind: "bool" },
      { key: "remoteControlAtStartup", label: "Contrôle à distance au démarrage", kind: "bool" },
      { key: "useAutoModeDuringPlan", label: "Mode auto pendant un plan", kind: "bool" },
    ],
  },
]

function Row({ f, value, file, propose }: { f: Field; value: unknown; file: string; propose: Propose }) {
  const [text, setText] = useState(value === undefined ? "" : String(value))
  const path = f.path ?? [f.key]
  const set = (v: unknown) => propose({ file, op: "set", path, value: v })
  const current = value === undefined ? "" : String(value)
  // une valeur déjà là mais inconnue d'ici reste proposée, pour qu'elle s'affiche et ne soit pas perdue
  const options: Opt[] = f.kind === "enum" ? [["", "(par défaut)"], ...(f.options ?? []), ...(current && !f.options?.some((o) => o[0] === current) ? [[current, `${current} (valeur actuelle)`] as const] : [])] : []
  return (
    <li className="flex flex-wrap items-center gap-2 py-1 text-sm">
      <span className="min-w-56 flex-1">
        {f.label}
        {describeKey(f.key) && <span className="block text-[11px] leading-snug text-muted-foreground">{describeKey(f.key)}</span>}
      </span>
      {f.kind === "bool" ? (
        <Checkbox checked={value === true} onCheckedChange={(c) => set(c === true)} aria-label={f.label} />
      ) : f.kind === "enum" ? (
        <Choose value={current} onChange={(v) => (v === "" ? propose({ file, op: "delete", path }) : v !== current && set(v))} options={options} label={f.label} className="w-72" />
      ) : (
        <>
          <Input
            type="number"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && text.trim() && set(Number(text))}
            placeholder="(par défaut)"
            className="h-8 w-56 text-xs"
            aria-label={f.label}
          />
          <Button size="xs" variant="outline" disabled={!text.trim() || text === current} onClick={() => set(Number(text))}>
            Appliquer
          </Button>
        </>
      )}
      {value !== undefined && f.kind !== "enum" && (
        <Button size="icon-xs" variant="ghost" title="Retirer le réglage (valeur par défaut)" aria-label={`Retirer ${f.label}`} onClick={() => propose({ file, op: "delete", path })}>
          <RotateCcw />
        </Button>
      )}
    </li>
  )
}

export function SettingsTab({ file, doc, plugins, propose }: { file: string; doc: Doc; plugins: CfgOverview["plugins"]; propose: Propose }) {
  const [name, setName] = useState("")
  const [val, setVal] = useState("")
  const env = (at(doc, "env") ?? {}) as Record<string, string>
  const enabled = (at(doc, "enabledPlugins") ?? {}) as Record<string, boolean>
  return (
    <div className="space-y-6">
      {FIELDS.map((g) => (
        <section key={g.title}>
          <h3 className="text-sm font-medium">{g.title}</h3>
          <ul className="mt-1 divide-y">
            {g.fields.map((f) => (
              <Row key={f.key + String(at(doc, ...(f.path ?? [f.key])))} f={f} value={at(doc, ...(f.path ?? [f.key]))} file={file} propose={propose} />
            ))}
          </ul>
        </section>
      ))}
      <section>
        <h3 className="text-sm font-medium">
          Variables d'environnement <span className="text-xs font-normal text-muted-foreground">· les valeurs de secrets sont masquées</span>
        </h3>
        <ul className="mt-1 space-y-px">
          {Object.entries(env).map(([k, v]) => (
            <li key={k} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="text-xs">{k}</code>
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground">= {String(v)}</code>
              <Button variant="ghost" size="icon-xs" aria-label={`Retirer ${k}`} onClick={() => propose({ file, op: "delete", path: ["env", k] })}>
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
        <form
          className="mt-1 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) propose({ file, op: "set", path: ["env", name.trim()], value: val }).then(() => (setName(""), setVal("")))
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="NOM" className="h-8 w-48 font-mono text-xs" aria-label="Nom de la variable" />
          <Input value={val} onChange={(e) => setVal(e.target.value)} placeholder="valeur" className="h-8 flex-1 font-mono text-xs" aria-label="Valeur de la variable" />
          <Button size="sm" variant="outline" type="submit">
            <Plus /> Ajouter
          </Button>
        </form>
      </section>
      {file === "u" && plugins.length > 0 && (
        <section>
          <h3 className="text-sm font-medium">Plugins</h3>
          <ul className="mt-1 grid gap-px sm:grid-cols-2">
            {plugins.map((p) => (
              <li key={p.id} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
                <Checkbox checked={enabled[p.id] === true} onCheckedChange={(c) => propose({ file, op: "set", path: ["enabledPlugins", p.id], value: c === true })} aria-label={p.id} />
                <span className="min-w-0 flex-1 truncate" title={p.id}>
                  {p.id}
                </span>
                {!p.installed && <span className="shrink-0 text-xs text-amber-600 dark:text-amber-400">non installé</span>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

// --- sauvegardes ---------------------------------------------------------------------------------------------

export function BackupsTab({ backups, onRestored, label }: { backups: BackupInfo[]; onRestored: () => void; label: (id: string) => string }) {
  const restore = async (b: BackupInfo) => {
    const r = await api.post("/config-restore", { name: b.name })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) onRestored()
  }
  if (!backups.length) return <p className="py-8 text-sm text-muted-foreground">Aucune sauvegarde : elles sont faites avant chaque modification depuis cette page.</p>
  return (
    <ul className="space-y-px">
      {backups.map((b) => (
        <li key={b.name} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted">
          <span className="w-44 shrink-0 text-xs text-muted-foreground">
            {b.at.slice(0, 4)}-{b.at.slice(4, 6)}-{b.at.slice(6, 8)} {b.at.slice(9, 11)}:{b.at.slice(11, 13)}:{b.at.slice(13, 15)}
          </span>
          <span className="min-w-0 flex-1 truncate">{label(b.file)}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{b.kb} Ko</span>
          <Button size="xs" variant="outline" onClick={() => restore(b)}>
            Restaurer
          </Button>
        </li>
      ))}
    </ul>
  )
}
