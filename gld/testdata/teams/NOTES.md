# Spike teams Claude Code (claude 2.1.311), constats

Lancé : `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 claude --teammate-mode tmux --session-id <uuid>` dans un tmux détaché, dossier git jetable. Chemins anonymisés (`/home/USER/proj`).

## Réponses

- `--session-id` accepté : OUI. uuid `9d087000-13eb-4519-9823-771e3a943e4c` donne `~/.claude/teams/session-9d087000/` (config.json : `"name": "session-9d087000"`, `"leadSessionId"` = l'uuid complet). Nom de team = `session-` + 8 premiers caractères : confirmé.
- Champ du pane : `members[].tmuxPaneId` (ex. `"%67"`, `"%68"`). Le chef a `"tmuxPaneId": "leader"` et `"backendType": "in-process"` (pas un id tmux : le pane du chef n'est pas dans config.json). Les coéquipiers : `"backendType": "tmux"`. Ids tmux réels confirmés par `tmux list-panes -a` : %66 chef, %67 ada, %68 hugo, %70 zoe. Les coéquipiers sont des panes splittés dans la fenêtre du chef.
- Autres champs de config.json : `name, createdAt, leadAgentId ("team-lead@<team>"), leadSessionId, leadProcess{pid,pidSpace,claimedAt,procStart}, members[]` ; membre : `agentId ("ada@<team>"), name, color, joinedAt, subscriptions, model, prompt, planModeRequired, cwd, backendType, isActive`. Le chef n'a ni `model` ni `prompt` ni `color`.
- Boîtes : `inboxes/<name>.json`, tableau JSON. Un fichier par membre, chef compris (`team-lead.json`). Message : `from, text, summary?, timestamp, color?, msgV (1), msg_id, type ("message"), read (bool)`. Les notifications d'inactivité sont un message dont `text` est un JSON sérialisé `{"type":"idle_notification","idleReason":"available",...}`. Au repos les boîtes valent `[]` : les messages lus sont retirés, donc `inbox-*.json` ici sont des instantanés pris en sondant (fichiers vus avec `read:false`).
- `~/.claude/tasks/session-9d087000/` créé vide. NON CONFIRMÉ : format des tâches. Le chef a répondu que TaskCreate/TaskUpdate/TaskList ne sont pas dans ses outils de cette session (2.1.311, teams expérimentales) ; aucune tâche n'a pu être créée. `tasks/` ne contient que `.gitkeep`. Le lecteur (tâche 4) doit donc tolérer un dossier vide.
- Teams persistantes : une team ancienne `session-28535867` (22 août) existe dans `~/.claude/teams` : les teams ne disparaissent pas forcément avec la session. Non confirmé.
- Mode des coéquipiers : en mode auto du chef, aucun prompt de permission (écriture `/tmp/...` par ada : aucun prompt). En mode manuel (shift+tab jusqu'à « manual mode on »), écriture hors projet par zoe : aucun prompt non plus (non confirmé pourquoi).
- Texte de permission d'un coéquipier affiché dans le pane du chef : OUI, capturé (mode manuel du chef, outil WebFetch lancé par le coéquipier yan). Fichier `permission-prompt-lead-pane.txt` : titre `Fetch · from the yan agent` (format `<Outil> · from the <nom> agent`), puis `Claude wants to fetch content from example.com`, `url: ...`, question `Do you want to allow Claude to fetch this content?`, options `1. Yes` / `2. Yes, and don't ask again for example.com` / `3. No, and tell Claude what to do differently (esc)`. Réponse : touche `1`. Bas du pane avant la demande : `Approve tool calls from your phone`. Ce coéquipier n'apparaît pas dans config.json au moment de la mesure (membres : team-lead, ada, hugo, zoe) : yan semblait in-process. Non confirmé.
- Demande de permission propre au chef (comparaison) : `Bash command` / commande / `Do you want to proceed?` / `1. Yes` ... `4. No`, sans mention « from the X agent ».
- Mode d'affichage tmux : les coéquipiers n'ont pas de session tmux à eux ; ils sont des panes de la session du lanceur.

## Passage de `--teammate-mode` et `--session-id` par `--args` de gld-matin (lecture du code)

- `flagRe` (claude.go:65) : `^--?[a-zA-Z][a-zA-Z0-9-]*(=[A-Za-z0-9._:/-]+)?$`, appliquée à chaque mot de `strings.Fields(raw)` (claude.go:52-58).
- `--teammate-mode=tmux` : passe (valeur `tmux` dans la classe). `--session-id=9d087000-13eb-4519-9823-771e3a943e4c` : passe (hex et tirets). Testé à la lecture seulement, non exécuté.
- Forme à deux mots `--teammate-mode tmux` : REFUSÉE (`tmux` ne commence pas par `-`). Il faut la forme `=`. Non confirmé que claude accepte `--teammate-mode=tmux` (le spike a utilisé la forme à espace).
- Chemin : `claudeArgs` -> `["--args", "a b"]` (claude.go:60) ; matin.go:68-69 `strings.Fields(v)` -> `o.args` ; matin.go:145 et 219 `claudeTabArgs(o.args, ...)` ; `--resume <id>` est ajouté après (matin.go claudeTabArgs) : `--session-id` et `--resume` ne doivent pas être combinés (non testé).

## Variable d'environnement

- `runTab` (term.go:169-180) lance `withPath(exec.Command("claude", s.Args...))` ; `withPath` écrase `c.Env = append(environNoAuth(), "PATH=...")` (claude.go:144) et `execClaude` fait `syscall.Exec(c.Path, c.Args, c.Env)` (term_unix.go:13). L'environnement du serveur gld ne passe donc pas tel quel, et `tabSpec` (term.go:19-22, `Cwd`, `Args`) n'a pas de champ d'environnement.
- Proposition confirmée par lecture : ajouter `Env map[string]string \`json:"env,omitempty"\`` à `tabSpec`, et dans `runTab`, après `withPath(...)` et avant `execClaude`, `for k, v := range s.Env { c.Env = append(c.Env, k+"="+v) }` (après withPath, sinon écrasé). Le spec est relu depuis un fichier JSON (writeSpec, term.go:152) donc le champ voyage sans autre changement. À valider : liste blanche côté serveur (seule `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`), `--args` ne devant jamais porter de variable.
- Fichiers créés sous `~/.claude` : lecture de `CLAUDE_CONFIG_DIR` par claude : non testé.

## Nettoyage

Session tmux `gld-spike-q7x2` tuée, dossier jetable, `/tmp/gld-spike-perm*.txt`, `~/.claude/teams/session-9d087000`, `~/.claude/tasks/session-9d087000` supprimés. Le transcript de session reste dans `~/.claude/projects/`.
