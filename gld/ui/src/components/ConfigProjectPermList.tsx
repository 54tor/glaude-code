import { Copy, Globe, Trash2, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { ProjectCfg } from "@/lib/api"
import { hooksCoveringRule, overridingKind, skillsUsingRule } from "@/lib/permDependencies"

export const GLOBAL_SCOPE = "u"
const LAYERS_FROM_GENERAL_TO_LOCAL = [GLOBAL_SCOPE, "p", "l"] as const
const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const

type Rule = { rule: string; scope: string }
type RulesByKind = Record<string, Rule[]>

type Props = {
  projectConfig: ProjectCfg
  perms: RulesByKind
  onCopyRule: (rule: string) => void
  onTransfer: (file: string, kind: string, rule: string, move: boolean) => void
  onRemove: (file: string, kind: string, rule: string) => void
}

function OverrideNote({ kind }: { kind?: string }) {
  const { t } = useTranslation()
  if (!kind) return null
  return <span className="shrink-0 text-2xs text-amber-600 dark:text-amber-400">{t("config.projects.overriddenBy", { kind: t(`config.perms.kind.${kind}`).toLowerCase() })}</span>
}

function LinkedNotes({ projectConfig, rule }: { projectConfig: ProjectCfg; rule: string }) {
  const { t } = useTranslation()
  const coveringHooks = hooksCoveringRule(projectConfig.hooks, rule)
  const usingSkills = skillsUsingRule(projectConfig.skillTools ?? {}, rule)
  return (
    <>
      {coveringHooks.length > 0 && (
        <span className="shrink-0 text-2xs text-muted-foreground" title={coveringHooks.map((hook) => `${hook.event} ${hook.matcher}: ${hook.command}`).join("\n")}>
          {t("config.projects.linkedHooks", { count: coveringHooks.length })}
        </span>
      )}
      {usingSkills.length > 0 && (
        <span className="shrink-0 text-2xs text-muted-foreground">{t("config.projects.linkedSkills", { list: usingSkills.map((skillName) => "/" + skillName).join(", ") })}</span>
      )}
    </>
  )
}

function ReadOnlyRule({ projectConfig, perms, kind, rule }: { projectConfig: ProjectCfg; perms: RulesByKind; kind: string; rule: string }) {
  const { t } = useTranslation()
  return (
    <li className="flex items-center gap-2 text-muted-foreground">
      <code className="min-w-0 flex-1 text-xs break-all">{rule}</code>
      <OverrideNote kind={overridingKind(perms, kind, rule)} />
      <LinkedNotes projectConfig={projectConfig} rule={rule} />
      <span className="shrink-0 text-2xs" title={t("config.projects.globalReadOnly")}>
        {t("config.scope.global")}
      </span>
    </li>
  )
}

function EditableRule({ projectConfig, perms, kind, permRule, onCopyRule, onTransfer, onRemove }: Props & { kind: string; permRule: Rule }) {
  const { t } = useTranslation()
  const file = `${permRule.scope}/${projectConfig.dir}`
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <li className="flex items-center gap-2">
          <code className="min-w-0 flex-1 text-xs break-all">{permRule.rule}</code>
          <OverrideNote kind={overridingKind(perms, kind, permRule.rule)} />
          <LinkedNotes projectConfig={projectConfig} rule={permRule.rule} />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="xs" variant="ghost">
                <Globe /> {t("config.projects.toGlobal")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => onTransfer(file, kind, permRule.rule, false)}>{t("config.projects.copyToUser")}</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onTransfer(file, kind, permRule.rule, true)}>{t("config.projects.moveFromProject")}</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button size="icon-xs" variant="ghost" aria-label={t("config.projects.deleteRule", { rule: permRule.rule })} title={t("config.projects.deleteRuleTitle")} onClick={() => onRemove(file, kind, permRule.rule)}>
            <X />
          </Button>
        </li>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-60">
        <ContextMenuItem onSelect={() => onCopyRule(permRule.rule)}>
          <Copy /> {t("config.perms.copyRule")}
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => onTransfer(file, kind, permRule.rule, false)}>
          <Globe /> {t("config.perms.copyGlobal")}
        </ContextMenuItem>
        <ContextMenuItem onSelect={() => onTransfer(file, kind, permRule.rule, true)}>
          <Globe /> {t("config.perms.moveGlobal")}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem variant="destructive" onSelect={() => onRemove(file, kind, permRule.rule)}>
          <Trash2 /> {t("common.delete")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

export function ProjectPermList(props: Props) {
  const { t } = useTranslation()
  const { perms, projectConfig } = props
  return (
    <>
      {LAYERS_FROM_GENERAL_TO_LOCAL.map((layer) => {
        const kindsOfLayer = KINDS.filter((kind) => (perms[kind] ?? []).some((permRule) => permRule.scope === layer))
        if (kindsOfLayer.length === 0) return null
        return (
          <div key={layer} className="mb-3" data-layer={layer}>
            <h4 className="mb-1 text-xs font-medium">{t(`config.projects.layer.${layer}`)}</h4>
            {kindsOfLayer.map((kind) => (
              <div key={kind} className="mb-2 pl-2">
                <p className="text-xs text-muted-foreground">{t(`config.perms.kind.${kind}`)}</p>
                <ul className="space-y-px">
                  {perms[kind]
                    .filter((permRule) => permRule.scope === layer)
                    .map((permRule, ruleIndex) =>
                      layer === GLOBAL_SCOPE ? (
                        <ReadOnlyRule key={kind + ruleIndex} projectConfig={projectConfig} perms={perms} kind={kind} rule={permRule.rule} />
                      ) : (
                        <EditableRule key={kind + ruleIndex} {...props} kind={kind} permRule={permRule} />
                      ),
                    )}
                </ul>
              </div>
            ))}
          </div>
        )
      })}
    </>
  )
}
