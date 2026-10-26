import { loc } from "@/i18n"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
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
import { takeConfigRaw } from "@/lib/wantConfigFile"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"
import { useActions } from "./actions"
import { toast } from "sonner"
import { useEffect } from "react"
import { reportResult } from "@/lib/report"

type Doc = Record<string, unknown> | undefined
type Propose = (e: CfgEdit) => Promise<void>

const at = (d: Doc, ...path: string[]): unknown => path.reduce<unknown>((c, k) => (c && typeof c === "object" ? (c as Record<string, unknown>)[k] : undefined), d)
const list = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [])

// --- permissions ---------------------------------------------------------------------------------------------

const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const
const RULE_KINDS = KINDS.slice(0, 3) // catégories entre lesquelles une règle de permission peut passer
const DIRS = "additionalDirectories"
// Affichage trié (le fichier garde son ordre) : par outil (Bash, Edit, Read…), puis alphabétique, sans tenir compte de la casse.
export const byRule = (a: string, b: string) => a.localeCompare(b, loc(), { sensitivity: "base", numeric: true })

export function PermsTab({ file, label, doc, propose, proposePerms, gone, onTransfer, added, moveRule }: { added: (file: string, kind: string) => string[]; moveRule: (file: string, from: string, rule: string, to: string, text: string) => Promise<void>; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void; gone: (file: string, kind: string, rule: string) => boolean; file: string; label: string; doc: Doc; propose: Propose; proposePerms: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null> }) {
  const { t } = useTranslation()
  const a = useActions()
  const home = a.home
  const [importing, setImporting] = useState(false)
  const [raw, setRaw] = useState(takeConfigRaw) // ouvert d'emblée depuis le bouton Permissions d'un dossier
  // Explication de Haiku par règle (clé : catégorie + règle) ; « … » pendant la demande.
  const [help, setHelp] = useState<Record<string, string>>({})
  const explain = async (kind: string, rule: string) => {
    const k = kind + "\0" + rule
    if (help[k]) return setHelp((h) => Object.fromEntries(Object.entries(h).filter(([x]) => x !== k))) // deuxième clic : replie
    setHelp((h) => ({ ...h, [k]: "…" }))
    const r = await api.post("/config-perm-explain", { kind, rule })
    setHelp((h) => ({ ...h, [k]: r.ok ? r.out : t("config.perms.explainFailed", { out: r.out }) }))
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
  // Vide les quatre listes : l'aperçu montre l'écart et la sauvegarde précède l'écriture, comme pour un import.
  const reset = async () => {
    const err = await proposePerms(file, JSON.stringify({ allow: [], ask: [], deny: [], additionalDirectories: [] }), "replace")
    if (err) toast.error(err)
  }
  return (
    <div className="space-y-5">
      <PermIOButtons file={file} label={label} doc={doc} onImport={() => setImporting(true)} onRaw={() => setRaw(true)} onReset={reset} />
      <PermRawDialog open={raw} file={file} label={label} doc={doc} propose={proposePerms} onClose={() => setRaw(false)} />
      <PermImportDialog open={importing} file={file} label={label} propose={proposePerms} onClose={() => setImporting(false)} />
      {KINDS.map((kind) => {
        const label = t(`config.perms.kind.${kind}`)
        const hint = t(`config.perms.hint.${kind}`)
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
                      <Input value={edit.text} onChange={(e) => setEdit({ ...edit, text: e.target.value })} onKeyDown={(e) => e.key === "Escape" && setEdit(null)} autoFocus className="h-8 flex-1 font-mono text-xs" aria-label={t("config.perms.editRule", { rule: r })} />
                      {kind !== DIRS && <Choose value={edit.to} onChange={(to) => setEdit({ ...edit, to })} options={RULE_KINDS.map((k) => [k, t(`config.perms.kind.${k}`)] as const)} label={t("config.perms.category")} className="w-36" />}
                      <Button size="icon-xs" variant="ghost" type="submit" aria-label={t("config.perms.saveRule")}>
                        <Check />
                      </Button>
                      <Button size="icon-xs" variant="ghost" type="button" aria-label={t("common.cancel")} onClick={() => setEdit(null)}>
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
                    <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.explain", { rule: r })} title={t("config.perms.explainTitle")} onClick={() => explain(kind, r)}>
                      <Sparkles />
                    </Button>
                  )}
                  <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.editRule", { rule: r })} title={t("config.perms.editTitle")} onClick={() => setEdit({ kind, rule: r, text: r, to: kind })}>
                    <Pencil />
                  </Button>
                  <Button variant="ghost" size="icon-xs" aria-label={t("config.shared.remove", { name: r })} onClick={() => propose({ file, op: "remove", path: ["permissions", kind], value: r })}>
                    <X />
                  </Button>
                  {help[kind + "\0" + r] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help[kind + "\0" + r]}</p>}
                </li>
                </ContextMenuTrigger>
                <ContextMenuContent className="w-64">
                  <ContextMenuItem onSelect={() => setEdit({ kind, rule: r, text: r, to: kind })}>
                    <Pencil /> {t("config.perms.edit")}
                  </ContextMenuItem>
                  <ContextMenuItem onSelect={() => navigator.clipboard.writeText(r).then(() => toast.success(t("config.shared.ruleCopied")), () => toast.error(t("config.shared.copyFailed")))}>
                    <ClipboardCopy /> {t("config.perms.copyRule")}
                  </ContextMenuItem>
                  {kind !== DIRS && (
                  <ContextMenuSub>
                    <ContextMenuSubTrigger>{t("config.perms.changeKind")}</ContextMenuSubTrigger>
                    <ContextMenuSubContent>
                      {RULE_KINDS.filter((k) => k !== kind).map((k) => (
                        <ContextMenuItem
                          key={k}
                          onSelect={() => moveRule(file, kind, r, k, r)}
                        >
                          {t(`config.perms.kind.${k}`)}
                        </ContextMenuItem>
                      ))}
                    </ContextMenuSubContent>
                  </ContextMenuSub>
                  )}
                  {file !== "u" && kind !== DIRS && (
                    <>
                      <ContextMenuSeparator />
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, r, false)}>
                        <Globe /> {t("config.perms.copyGlobal")}
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, r, true)}>
                        <Globe /> {t("config.perms.moveGlobal")}
                      </ContextMenuItem>
                    </>
                  )}
                  <ContextMenuSeparator />
                  <ContextMenuItem variant="destructive" onSelect={() => propose({ file, op: "remove", path: ["permissions", kind], value: r })}>
                    <X /> {t("common.delete")}
                  </ContextMenuItem>
                </ContextMenuContent>
                </ContextMenu>
                )
              )}
              {rules.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">{t("config.perms.none")}</li>}
            </ul>
            <form
              className="mt-1 flex gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                if (draft[kind]?.trim()) add(kind, draft[kind]).then(() => setDraft({ ...draft, [kind]: "" }))
              }}
            >
              <Input value={draft[kind] ?? ""} onChange={(e) => setDraft({ ...draft, [kind]: e.target.value })} placeholder={kind === DIRS ? t("config.perms.dirPlaceholder") : "Bash(git status:*)"} className="h-8 font-mono text-xs" aria-label={t("config.perms.newRule", { label })} />
              {kind === DIRS && (
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  onClick={async () => {
                    const p = await a.pickDir(t("config.perms.pickDir"), draft[kind] || home)
                    if (p) add(kind, p)
                  }}
                >
                  <FolderOpen /> {t("config.perms.browse")}
                </Button>
              )}
              <Button size="sm" variant="outline" type="submit">
                <Plus /> {t("config.perms.add")}
              </Button>
            </form>
          </section>
        )
      })}
      <section>
        <h3 className="text-sm font-medium">
          {t("config.perms.suggestedTitle")} <span className="text-xs font-normal text-muted-foreground">· {t("config.perms.suggestedHint")}</span>
        </h3>
        <ul className="mt-1 space-y-px">
          {(sug ?? []).map((s) => (
            <li key={s.rule} className="flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="min-w-0 flex-1 text-xs break-all" title={s.example}>
                {s.rule}
              </code>
              <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.explain", { rule: s.rule })} title={t("config.perms.explainTitle")} onClick={() => explain("allow", s.rule)}>
                <Sparkles />
              </Button>
              <span className="shrink-0 text-xs text-muted-foreground">{t("config.perms.times", { count: s.count })}</span>
              <span className={cn("shrink-0 text-xs", s.readOnly ? "text-success" : "text-warning")}>{s.readOnly ? t("config.perms.readOnly") : t("config.perms.toCheck")}</span>
              <Button size="xs" variant="outline" onClick={() => add("allow", s.rule)}>
                {t("config.perms.allowBtn")}
              </Button>
              {help["allow\0" + s.rule] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help["allow\0" + s.rule]}</p>}
            </li>
          ))}
          {sug?.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">{t("config.perms.nothingToSuggest")}</li>}
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
const LANGUAGES: Opt[] = ["French", "English", "Spanish", "German", "Italian", "Portuguese", "Dutch", "Japanese", "Chinese", "Korean"].map((l) => [l, l] as const)

const fieldGroups = (t: TFunction): { title: string; fields: Field[] }[] => {
  const o = (group: string, items: [string, string][]): Opt[] => items.map(([v, id]) => [v, t(`config.settings.${group}.${id}`)] as const)
  const f = (key: string, kind: Field["kind"], options?: Opt[], path?: string[]): Field => {
    const id = path ? path[path.length - 1] : key
    return { key, path, label: t(`config.settings.field.${id}`), kind, options }
  }
  return [
    {
      title: t("config.settings.group.model"),
      fields: [
        f("model", "enum", o("model", [["fable", "fable"], ["opus", "opus"], ["sonnet", "sonnet"], ["haiku", "haiku"], ["best", "best"], ["opusplan", "opusplan"], ["opus[1m]", "opus1m"], ["sonnet[1m]", "sonnet1m"]])),
        f("effortLevel", "enum", [["low", t("config.settings.effort.low")], ["medium", "medium"], ["high", "high"], ["xhigh", "xhigh"], ["max", t("config.settings.effort.max")]]),
        f("language", "enum", LANGUAGES),
        f("autoCompactEnabled", "bool"),
        f("idleCompaction", "bool"),
        f("fileCheckpointingEnabled", "bool"),
        f("switchModelsOnFlag", "bool"),
        f("cleanupPeriodDays", "number"),
      ],
    },
    {
      title: t("config.settings.group.ui"),
      fields: [
        f("theme", "enum", o("theme", [["auto", "auto"], ["dark", "dark"], ["light", "light"], ["dark-daltonized", "darkDaltonized"], ["light-daltonized", "lightDaltonized"], ["dark-ansi", "darkAnsi"], ["light-ansi", "lightAnsi"]])),
        f("editorMode", "enum", [["normal", "normal"], ["vim", "vim"]]),
        f("promptSuggestionEnabled", "bool"),
        f("awaySummaryEnabled", "bool"),
        f("preferredNotifChannel", "enum", [["auto", "auto"], ["terminal_bell", t("config.settings.notif.terminalBell")], ["iterm2", "iTerm2"], ["iterm2_with_bell", t("config.settings.notif.iterm2WithBell")], ["kitty", "kitty"], ["ghostty", "Ghostty"], ["notifications_disabled", t("config.settings.notif.disabled")]]),
        f("inputNeededNotifEnabled", "bool"),
        f("agentPushNotifEnabled", "bool"),
        f("teammateMode", "enum", [["auto", "auto"], ["in-process", t("config.settings.teammate.inProcess")], ["tmux", t("config.settings.teammate.tmux")]]),
        f("feedbackDrafts", "enum", o("drafts", [["notify", "notify"], ["quiet", "quiet"], ["off", "off"]])),
      ],
    },
    {
      title: t("config.settings.group.security"),
      fields: [
        f("permissions.defaultMode", "enum", o("mode", [["default", "default"], ["acceptEdits", "acceptEdits"], ["plan", "plan"], ["auto", "auto"], ["bypassPermissions", "bypassPermissions"]]), ["permissions", "defaultMode"]),
        f("crossSessionInbound", "enum", o("inbound", [["accept", "accept"], ["hold", "hold"], ["refuse", "refuse"]])),
        f("dialogExpiry", "enum", o("expiry", [["1m", "1m"], ["5m", "5m"], ["10m", "10m"], ["30m", "30m"], ["1h", "1h"]])),
        f("skipDangerousModePermissionPrompt", "bool"),
        f("skipWorkflowUsageWarning", "bool"),
        f("workflowKeywordTriggerEnabled", "bool"),
        f("remoteControlAtStartup", "bool"),
        f("useAutoModeDuringPlan", "bool"),
      ],
    },
  ]
}

function Row({ f, value, file, propose }: { f: Field; value: unknown; file: string; propose: Propose }) {
  const { t } = useTranslation()
  const [text, setText] = useState(value === undefined ? "" : String(value))
  const path = f.path ?? [f.key]
  const set = (v: unknown) => propose({ file, op: "set", path, value: v })
  const current = value === undefined ? "" : String(value)
  // une valeur déjà là mais inconnue d'ici reste proposée, pour qu'elle s'affiche et ne soit pas perdue
  const options: Opt[] = f.kind === "enum" ? [["", t("config.settings.default")], ...(f.options ?? []), ...(current && !f.options?.some((o) => o[0] === current) ? [[current, t("config.settings.currentValue", { value: current })] as const] : [])] : []
  return (
    <li className="flex flex-wrap items-center gap-2 py-1 text-sm">
      <span className="min-w-56 flex-1">
        {f.label}
        {describeKey(f.key) && <span className="block text-2xs leading-snug text-muted-foreground">{describeKey(f.key)}</span>}
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
            placeholder={t("config.settings.default")}
            className="h-8 w-56 text-xs"
            aria-label={f.label}
          />
          <Button size="xs" variant="outline" disabled={!text.trim() || text === current} onClick={() => set(Number(text))}>
            {t("common.apply")}
          </Button>
        </>
      )}
      {value !== undefined && f.kind !== "enum" && (
        <Button size="icon-xs" variant="ghost" title={t("config.settings.reset")} aria-label={t("config.shared.remove", { name: f.label })} onClick={() => propose({ file, op: "delete", path })}>
          <RotateCcw />
        </Button>
      )}
    </li>
  )
}

export function SettingsTab({ file, doc, plugins, propose }: { file: string; doc: Doc; plugins: CfgOverview["plugins"]; propose: Propose }) {
  const { t } = useTranslation()
  const [name, setName] = useState("")
  const [val, setVal] = useState("")
  const env = (at(doc, "env") ?? {}) as Record<string, string>
  const enabled = (at(doc, "enabledPlugins") ?? {}) as Record<string, boolean>
  return (
    <div className="space-y-6">
      {fieldGroups(t).map((g) => (
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
          {t("config.settings.envTitle")} <span className="text-xs font-normal text-muted-foreground">· {t("config.settings.envHint")}</span>
        </h3>
        <ul className="mt-1 space-y-px">
          {Object.entries(env).map(([k, v]) => (
            <li key={k} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="text-xs">{k}</code>
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground">= {String(v)}</code>
              <Button variant="ghost" size="icon-xs" aria-label={t("config.shared.remove", { name: k })} onClick={() => propose({ file, op: "delete", path: ["env", k] })}>
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
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("config.settings.envName")} className="h-8 w-48 font-mono text-xs" aria-label={t("config.settings.envNameAria")} />
          <Input value={val} onChange={(e) => setVal(e.target.value)} placeholder={t("config.settings.envValue")} className="h-8 flex-1 font-mono text-xs" aria-label={t("config.settings.envValueAria")} />
          <Button size="sm" variant="outline" type="submit">
            <Plus /> {t("config.perms.add")}
          </Button>
        </form>
      </section>
      {file === "u" && plugins.length > 0 && (
        <section>
          <h3 className="text-sm font-medium">{t("config.settings.plugins")}</h3>
          <ul className="mt-1 grid gap-px sm:grid-cols-2">
            {plugins.map((p) => (
              <li key={p.id} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
                <Checkbox checked={enabled[p.id] === true} onCheckedChange={(c) => propose({ file, op: "set", path: ["enabledPlugins", p.id], value: c === true })} aria-label={p.id} />
                <span className="min-w-0 flex-1 truncate" title={p.id}>
                  {p.id}
                </span>
                {!p.installed && <span className="shrink-0 text-xs text-warning">{t("config.settings.notInstalled")}</span>}
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
  const { t } = useTranslation()
  const restore = async (b: BackupInfo) => {
    const r = await api.post("/config-restore", { name: b.name })
    reportResult(r)
    if (r.ok) onRestored()
  }
  if (!backups.length) return <p className="py-8 text-sm text-muted-foreground">{t("config.backups.none")}</p>
  return (
    <ul className="space-y-px">
      {backups.map((b) => (
        <li key={b.name} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted">
          <span className="w-44 shrink-0 text-xs text-muted-foreground">
            {b.at.slice(0, 4)}-{b.at.slice(4, 6)}-{b.at.slice(6, 8)} {b.at.slice(9, 11)}:{b.at.slice(11, 13)}:{b.at.slice(13, 15)}
          </span>
          <span className="min-w-0 flex-1 truncate">{label(b.file)}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{t("config.backups.size", { kb: b.kb })}</span>
          <Button size="xs" variant="outline" onClick={() => restore(b)}>
            {t("config.backups.restore")}
          </Button>
        </li>
      ))}
    </ul>
  )
}
