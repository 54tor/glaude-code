import { loc } from "@/i18n"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { Check, Copy, FolderOpen, Globe, Pencil, Plus, RotateCcw, Search, Sparkles, Trash2, X } from "lucide-react"
import type { BackupInfo, CfgEdit, CfgOverview, PermSuggest } from "@/lib/api"
import { api } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { cn, copyText } from "@/lib/utils"
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
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"

type Doc = Record<string, unknown> | undefined
type Propose = (e: CfgEdit) => Promise<void>

const at = (doc: Doc, ...path: string[]): unknown =>
  path.reduce<unknown>((current, segment) => {
    if (current && typeof current === "object") return (current as Record<string, unknown>)[segment]
    return undefined
  }, doc)
const list = (value: unknown): string[] => {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === "string")
}
const helpKey = (kind: string, rule: string) => kind + "\0" + rule
// Horodatage des sauvegardes au format AAAAMMJJ-HHMMSS.
const BACKUP_TIMESTAMP_PARTS = { year: [0, 4], month: [4, 6], day: [6, 8], hour: [9, 11], minute: [11, 13], second: [13, 15] } as const
const backupTimestampPart = (timestamp: string, part: keyof typeof BACKUP_TIMESTAMP_PARTS) => timestamp.slice(...BACKUP_TIMESTAMP_PARTS[part])

// --- permissions ---------------------------------------------------------------------------------------------

const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const
const RULE_KINDS = KINDS.slice(0, 3) // catégories entre lesquelles une règle de permission peut passer
const DIRS = "additionalDirectories"
// Affichage trié (le fichier garde son ordre) : par outil (Bash, Edit, Read…), puis alphabétique, sans tenir compte de la casse.
export const byRule = (leftRule: string, rightRule: string) => leftRule.localeCompare(rightRule, loc(), { sensitivity: "base", numeric: true })

export function PermsTab({ file, label, doc, propose, proposePerms, gone, onTransfer, added, moveRule }: { added: (file: string, kind: string) => string[]; moveRule: (file: string, from: string, rule: string, to: string, text: string) => Promise<void>; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void; gone: (file: string, kind: string, rule: string) => boolean; file: string; label: string; doc: Doc; propose: Propose; proposePerms: (file: string, text: string, mode: string, then?: () => void, now?: boolean) => Promise<string | null> }) {
  const { t } = useTranslation()
  const actions = useActions()
  const home = actions.home
  const [importing, setImporting] = useState(false)
  const [raw, setRaw] = useState(takeConfigRaw) // ouvert d'emblée depuis le bouton Permissions d'un dossier
  // Explication de Haiku par règle (clé : catégorie + règle) ; « … » pendant la demande.
  const [help, setHelp] = useState<Record<string, string>>({})
  const explain = async (kind: string, rule: string) => {
    const key = helpKey(kind, rule)
    if (help[key]) {
      // deuxième clic : replie
      setHelp((previousHelp) => Object.fromEntries(Object.entries(previousHelp).filter(([existingKey]) => existingKey !== key)))
      return
    }
    setHelp((previousHelp) => ({ ...previousHelp, [key]: "…" }))
    let text: string
    try {
      text = (await api.configPermExplain(kind, rule)).out
    } catch (error) {
      text = t("config.perms.explainFailed", { out: errMsg(error) })
    }
    setHelp((previousHelp) => ({ ...previousHelp, [key]: text }))
  }
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [suggestions, setSuggestions] = useState<PermSuggest[] | null>(null)
  useEffect(() => {
    let isMounted = true
    api.configPermSuggest().then((response) => isMounted && setSuggestions(response.suggestions))
    return () => {
      isMounted = false
    }
  }, [doc])
  const [edit, setEdit] = useState<{ kind: string; rule: string; text: string; to: string } | null>(null)
  // Modifier une règle : texte et catégorie. La nouvelle est ajoutée avant que l'ancienne soit retirée, pour ne jamais tout perdre.
  const saveEdit = async () => {
    if (!edit) return
    const text = edit.text.trim()
    const same = text === edit.rule && edit.to === edit.kind
    if (!text || same) {
      setEdit(null)
      return
    }
    setEdit(null)
    await moveRule(file, edit.kind, edit.rule, edit.to, text)
  }
  const add = (kind: string, rule: string) => propose({ file, op: "add", path: ["permissions", kind], value: rule })
  // Vide les quatre listes : l'aperçu montre l'écart et la sauvegarde précède l'écriture, comme pour un import.
  const reset = async () => {
    const proposeError = await proposePerms(file, JSON.stringify({ allow: [], ask: [], deny: [], additionalDirectories: [] }), "replace")
    if (proposeError) toast.error(proposeError)
  }
  const copyRule = (rule: string) =>
    copyText(rule).then(
      () => toast.success(t("config.shared.ruleCopied")),
      () => toast.error(t("config.shared.copyFailed")),
    )
  return (
    <div className="space-y-5">
      <PermIOButtons file={file} label={label} doc={doc} onImport={() => setImporting(true)} onRaw={() => setRaw(true)} onReset={reset} />
      <PermRawDialog open={raw} file={file} label={label} doc={doc} propose={proposePerms} onClose={() => setRaw(false)} />
      <PermImportDialog open={importing} file={file} label={label} propose={proposePerms} onClose={() => setImporting(false)} />
      {KINDS.map((kind) => {
        const label = t(`config.perms.kind.${kind}`)
        const hint = t(`config.perms.hint.${kind}`)
        const candidateRules = [...list(at(doc, "permissions", kind)), ...added(file, kind)]
        const rules = candidateRules
          .filter((candidateRule, ruleIndex, allRules) => allRules.indexOf(candidateRule) === ruleIndex && !gone(file, kind, candidateRule))
          .sort(byRule)
        return (
          <section key={kind}>
            <h3 className="text-sm font-medium">
              {label} <span className="text-xs font-normal text-muted-foreground">· {hint}</span>
            </h3>
            <ul className="mt-1 space-y-px">
              {rules.map((rule) =>
                edit?.kind === kind && edit.rule === rule ? (
                  <li key={rule}>
                    <form
                      className="flex items-center gap-2 px-2 py-1"
                      onSubmit={(submitEvent) => {
                        submitEvent.preventDefault()
                        saveEdit()
                      }}
                    >
                      <Input value={edit.text} onChange={(changeEvent) => setEdit({ ...edit, text: changeEvent.target.value })} onKeyDown={(keyEvent) => keyEvent.key === "Escape" && setEdit(null)} autoFocus className="h-7 flex-1 font-mono text-xs" aria-label={t("config.perms.editRule", { rule })} />
                      {kind !== DIRS && <Choose value={edit.to} onChange={(to) => setEdit({ ...edit, to })} options={RULE_KINDS.map((ruleKind) => [ruleKind, t(`config.perms.kind.${ruleKind}`)] as const)} label={t("config.perms.category")} size="sm" className="w-36" />}
                      <Button size="icon-sm" variant="ghost" type="submit" aria-label={t("config.perms.saveRule")}>
                        <Check />
                      </Button>
                      <Button size="icon-sm" variant="ghost" type="button" aria-label={t("common.cancel")} onClick={() => setEdit(null)}>
                        <X />
                      </Button>
                    </form>
                  </li>
                ) : (
                <ContextMenu key={rule}>
                <ContextMenuTrigger asChild>
                <li className="group flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted" onDoubleClick={() => setEdit({ kind, rule, text: rule, to: kind })}>
                  <code className="min-w-0 flex-1 text-xs break-all">{rule}</code>
                  {kind !== DIRS && (
                    <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.explain", { rule })} title={t("config.perms.explainTitle")} onClick={() => explain(kind, rule)}>
                      <Sparkles />
                    </Button>
                  )}
                  <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.editRule", { rule })} title={t("config.perms.editTitle")} onClick={() => setEdit({ kind, rule, text: rule, to: kind })}>
                    <Pencil />
                  </Button>
                  <Button variant="ghost" size="icon-xs" aria-label={t("config.shared.remove", { name: rule })} onClick={() => propose({ file, op: "remove", path: ["permissions", kind], value: rule })}>
                    <X />
                  </Button>
                  {help[helpKey(kind, rule)] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help[helpKey(kind, rule)]}</p>}
                </li>
                </ContextMenuTrigger>
                <ContextMenuContent className="w-64">
                  <ContextMenuItem onSelect={() => copyRule(rule)}>
                    <Copy /> {t("config.perms.copyRule")}
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem onSelect={() => setEdit({ kind, rule, text: rule, to: kind })}>
                    <Pencil /> {t("config.perms.edit")}
                  </ContextMenuItem>
                  {kind !== DIRS && (
                  <ContextMenuSub>
                    <ContextMenuSubTrigger>{t("config.perms.changeKind")}</ContextMenuSubTrigger>
                    <ContextMenuSubContent>
                      {RULE_KINDS.filter((ruleKind) => ruleKind !== kind).map((ruleKind) => (
                        <ContextMenuItem
                          key={ruleKind}
                          onSelect={() => moveRule(file, kind, rule, ruleKind, rule)}
                        >
                          {t(`config.perms.kind.${ruleKind}`)}
                        </ContextMenuItem>
                      ))}
                    </ContextMenuSubContent>
                  </ContextMenuSub>
                  )}
                  {file !== "u" && kind !== DIRS && (
                    <>
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, rule, false)}>
                        <Globe /> {t("config.perms.copyGlobal")}
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => onTransfer(file, kind, rule, true)}>
                        <Globe /> {t("config.perms.moveGlobal")}
                      </ContextMenuItem>
                    </>
                  )}
                  <ContextMenuSeparator />
                  <ContextMenuItem variant="destructive" onSelect={() => propose({ file, op: "remove", path: ["permissions", kind], value: rule })}>
                    <Trash2 /> {t("common.delete")}
                  </ContextMenuItem>
                </ContextMenuContent>
                </ContextMenu>
                )
              )}
              {rules.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">{t("config.perms.none")}</li>}
            </ul>
            <form
              className="mt-1 flex gap-2"
              onSubmit={(submitEvent) => {
                submitEvent.preventDefault()
                if (!draft[kind]?.trim()) return
                add(kind, draft[kind]).then(() => setDraft({ ...draft, [kind]: "" }))
              }}
            >
              <Input value={draft[kind] ?? ""} onChange={(changeEvent) => setDraft({ ...draft, [kind]: changeEvent.target.value })} placeholder={kind === DIRS ? t("config.perms.dirPlaceholder") : "Bash(git status:*)"} className="h-7 font-mono text-xs" aria-label={t("config.perms.newRule", { label })} />
              {kind === DIRS && (
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  onClick={async () => {
                    const pickedDir = await actions.pickDir(t("config.perms.pickDir"), draft[kind] || home)
                    if (pickedDir) add(kind, pickedDir)
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
          {(suggestions ?? []).map((suggestion) => (
            <li key={suggestion.rule} className="flex flex-wrap items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="min-w-0 flex-1 text-xs break-all" title={suggestion.example}>
                {suggestion.rule}
              </code>
              <Button variant="ghost" size="icon-xs" aria-label={t("config.perms.explain", { rule: suggestion.rule })} title={t("config.perms.explainTitle")} onClick={() => explain("allow", suggestion.rule)}>
                <Sparkles />
              </Button>
              <span className="shrink-0 text-xs text-muted-foreground">{t("config.perms.times", { count: suggestion.count })}</span>
              <span className={cn("shrink-0 text-xs", suggestion.readOnly ? "text-success" : "text-warning")}>{suggestion.readOnly ? t("config.perms.readOnly") : t("config.perms.toCheck")}</span>
              <Button size="xs" variant="outline" onClick={() => add("allow", suggestion.rule)}>
                {t("config.perms.allowBtn")}
              </Button>
              {help[helpKey("allow", suggestion.rule)] && <p className="basis-full pb-1 text-xs whitespace-pre-wrap text-muted-foreground">{help[helpKey("allow", suggestion.rule)]}</p>}
            </li>
          ))}
          {suggestions?.length === 0 && <li className="px-2 py-1 text-xs text-muted-foreground">{t("config.perms.nothingToSuggest")}</li>}
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
const LANGUAGES: Opt[] = ["French", "English", "Spanish", "German", "Italian", "Portuguese", "Dutch", "Japanese", "Chinese", "Korean"].map((language) => [language, language] as const)

const fieldGroups = (t: TFunction): { title: string; fields: Field[] }[] => {
  const translatedOptions = (group: string, items: [string, string][]): Opt[] =>
    items.map(([optionValue, labelId]) => [optionValue, t(`config.settings.${group}.${labelId}`)] as const)
  const makeField = (key: string, kind: Field["kind"], options?: Opt[], path?: string[]): Field => {
    const id = path ? path[path.length - 1] : key
    return { key, path, label: t(`config.settings.field.${id}`), kind, options }
  }
  return [
    {
      title: t("config.settings.group.model"),
      fields: [
        makeField("model", "enum", translatedOptions("model", [["fable", "fable"], ["opus", "opus"], ["sonnet", "sonnet"], ["haiku", "haiku"], ["best", "best"], ["opusplan", "opusplan"], ["opus[1m]", "opus1m"], ["sonnet[1m]", "sonnet1m"]])),
        makeField("effortLevel", "enum", [["low", t("config.settings.effort.low")], ["medium", "medium"], ["high", "high"], ["xhigh", "xhigh"], ["max", t("config.settings.effort.max")]]),
        makeField("language", "enum", LANGUAGES),
        makeField("autoCompactEnabled", "bool"),
        makeField("idleCompaction", "bool"),
        makeField("fileCheckpointingEnabled", "bool"),
        makeField("switchModelsOnFlag", "bool"),
        makeField("cleanupPeriodDays", "number"),
      ],
    },
    {
      title: t("config.settings.group.ui"),
      fields: [
        makeField("theme", "enum", translatedOptions("theme", [["auto", "auto"], ["dark", "dark"], ["light", "light"], ["dark-daltonized", "darkDaltonized"], ["light-daltonized", "lightDaltonized"], ["dark-ansi", "darkAnsi"], ["light-ansi", "lightAnsi"]])),
        makeField("editorMode", "enum", [["normal", "normal"], ["vim", "vim"]]),
        makeField("promptSuggestionEnabled", "bool"),
        makeField("awaySummaryEnabled", "bool"),
        makeField("preferredNotifChannel", "enum", [["auto", "auto"], ["terminal_bell", t("config.settings.notif.terminalBell")], ["iterm2", "iTerm2"], ["iterm2_with_bell", t("config.settings.notif.iterm2WithBell")], ["kitty", "kitty"], ["ghostty", "Ghostty"], ["notifications_disabled", t("config.settings.notif.disabled")]]),
        makeField("inputNeededNotifEnabled", "bool"),
        makeField("agentPushNotifEnabled", "bool"),
        makeField("teammateMode", "enum", [["auto", "auto"], ["in-process", t("config.settings.teammate.inProcess")], ["tmux", t("config.settings.teammate.tmux")]]),
        makeField("feedbackDrafts", "enum", translatedOptions("drafts", [["notify", "notify"], ["quiet", "quiet"], ["off", "off"]])),
      ],
    },
    {
      title: t("config.settings.group.security"),
      fields: [
        makeField("permissions.defaultMode", "enum", translatedOptions("mode", [["default", "default"], ["acceptEdits", "acceptEdits"], ["plan", "plan"], ["auto", "auto"], ["bypassPermissions", "bypassPermissions"]]), ["permissions", "defaultMode"]),
        makeField("crossSessionInbound", "enum", translatedOptions("inbound", [["accept", "accept"], ["hold", "hold"], ["refuse", "refuse"]])),
        makeField("dialogExpiry", "enum", translatedOptions("expiry", [["1m", "1m"], ["5m", "5m"], ["10m", "10m"], ["30m", "30m"], ["1h", "1h"]])),
        makeField("skipDangerousModePermissionPrompt", "bool"),
        makeField("skipWorkflowUsageWarning", "bool"),
        makeField("workflowKeywordTriggerEnabled", "bool"),
        makeField("remoteControlAtStartup", "bool"),
        makeField("useAutoModeDuringPlan", "bool"),
      ],
    },
  ]
}

function enumOptions(field: Field, current: string, t: TFunction): Opt[] {
  if (field.kind !== "enum") return []
  const isUnknownCurrent = !!current && !field.options?.some(([optionValue]) => optionValue === current)
  // une valeur déjà là mais inconnue d'ici reste proposée, pour qu'elle s'affiche et ne soit pas perdue
  const currentOption: Opt[] = isUnknownCurrent ? [[current, t("config.settings.currentValue", { value: current })] as const] : []
  return [["", t("config.settings.default")], ...(field.options ?? []), ...currentOption]
}

function Row({ field, value, file, propose }: { field: Field; value: unknown; file: string; propose: Propose }) {
  const { t } = useTranslation()
  const [text, setText] = useState(value === undefined ? "" : String(value))
  const path = field.path ?? [field.key]
  const set = (newValue: unknown) => propose({ file, op: "set", path, value: newValue })
  const current = value === undefined ? "" : String(value)
  const options = enumOptions(field, current, t)
  const changeEnum = (selectedValue: string) => {
    if (selectedValue === "") {
      propose({ file, op: "delete", path })
      return
    }
    if (selectedValue !== current) set(selectedValue)
  }
  return (
    <li className="flex flex-wrap items-center gap-2 py-1 text-sm">
      <span className="min-w-56 flex-1">
        {field.label}
        {describeKey(field.key) && <span className="block text-2xs leading-snug text-muted-foreground">{describeKey(field.key)}</span>}
      </span>
      {field.kind === "bool" ? (
        <Checkbox checked={value === true} onCheckedChange={(checked) => set(checked === true)} aria-label={field.label} />
      ) : field.kind === "enum" ? (
        <Choose value={current} onChange={changeEnum} options={options} label={field.label} size="sm" className="w-72" />
      ) : (
        <>
          <Input
            type="number"
            value={text}
            onChange={(changeEvent) => setText(changeEvent.target.value)}
            onKeyDown={(keyEvent) => keyEvent.key === "Enter" && text.trim() && set(Number(text))}
            placeholder={t("config.settings.default")}
            className="h-7 w-56 text-xs"
            aria-label={field.label}
          />
          <Button size="sm" variant="outline" disabled={!text.trim() || text === current} onClick={() => set(Number(text))}>
            {t("common.apply")}
          </Button>
        </>
      )}
      {value !== undefined && field.kind !== "enum" && (
        <Button size="icon-sm" variant="ghost" title={t("config.settings.reset")} aria-label={t("config.shared.remove", { name: field.label })} onClick={() => propose({ file, op: "delete", path })}>
          <RotateCcw />
        </Button>
      )}
    </li>
  )
}

export function SettingsTab({ file, doc, plugins, propose }: { file: string; doc: Doc; plugins: CfgOverview["plugins"]; propose: Propose }) {
  const { t } = useTranslation()
  const [name, setName] = useState("")
  const [envValue, setEnvValue] = useState("")
  const env = (at(doc, "env") ?? {}) as Record<string, string>
  const enabled = (at(doc, "enabledPlugins") ?? {}) as Record<string, boolean>
  const [query, setQuery] = useState("")
  const lowerQuery = query.trim().toLowerCase()
  const hit = (...candidates: string[]) => !lowerQuery || candidates.some((candidate) => candidate.toLowerCase().includes(lowerQuery))
  const groups = fieldGroups(t)
    .map((group) => ({ ...group, fields: group.fields.filter((field) => hit(field.label, field.key, describeKey(field.key) ?? "")) }))
    .filter((group) => group.fields.length)
  const valueAt = (field: Field) => at(doc, ...(field.path ?? [field.key]))
  return (
    <div className="space-y-6">
      <div className="relative">
        <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
        <Input value={query} onChange={(changeEvent) => setQuery(changeEvent.target.value)} placeholder={t("config.settings.search")} aria-label={t("config.settings.search")} className="pl-8" />
      </div>
      {groups.map((group) => (
        <section key={group.title}>
          <h3 className="text-sm font-medium">{group.title}</h3>
          <ul className="mt-1 divide-y">
            {group.fields.map((field) => (
              <Row key={field.key + String(valueAt(field))} field={field} value={valueAt(field)} file={file} propose={propose} />
            ))}
          </ul>
        </section>
      ))}
      <section>
        <h3 className="text-sm font-medium">
          {t("config.settings.envTitle")} <span className="text-xs font-normal text-muted-foreground">· {t("config.settings.envHint")}</span>
        </h3>
        <ul className="mt-1 space-y-px">
          {Object.entries(env).filter(([variableName]) => hit(variableName)).map(([variableName, variableValue]) => (
            <li key={variableName} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
              <code className="text-xs">{variableName}</code>
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground">= {String(variableValue)}</code>
              <Button variant="ghost" size="icon-xs" aria-label={t("config.shared.remove", { name: variableName })} onClick={() => propose({ file, op: "delete", path: ["env", variableName] })}>
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
        <form
          className="mt-1 flex gap-2"
          onSubmit={(submitEvent) => {
            submitEvent.preventDefault()
            if (!name.trim()) return
            propose({ file, op: "set", path: ["env", name.trim()], value: envValue }).then(() => {
              setName("")
              setEnvValue("")
            })
          }}
        >
          <Input value={name} onChange={(changeEvent) => setName(changeEvent.target.value)} placeholder={t("config.settings.envName")} className="h-7 w-48 font-mono text-xs" aria-label={t("config.settings.envNameAria")} />
          <Input value={envValue} onChange={(changeEvent) => setEnvValue(changeEvent.target.value)} placeholder={t("config.settings.envValue")} className="h-7 flex-1 font-mono text-xs" aria-label={t("config.settings.envValueAria")} />
          <Button size="sm" variant="outline" type="submit">
            <Plus /> {t("config.perms.add")}
          </Button>
        </form>
      </section>
      {file === "u" && plugins.length > 0 && (
        <section>
          <h3 className="text-sm font-medium">{t("config.settings.plugins")}</h3>
          <ul className="mt-1 grid gap-px sm:grid-cols-2">
            {plugins.map((plugin) => (
              <li key={plugin.id} className="flex items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted">
                <Checkbox checked={enabled[plugin.id] === true} onCheckedChange={(checked) => propose({ file, op: "set", path: ["enabledPlugins", plugin.id], value: checked === true })} aria-label={plugin.id} />
                <span className="min-w-0 flex-1 truncate" title={plugin.id}>
                  {plugin.id}
                </span>
                {!plugin.installed && <span className="shrink-0 text-xs text-warning">{t("config.settings.notInstalled")}</span>}
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
  const restore = async (backup: BackupInfo) => {
    const restored = await reportAction(() => api.configRestore(backup.name).then((response) => ({ message: response.out })))
    if (restored) onRestored()
  }
  if (!backups.length) return <p className="py-8 text-sm text-muted-foreground">{t("config.backups.none")}</p>
  return (
    <ul className="space-y-px">
      {backups.map((backup) => (
        <li key={backup.name} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted">
          <span className="w-44 shrink-0 text-xs text-muted-foreground">
            {backupTimestampPart(backup.at, "year")}-{backupTimestampPart(backup.at, "month")}-{backupTimestampPart(backup.at, "day")} {backupTimestampPart(backup.at, "hour")}:{backupTimestampPart(backup.at, "minute")}:{backupTimestampPart(backup.at, "second")}
          </span>
          <span className="min-w-0 flex-1 truncate">{label(backup.file)}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{t("config.backups.size", { kb: backup.kb })}</span>
          <Button size="xs" variant="outline" onClick={() => restore(backup)}>
            {t("config.backups.restore")}
          </Button>
        </li>
      ))}
    </ul>
  )
}
