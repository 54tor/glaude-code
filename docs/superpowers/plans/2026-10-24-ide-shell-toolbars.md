# IDE de gld : vrai terminal et barres d'outils, plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ajouter à l'espace de travail de gld un vrai shell (dans tmux, rebranchable après rechargement) et les quatre barres : menu supérieur, actions de l'éditeur, outils de l'arbre, barre d'état.

**Architecture:** Backend : trois routes `exec` (`/shell-open`, `/shell-list`, `/shell-close`) qui créent des sessions tmux dédiées et délivrent un jeton du terminal web existant ; `/term-ws` n'est pas modifié. Front : un registre de commandes unique (`commands.ts`) alimente menu, barres, palette et raccourcis ; `CodeEditor` expose une poignée impérative ; les préférences d'affichage vivent en `useLocal`.

**Tech Stack:** Go (tmux déjà requis), React 19, `radix-ui` (`Menubar`, `Toolbar`, `Tooltip`, `DropdownMenu` déjà installés), `lucide-react`, xterm existant, Monaco existant (aucune dépendance npm ni Go nouvelle).

**Spec:** `docs/superpowers/specs/2026-10-24-ide-shell-toolbars-design.md` (complète `2026-10-24-ide-design.md`).

## Global Constraints

- Droit **`exec`** pour les trois routes shell, ajoutées à `routeRights` dans le même commit ; `GLD_WEB_TERMINAL=1` requis (`termEnabled()`) ; racine validée par `ideKnownDir` (jamais `$HOME`, config Claude, auth de gld).
- Nom de session tmux `gld-shell-<12 hex>` ; option de session `@gld_dir=<racine>` ; toutes les commandes tmux en tableau d'arguments, jamais via un shell intermédiaire ; tout identifiant reçu est validé par `^[0-9a-f]{12}$` avant usage.
- Bornes (constantes) : 8 shells gld au total, 4 par espace de travail (HTTP 429 au-delà), balai : shell non attaché depuis 30 min supprimé, taille 20–500 colonnes / 5–300 lignes.
- Environnement du shell : `env -u GLD_AUTH_KEY -u GLD_AUTH_DIR -u GLD_WEB_MAIN_CONFIG -u <variable du jeton enfant>` (liste en constante, testée) ; shell = `$SHELL` exécutable, sinon `/bin/bash`, sinon `/bin/sh` ; `TERM=xterm-256color` ; `applyRunAs` respecté.
- Audit : événements `shell_open` / `shell_close` (utilisateur, base du dossier, durée), jamais commandes ni sortie, relais enfant par l'en-tête interne comme `X-Gld-Audit`.
- `monaco-editor` importé uniquement par les fichiers du chunk paresseux (`CodeEditor.tsx`, `monacoSetup.ts` et tout fichier qu'ils importent exclusivement) ; la poignée impérative est l'unique pont.
- Raccourcis globaux : toujours soumis à `isEditableTarget` (`lib/editable.ts`) ; les commandes du menu ne capturent pas la frappe dans Monaco.
- Textes visibles en `fr` ET `en` (`src/i18n/{fr,en}/ide.json`, mêmes clés), « » et accents, aucun commentaire dans le HTML/JSX rendu ; commentaires de source = un *pourquoi* seulement.
- Accessibilité : `Menubar`, `Toolbar`, `aria-label`, info-bulles avec raccourci, navigation au clavier ; boutons désactivés (jamais masqués) quand le droit ou l'état manque, avec la raison en info-bulle.
- Préférences d'affichage : `useLocal` globales `gld.ide.view.*` (retour à la ligne, minimap, taille de police, arbre/terminal/barre d'état visibles).
- Checkout partagé : commits `type(scope): description` français, sujet seul, **aucun trailer**, jamais de `git push` ; ne stager que ses propres hunks, jamais `git add -A` ; `gld/auth_store_test.go`, `gld/runas_test.go`, `gld/ui/src/components/Conversation.tsx` et `LiveStatus.tsx` appartiennent à une autre session.
- Builds ciblés, `-p 4` au plus ; `npm run lint` échoue sur un fichier d'une autre session : l'ignorer ; vérifier les types avec `npx tsc -p tsconfig.app.json --noEmit` ; jamais de `npm run build` dans `gld/ui/dist` (dossier temporaire seulement) ; jamais de `rm` à préfixe variable.

## Review Focus

- Le shell ne doit jamais s'ouvrir sans `exec` + `GLD_WEB_TERMINAL=1`, ni dans une racine interdite, ni avec l'environnement de gld (secrets) : tests de refus et de filtrage d'environnement (Task 14).
- Un identifiant de shell forgé (`../x`, majuscules, trop long, shell d'un autre dossier) ne doit rien fermer ni rattacher (Task 14).
- Fermer/perdre le droit `exec` : les routes répondent 403, les shells continuent jusqu'au balai (documenté) (Task 14, 19).
- Une barre ne doit jamais proposer une action interdite (lecture seule, sans `fs_write`, sans `exec`) : bouton désactivé avec raison (Tasks 15-18).
- Les raccourcis du menu ne doivent pas voler la frappe dans Monaco ni dans xterm (Tasks 15, 18, 19).
- Rechargement de la page avec des shells ouverts : rebranchement, pas de doublon ni de shell orphelin (Tasks 14, 16, 19).

---

## Task 14: Shell dans tmux (backend)

**Files:**
- Create: `gld/ide_shell.go`, `gld/ide_shell_test.go`
- Modify: `gld/main.go` (appel `routesShell(mux)` et balai arrêté à l'arrêt du serveur), `gld/auth_acl.go` (`routeRights` : trois routes en `RightExec`), `gld/locales/{fr,en}.json` (clés `srv.shell.*`)

**Interfaces:**
- Consumes: `termEnabled()`, `termToks`/`termTok`/`termMu`, `termGeometry(pane)`, `tmuxRun(...)`, `termToken()`, `ideKnownDir(dir)` (gld/termweb.go, ide_path.go), `ideAudit`-style relais d'audit (gld/ide_audit.go).
- Produces: `POST /shell-open` `{dir, cols, rows, id?}` → `{ok, token, id, cols, rows}` ; `GET /shell-list?dir=` → `{shells:[{id, created, attached}]}` ; `POST /shell-close` `{id}` → `{ok}` ; `func routesShell(mux *http.ServeMux)` ; `func shellSweepStart() (stop func())`.

- [ ] **Step 1: Write the failing tests** (gld/ide_shell_test.go, avec un faux `tmux` comme le fait gld/termweb_test.go) : `TestShellOpenCreatesDedicatedSession` (nom `gld-shell-<12hex>`, `-c <racine>`, `@gld_dir`, commande `env -u … $SHELL`) ; `TestShellOpenRefusals` (terminal désactivé, racine interdite/inconnue, sans `exec` → 403 par le garde, `id` mal formé → 400) ; `TestShellEnvFilterList` (les quatre variables retirées, liste testée) ; `TestShellListFiltersByDirAndPrefix` ; `TestShellCloseOnlyOwnShells` (id d'un autre dossier ou sans préfixe → refus, rien exécuté) ; `TestShellBounds` (9ᵉ shell → 429, 5ᵉ dans un dossier → 429) ; `TestShellReattachIssuesToken` (avec `id`, pas de nouvelle session, jeton émis dans `termToks` avec l'id du shell) ; `TestShellSweepKillsIdle` (session non attachée > 30 min tuée, attachée ou récente conservée ; délai abaissé via variable) ; `TestShellAudit` (un événement `shell_open` et `shell_close`, sans commande) ; `TestEveryRouteClassified` passe.
- [ ] **Step 2: Run to verify failure** — `go test -p 4 -count=1 -run 'TestShell' .` depuis `gld/` ; attendu : FAIL (symboles indéfinis).
- [ ] **Step 3: Implement** selon les contraintes globales. Le jeton est enregistré dans la table `termToks` existante avec `pane/sess/win` résolus par `termGeometry` sur le panneau de la session créée, et `id` = identifiant du shell (`termBusy` garantit ainsi un seul client). Aucune modification de `termWS`.
- [ ] **Step 4: Run to verify pass** — mêmes tests + `go vet .` + `go test -p 4 -count=1 -run 'TestEveryRouteClassified|TestI18n|TestCatalog|TestWSCutOnRevoke|TestTerm' .`.
- [ ] **Step 5: Commit** — `feat(ide): shells libres dans des sessions tmux dédiées`

## Task 15: Registre de commandes, poignée de l'éditeur et préférences d'affichage

**Files:**
- Create: `gld/ui/src/components/ide/commands.ts`, `commands.test.ts`, `useViewPrefs.ts`, `useViewPrefs.test.ts`
- Modify: `gld/ui/src/components/ide/CodeEditor.tsx` (poignée impérative), `IdeView.tsx` (câblage), `src/i18n/{fr,en}/ide.json`
- Test: `CodeEditor.dom.test.tsx`

**Interfaces:**
- Produces: `type EditorHandle = { undo(): void; redo(): void; find(): void; replace(): void; gotoLine(): void; palette(): void; focus(): void; zoom(delta: 1|-1|0): void; subscribe(l: (s: EditorStatus) => void): () => void }` ; `type EditorStatus = { line: number; col: number; selected: number; eol: "LF"|"CRLF"; language: string; indent: string; readOnly: boolean }` ; `type Command = { id: string; label: string /* clé i18n */; shortcut?: string; group: "file"|"edit"|"find"|"view"|"goto"|"terminal"; enabled(ctx: CmdCtx): boolean; reason?(ctx: CmdCtx): string; run(ctx: CmdCtx): void }` ; `const commands: Command[]` ; `function useViewPrefs(): { wordWrap, minimap, fontSize, showTree, showTerminal, showStatus, set(k, v) }` ; `CodeEditor` reçoit `wordWrap`, `minimap`, `fontSize` et `editorRef`.

- [ ] **Step 1: Write the failing tests** : `commands.test.ts` (chaque commande a un id unique, une clé i18n présente en fr ET en, `enabled` faux en lecture seule pour les commandes d'écriture, faux sans `exec` pour celles du groupe terminal, `reason` non vide quand désactivée) ; `useViewPrefs.test.ts` (valeurs par défaut, persistance `gld.ide.view.*`, JSON invalide toléré, bornes de `fontSize` 10–28) ; `CodeEditor.dom.test.tsx` (poignée : `undo/redo/find/replace/gotoLine/palette` appellent l'action Monaco correspondante sur le faux éditeur, `subscribe` reçoit position/EOL/langage, désabonnement au démontage, `wordWrap`/`minimap`/`fontSize` poussés dans les options).
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/commands.test.ts src/components/ide/useViewPrefs.test.ts src/components/ide/CodeEditor.dom.test.tsx` depuis `gld/ui`.
- [ ] **Step 3: Implement** : poignée via `editor.getAction("actions.find")`, `"editor.action.startFindReplaceAction"`, `"editor.action.gotoLine"`, `trigger("keyboard","undo",…)`, palette `"editor.action.quickCommand"` (vérifier les identifiants dans les types de la version installée) ; `EditorStatus` alimenté par `onDidChangeCursorPosition`, `onDidChangeModel`, `model.getEOL()` ; aucun autre fichier n'importe `monaco-editor`.
- [ ] **Step 4: Run to verify pass** + `npx tsc -p tsconfig.app.json --noEmit` + eslint sur les fichiers touchés + `npx vitest run src/components/ide src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): registre de commandes, poignée de l'éditeur et préférences d'affichage`

## Task 16: Zone basse à onglets de terminal (front)

**Files:**
- Create: `gld/ui/src/components/ide/BottomPane.tsx`, `BottomPane.test.tsx`, `useShells.ts`, `useShells.test.ts`
- Modify: `TerminalPane.tsx` (devient l'onglet « session Claude »), `IdeView.tsx`, `src/lib/api.ts` (`shellOpen`, `shellList`, `shellClose`), `src/components/FullTerminal.tsx` si besoin d'accepter un jeton fourni, `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Task 14 (routes), Task 15 (`useViewPrefs.showTerminal`, `commands`).
- Produces: `api.shellOpen(dir, cols, rows, id?)`, `api.shellList(dir)`, `api.shellClose(id)` ; `useShells(dir): { shells: Shell[]; open(): Promise<void>; close(id): Promise<void>; state: "loading"|"disabled"|"forbidden"|"ready" }` ; `BottomPane({ dir })`.

- [ ] **Step 1: Write the failing tests** : au montage avec terminal activé et droit `exec`, `shellList` est appelé puis les shells existants s'affichent (un onglet chacun) et, s'il n'y en a aucun, un premier shell est ouvert ; la zone est ouverte par défaut (`showTerminal` vrai) ; `+` ouvre un shell, la croix le ferme (`shellClose`) ; terminal désactivé → message « désactivé (GLD_WEB_TERMINAL=1) » sans appel ; sans `exec` → zone absente avec info-bulle de raison dans la barre d'état (Task 18) ; plafond 429 → toast et pas de nouvel onglet ; un shell disparu (erreur à l'attache) montre « session terminée » et un bouton Relancer ; l'onglet « session Claude » reste présent seulement s'il y a une session vivante du dossier ; aucun appel `shellOpen` avant l'autorisation d'ouvrir (pas de rafale au rendu, une seule création au premier montage même sous StrictMode).
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/BottomPane.test.tsx src/components/ide/useShells.test.ts`.
- [ ] **Step 3: Implement** ; le shell est monté avec `FullTerminal` (`takeHand` vrai, un shell est fait pour taper) en lui fournissant l'`id` du shell ; la garde de focus/`isEditableTarget` empêche les raccourcis globaux de voler la frappe dans xterm.
- [ ] **Step 4: Run to verify pass** + tsc + eslint + `npx vitest run src/components/ide src/components/FullTerminal src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): zone basse à onglets avec shells et session Claude`

## Task 17: Barre d'actions de l'éditeur, barre d'outils de l'arbre et corbeille

**Files:**
- Create: `gld/ui/src/components/ide/EditorToolbar.tsx`, `TreeToolbar.tsx`, `TrashDialog.tsx` et leurs tests
- Modify: `FileTree.tsx` (filtre sur les entrées chargées, « tout replier », actualiser), `IdeView.tsx`, `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Task 15 (`commands`, `EditorHandle`, `useViewPrefs`), `api.fsTrash`, `api.fsTrashRestore`, `api.fsTrashPurge`, `useTree`.
- Produces: `EditorToolbar({ handle, tab, can })`, `TreeToolbar({ tree, can })`, `TrashDialog({ dir, open, onClose })`, `useTree.collapseAll()` et `useTree.refreshAll()`.

- [ ] **Step 1: Write the failing tests** : la barre d'actions déclenche chaque commande de la poignée (Enregistrer = fsSave exact via le flux existant, Enregistrer tout = chaque onglet sale en série), est désactivée avec raison en lecture seule/sans `fs_write`, `Ctrl+Z`/`F1` inchangés ; la barre de l'arbre : nouveau fichier/dossier à la racine (réutilise le flux de création existant), actualiser (`refreshAll` recharge seulement les dossiers dépliés), tout replier, filtre (sous-chaîne insensible à la casse sur les entrées chargées, vider = tout revoir, aucun appel réseau), bouton Ouverture rapide ouvre le dialogue existant ; `TrashDialog` : liste (`fsTrash`), restaurer (409 → message i18n « existe déjà »), purger avec confirmation, désactivé sans `fs_write`, vide = message.
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/EditorToolbar.test.tsx src/components/ide/TreeToolbar.test.tsx src/components/ide/TrashDialog.test.tsx`.
- [ ] **Step 3: Implement** avec `radix-ui` `Toolbar` et `Tooltip` (libellé + raccourci), icônes `lucide-react`.
- [ ] **Step 4: Run to verify pass** + tsc + eslint + `npx vitest run src/components/ide src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): barres d'outils de l'éditeur et de l'arbre, corbeille`

## Task 18: Menu supérieur et barre d'état

**Files:**
- Create: `gld/ui/src/components/ide/MenuBar.tsx`, `StatusBar.tsx` et leurs tests
- Modify: `IdeView.tsx`, `useWatch.ts` (reconnexion avec attente 5 s / 15 s / 30 s quand l'`EventSource` est fermé définitivement, état exposé), `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Tasks 15-17, `useWatch.connected`, `useIndex.state`, `useGitMarks` (branche via `GitState.branch`/`api.gitStatus`), `EditorStatus`.
- Produces: `MenuBar({ ctx })` (Fichier, Édition, Rechercher, Affichage, Aller à, Terminal ; raccourcis affichés ; entrées désactivées avec raison), `StatusBar({ status, watch, index, branch, dirty, can })`, `useWatch` renvoie `{ connected, state: "open"|"retrying"|"closed", retryIn }`.

- [ ] **Step 1: Write the failing tests** : chaque entrée du menu correspond à une commande du registre (aucune commande orpheline ni entrée sans commande), le menu est navigable au clavier (`Menubar`), Alt+lettre n'entre pas en conflit avec Monaco (non capturé quand `isEditableTarget`), les entrées d'écriture sont désactivées en lecture seule, « Terminal » : nouveau shell / basculer la zone (désactivé sans `exec`) ; la barre d'état affiche ligne:colonne et sélection, LF/CRLF (avec « lecture seule »), langage, indentation, UTF-8, branche git (absente hors dépôt), état du flux (connecté / reconnexion dans N s / interrompu), index (en cours / partiel / prêt), nombre d'onglets modifiés, bouton du terminal ; `useWatch` : fermeture définitive de l'`EventSource` → nouvel essai à 5 s puis 15 s puis 30 s, un succès remet à zéro et est traité comme une resynchronisation, démontage = plus aucun minuteur.
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/MenuBar.test.tsx src/components/ide/StatusBar.test.tsx src/components/ide/useWatch.test.ts`.
- [ ] **Step 3: Implement** avec `radix-ui` `Menubar`.
- [ ] **Step 4: Run to verify pass** + tsc + eslint + `npx vitest run src/components/ide src/hooks src/lib src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): menu supérieur et barre d'état avec reconnexion du flux`

## Task 19: Documentation et vérification de bout en bout

**Files:**
- Modify: `docs/ide.md`, `docs/auth.md` (droit `exec` et shells : persistance jusqu'au balai de 30 min, événements `shell_open`/`shell_close`), `README.md`, `CHANGELOG.md`, `docs/superpowers/specs/2026-10-24-ide-shell-toolbars-design.md` (écarts éventuels)

- [ ] **Step 1: Documenter** : shells dans tmux (visibles depuis le tmux de l'opérateur), bornes, balai, variables retirées, `exec` + `GLD_WEB_TERMINAL=1`, menu et barres, préférences en `localStorage`, raccourcis.
- [ ] **Step 2: Suites ciblées** (une seule passe, sorties filtrées) : `go vet .` ; `go test -p 4 -count=1 -run 'TestShell|TestIde|TestFs|TestIndex|TestWatch|TestEveryRouteClassified|TestI18n|TestCatalog|TestAuth|TestWSCutOnRevoke|TestTerm' .` depuis `gld/` ; `npx vitest run src/components/ide src/components/FullTerminal src/components/auth src/hooks src/lib src/i18n` et `npx tsc -p tsconfig.app.json --noEmit` depuis `gld/ui`.
- [ ] **Step 3: Build de production** dans un dossier temporaire : plus d'avertissement de taille, `app.js` ≈ 655 ko sans Monaco, workers présents, chargement du graphe de chunks Monaco découpé vérifié **dans un vrai navigateur** (ce que seule la vérification statique faisait jusqu'ici).
- [ ] **Step 4: Navigateur réel** (Playwright, instance isolée comme dans `.superpowers/sdd/2026-10-24-ide/task-12-report.md`, avec `GLD_WEB_TERMINAL=1`, un `TMUX_TMPDIR` jetable et tmux installé) : le terminal est visible et ouvert par défaut avec un premier shell ; `echo $GLD_AUTH_KEY` ne montre rien ; `pwd` = racine du projet ; taper, recharger la page → le même shell se rebranche avec son historique ; fermer l'onglet ferme le shell (`tmux ls`) ; 5ᵉ shell refusé ; les quatre barres présentes, chaque bouton/entrée fonctionne ou est désactivé avec raison ; la frappe dans Monaco et dans xterm n'est pas volée par les raccourcis ; la barre d'état suit le curseur, les EOL, le langage, la branche git ; couper le serveur puis le relancer → la barre d'état passe en « reconnexion » puis se rétablit ; clair/sombre ; largeur 390 px sans défilement horizontal. Captures dans le scratchpad. Nettoyage obligatoire (PID lancés par vous, `.gld-dev-ide/`, sessions tmux `gld-shell-*` de l'instance isolée, onglet du navigateur).
- [ ] **Step 5: Commit** — `docs(ide): shell, barres d'outils et limites`

## Auto-relecture

- **Couverture de la spec :** shell tmux, rebranchement, bornes, balai, environnement filtré, audit, droits (Task 14, 16, 19) ; barre d'actions (15, 17) ; barre de l'arbre + corbeille + filtre (17) ; barre d'état + flux visible avec reconnexion (18) ; menu à six entrées (18) ; préférences d'affichage (15) ; documentation et risques (19).
- **Cohérence de types :** `EditorHandle`/`EditorStatus` définis en Task 15 et consommés en 17-18 ; `api.shell*` en 16 ; `useWatch` étendu en 18 sans casser son usage actuel (`connected` conservé).
- **Hors périmètre rappelé :** pas d'exécution/débogage, pas de partage d'un shell entre deux navigateurs, pas de raccourcis personnalisables.
