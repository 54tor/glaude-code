// Ce que veulent dire les clés des fichiers de réglages de Claude Code (settings.json), d'après la documentation de Claude Code.
// Les clés récentes ou peu documentées le disent : mieux vaut « peu documenté » qu'une explication inventée.

const KEYS: Record<string, string> = {
  model: "Modèle utilisé par défaut (alias comme opus, sonnet, haiku, ou identifiant complet).",
  effortLevel: "Effort de raisonnement par défaut (low, medium, high, xhigh, max). Surchargé par --effort ou CLAUDE_CODE_EFFORT_LEVEL.",
  language: "Langue des réponses de Claude.",
  theme: "Thème de couleurs de l'interface (dark, light, variantes daltonien ou ANSI…).",
  editorMode: "Raccourcis de la zone de saisie : normal ou vim.",
  cleanupPeriodDays: "Jours de conservation des transcripts de session avant nettoyage. Une valeur énorme revient à ne jamais les supprimer.",
  env: "Variables d'environnement appliquées à chaque session et à ses sous-processus ; elles écrasent celles du shell, sauf si le lancement les définit déjà.",
  attribution: "Ce que Claude ajoute aux commits (commit) et aux pull requests (pr), et si le lien de session est ajouté (sessionUrl). Vide : rien n'est ajouté.",
  "attribution.commit": "Texte ajouté en fin de message de commit. Vide : aucune ligne d'attribution.",
  "attribution.pr": "Texte ajouté à la description d'une pull request.",
  permissions: "Règles d'autorisation des outils.",
  "permissions.allow[]": "Règle autorisée : l'outil est lancé sans demander. Ex. Bash(git status:*) autorise toutes les formes de « git status ».",
  "permissions.ask[]": "Règle à confirmer : demande toujours, même si une règle plus large autorise.",
  "permissions.deny[]": "Règle refusée : jamais lancée ; l'emporte sur allow et ask.",
  "permissions.defaultMode": "Mode de permissions au démarrage (default, acceptEdits, plan, auto, bypassPermissions).",
  hooks: "Commandes lancées par Claude Code à certains événements (PreToolUse, PostToolUse, Stop, SessionStart, InstructionsLoaded…). Elles s'exécutent avec vos droits.",
  worktree: "Réglages des worktrees git créés par Claude (peu documenté).",
  workflowKeywordTriggerEnabled: "Autorise le lancement d'un workflow multi-agents par un mot-clé dans le prompt.",
  enabledPlugins: "Plugins activés ou non, au format nom@place-de-marché : true ou false.",
  extraKnownMarketplaces: "Places de marché de plugins ajoutées en plus de celles par défaut.",
  feedbackDrafts: "Brouillons de retour que Claude peut mettre en file : notify, quiet ou off.",
  modelSettings: "Réglages propres à certains modèles (peu documenté).",
  promptSuggestionEnabled: "Suggestions de prompt grisées dans la zone de saisie (activées par défaut).",
  awaySummaryEnabled: "Récapitulatif d'une ligne de la session quand vous revenez au terminal.",
  dialogExpiry: "Délai (ex. 5m) des dialogues transmis à un client distant et des approbations de messages entre sessions, avant l'action par défaut.",
  pluginConfigs: "Options non sensibles des plugins, par identifiant de plugin.",
  skipDangerousModePermissionPrompt: "Ne pas afficher l'avertissement avant le mode sans permissions (bypassPermissions).",
  skipWorkflowUsageWarning: "Ne pas afficher l'avertissement sur la consommation des workflows.",
  preferredNotifChannel: "Canal préféré des notifications (auto, sonnerie du terminal…).",
  autoCompactEnabled: "Compacte automatiquement la conversation quand le contexte approche de sa limite.",
  idleCompaction: "Compacte la conversation d'une session inactive avant l'expiration du cache de prompt.",
  switchModelsOnFlag: "Changement de modèle sur signalement (peu documenté).",
  fileCheckpointingEnabled: "Sauvegarde les fichiers avant chaque modification de Claude : c'est ce qui permet /rewind. Désactivé : plus de retour en arrière.",
  teammateMode: "Où tournent les agents coéquipiers : auto, in-process (dans le terminal principal) ou tmux (volets séparés).",
  remoteControlAtStartup: "Active le contrôle à distance de la session au démarrage (peu documenté).",
  crossSessionInbound: "Que faire des messages envoyés par une autre session : accept, hold (en attente de votre accord) ou refuse.",
  inputNeededNotifEnabled: "Notification quand Claude attend une saisie de votre part (peu documenté).",
  agentPushNotifEnabled: "Notifications push des agents (peu documenté).",
  useAutoModeDuringPlan: "Garde le classificateur du mode auto actif pendant le mode plan. Désactivé : toute commande hors lecture seule demande confirmation.",
  autoMode: "Règles du classificateur du mode auto : le contexte du dépôt qu'il connaît (environment) et ce qu'il doit refuser (soft_deny).",
  "autoMode.environment[]": "Un fait sur votre environnement (dépôt, secrets, branches protégées, réseau) que le classificateur du mode auto prend en compte.",
  "autoMode.soft_deny[]": "Une action que le mode auto doit refuser ; $defaults reprend les refus intégrés.",
  "autoMode.allow[]": "Une action que le mode auto peut accepter sans demander.",
}

// describe : l'explication d'un réglage, par son chemin (« permissions.allow[] », « env.MA_VAR », « enabledPlugins.x@y »…).
export function describeKey(path: string): string | undefined {
  if (KEYS[path]) return KEYS[path]
  if (path.startsWith("env.")) return "Variable d'environnement " + path.slice(4) + " : valeur donnée à chaque session."
  if (path.startsWith("enabledPlugins.")) return "Plugin " + path.slice(15) + " : activé (true) ou désactivé (false)."
  if (path.startsWith("pluginConfigs.")) return "Options du plugin " + path.slice(14) + "."
  if (path.startsWith("extraKnownMarketplaces.")) return "Place de marché de plugins : " + path.slice(23) + "."
  if (path.startsWith("modelSettings.")) return KEYS.modelSettings
  if (path.startsWith("worktree.")) return KEYS.worktree
  return KEYS[path.split(".")[0]]
}
