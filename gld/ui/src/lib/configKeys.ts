// Ce que veulent dire les clés des fichiers de réglages de Claude Code (settings.json), d'après la documentation de Claude Code.
// Les clés récentes ou peu documentées le disent : mieux vaut « peu documenté » qu'une explication inventée.

import { i18n } from "@/i18n"

// Les textes sont dans le catalogue (config.keys.<id>) ; ici, seulement la correspondance chemin -> id.
const PLAIN = new Set([
  "model", "effortLevel", "language", "theme", "editorMode", "cleanupPeriodDays", "env", "attribution", "permissions", "hooks", "worktree",
  "workflowKeywordTriggerEnabled", "enabledPlugins", "extraKnownMarketplaces", "feedbackDrafts", "modelSettings", "promptSuggestionEnabled",
  "awaySummaryEnabled", "dialogExpiry", "pluginConfigs", "skipDangerousModePermissionPrompt", "skipWorkflowUsageWarning", "preferredNotifChannel",
  "autoCompactEnabled", "idleCompaction", "switchModelsOnFlag", "fileCheckpointingEnabled", "teammateMode", "remoteControlAtStartup",
  "crossSessionInbound", "inputNeededNotifEnabled", "agentPushNotifEnabled", "useAutoModeDuringPlan", "autoMode",
])
const DOTTED: Record<string, string> = {
  "attribution.commit": "attributionCommit",
  "attribution.pr": "attributionPr",
  "permissions.allow[]": "permissionsAllow",
  "permissions.ask[]": "permissionsAsk",
  "permissions.deny[]": "permissionsDeny",
  "permissions.defaultMode": "permissionsDefaultMode",
  "autoMode.environment[]": "autoModeEnvironment",
  "autoMode.soft_deny[]": "autoModeSoftDeny",
  "autoMode.allow[]": "autoModeAllow",
}
const doc = (id: string) => i18n.t(`config.keys.${id}`)

// describe : l'explication d'un réglage, par son chemin (« permissions.allow[] », « env.MA_VAR », « enabledPlugins.x@y »…).
export function describeKey(path: string): string | undefined {
  if (DOTTED[path]) return doc(DOTTED[path])
  if (PLAIN.has(path)) return doc(path)
  if (path.startsWith("env.")) return i18n.t("config.keys.envVar", { name: path.slice(4) })
  if (path.startsWith("enabledPlugins.")) return i18n.t("config.keys.plugin", { name: path.slice(15) })
  if (path.startsWith("pluginConfigs.")) return i18n.t("config.keys.pluginConfig", { name: path.slice(14) })
  if (path.startsWith("extraKnownMarketplaces.")) return i18n.t("config.keys.marketplace", { name: path.slice(23) })
  if (path.startsWith("modelSettings.")) return doc("modelSettings")
  if (path.startsWith("worktree.")) return doc("worktree")
  const head = path.split(".")[0]
  return PLAIN.has(head) ? doc(head) : undefined
}
