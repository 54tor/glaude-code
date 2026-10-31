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

// Préfixes dont la suite est un nom libre (variable, plugin, place de marché), inséré dans l'explication.
const NAMED_PREFIXES: [prefix: string, catalogKey: string][] = [
  ["env.", "config.keys.envVar"],
  ["enabledPlugins.", "config.keys.plugin"],
  ["pluginConfigs.", "config.keys.pluginConfig"],
  ["extraKnownMarketplaces.", "config.keys.marketplace"],
]
const SECTION_PREFIXES = ["modelSettings", "worktree"]

// describe : l'explication d'un réglage, par son chemin (« permissions.allow[] », « env.MA_VAR », « enabledPlugins.x@y »…).
export function describeKey(path: string): string | undefined {
  if (DOTTED[path]) return doc(DOTTED[path])
  if (PLAIN.has(path)) return doc(path)
  for (const [prefix, catalogKey] of NAMED_PREFIXES) {
    if (path.startsWith(prefix)) return i18n.t(catalogKey, { name: path.slice(prefix.length) })
  }
  for (const section of SECTION_PREFIXES) {
    if (path.startsWith(section + ".")) return doc(section)
  }
  const firstSegment = path.split(".")[0]
  if (PLAIN.has(firstSegment)) return doc(firstSegment)
  return undefined
}
