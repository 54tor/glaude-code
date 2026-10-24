# IDE de gld : barres d'outils et zone terminal visible, plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ajouter à l'espace de travail de gld les quatre barres (menu supérieur, actions de l'éditeur, outils de l'arbre avec corbeille, barre d'état) et rendre la zone terminal visible et explicite, sans aucun shell libre ni nouvelle route serveur.

**Architecture:** Front uniquement. Un registre de commandes unique (`commands.ts`) alimente menu, barres, palette et raccourcis ; `CodeEditor` expose une poignée impérative (seul pont vers Monaco) ; les préférences d'affichage vivent en `useLocal` ; `BottomPane` enveloppe le `TerminalPane` existant. Aucun changement Go.

**Tech Stack:** React 19, `radix-ui` (`Menubar`, `Toolbar`, `Tooltip`, `DropdownMenu` déjà installés), `lucide-react`, xterm et Monaco existants. Aucune dépendance npm ni Go nouvelle.

**Spec:** `docs/superpowers/specs/2026-10-24-ide-shell-toolbars-design.md` (complète `2026-10-24-ide-design.md`).

## Global Constraints

- **Aucun shell, aucun processus lancé depuis l'interface, aucune nouvelle route serveur.** `/term-open` et `/term-ws` restent inchangés ; le terminal reste un branchement sur une session Claude vivante du dossier, soumis à `GLD_WEB_TERMINAL=1` et au droit `exec`.
- Le terminal ne s'attache jamais, ne prend jamais la main, ne vole jamais le focus et ne redimensionne jamais l'hôte sans action explicite : il s'ouvre en lecture seule (`takeHand=false`, comportement déjà livré) ; le crayon prend la main.
- `monaco-editor` importé uniquement par les fichiers du chunk paresseux (`CodeEditor.tsx`, `monacoSetup.ts`) ; la poignée impérative est l'unique pont.
- Raccourcis globaux : toujours soumis à `isEditableTarget` (`lib/editable.ts`) ; menu et barres ne capturent pas la frappe dans Monaco ni dans xterm.
- Un bouton ou une entrée dont le droit ou l'état manque est **désactivé avec la raison en info-bulle**, jamais masqué ni actif.
- Textes visibles en `fr` ET `en` (`src/i18n/{fr,en}/ide.json`, mêmes clés), « » et accents, aucun commentaire dans le HTML/JSX rendu ; commentaires de source = un *pourquoi* seulement.
- Accessibilité : `Menubar`, `Toolbar`, `aria-label`, info-bulles avec raccourci, navigation au clavier.
- Préférences d'affichage : `useLocal` globales `gld.ide.view.*` (retour à la ligne, minimap, taille de police 10–28, arbre / terminal / barre d'état visibles).
- Checkout partagé : commits `type(scope): description` français, sujet seul, **aucun trailer**, jamais de `git push` ; ne stager que ses propres hunks, jamais `git add -A` ; `gld/auth_store_test.go`, `gld/runas_test.go`, `gld/ui/src/components/Conversation.tsx` et `LiveStatus.tsx` appartiennent à une autre session.
- Commandes : depuis `gld/ui`, `npx vitest run <fichiers>`, `npx tsc -p tsconfig.app.json --noEmit` (`npm run typecheck` ne vérifie rien), eslint sur les fichiers touchés (`npm run lint` échoue sur un fichier d'une autre session : l'ignorer) ; jamais de `npm run build` dans `gld/ui/dist` (dossier temporaire seulement) ; jamais de `rm` à préfixe variable ; `-p 4` au plus pour Go.

## Review Focus

- Une barre ne doit jamais proposer une action interdite (lecture seule, sans `fs_write`, sans `exec`) : bouton désactivé avec raison (Tasks 15-17).
- Les raccourcis du menu ne doivent pas voler la frappe dans Monaco ni dans xterm ; Alt+lettre, F1, Ctrl+Maj+P (Tasks 14, 17, 18).
- Chaque commande du registre a un point d'entrée ; aucune entrée de menu sans commande ni commande orpheline (Task 17).
- Le terminal ne s'attache pas sans conditions (activé + `exec` + session vivante) et jamais en écriture ni avec focus tant que l'utilisateur n'a pas pris la main (Task 15, 18).
- La reconnexion du flux ne doit pas créer de flux en double, ni de minuteur après démontage (Task 17).
- Largeur étroite : les barres passent à la ligne ou se replient sans défilement horizontal de la page (Task 18).

---

## Task 14: Registre de commandes, poignée de l'éditeur et préférences d'affichage

**Files:**
- Create: `gld/ui/src/components/ide/commands.ts`, `commands.test.ts`, `useViewPrefs.ts`, `useViewPrefs.test.ts`
- Modify: `gld/ui/src/components/ide/CodeEditor.tsx` (poignée impérative), `IdeView.tsx` (câblage), `src/i18n/{fr,en}/ide.json`
- Test: `CodeEditor.dom.test.tsx`

**Interfaces:**
- Produces: `type EditorHandle = { undo(): void; redo(): void; find(): void; replace(): void; gotoLine(): void; palette(): void; focus(): void; zoom(delta: 1|-1|0): void; subscribe(l: (s: EditorStatus) => void): () => void }` ; `type EditorStatus = { line: number; col: number; selected: number; eol: "LF"|"CRLF"; language: string; indent: string; readOnly: boolean }` ; `type Command = { id: string; label: string /* clé i18n */; shortcut?: string; group: "file"|"edit"|"find"|"view"|"goto"|"terminal"; enabled(ctx: CmdCtx): boolean; reason?(ctx: CmdCtx): string; run(ctx: CmdCtx): void }` ; `const commands: Command[]` ; `function useViewPrefs(): { wordWrap, minimap, fontSize, showTree, showTerminal, showStatus, set(k, v) }` ; `CodeEditor` reçoit `wordWrap`, `minimap`, `fontSize` et `editorRef`.

- [ ] **Step 1: Write the failing tests** : `commands.test.ts` (id uniques ; clé i18n présente en fr ET en ; `enabled` faux en lecture seule pour les commandes d'écriture ; faux sans `exec` ou terminal désactivé pour le groupe terminal ; `reason` non vide quand désactivée) ; `useViewPrefs.test.ts` (défauts, persistance `gld.ide.view.*`, JSON invalide toléré, bornes de `fontSize` 10–28) ; `CodeEditor.dom.test.tsx` (la poignée déclenche l'action Monaco correspondante sur le faux éditeur ; `subscribe` reçoit position/EOL/langage ; désabonnement au démontage ; `wordWrap`/`minimap`/`fontSize` poussés dans les options).
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/commands.test.ts src/components/ide/useViewPrefs.test.ts src/components/ide/CodeEditor.dom.test.tsx` depuis `gld/ui` ; attendu : FAIL.
- [ ] **Step 3: Implement** : la poignée passe par `editor.getAction("actions.find")`, `"editor.action.startFindReplaceAction"`, `"editor.action.gotoLine"`, `trigger("keyboard","undo",…)`, `"editor.action.quickCommand"` (vérifier les identifiants dans les types de la version installée de Monaco) ; `EditorStatus` alimenté par `onDidChangeCursorPosition`, `onDidChangeModel`, `model.getEOL()` ; aucun autre fichier n'importe `monaco-editor`.
- [ ] **Step 4: Run to verify pass** — mêmes tests + `npx tsc -p tsconfig.app.json --noEmit` + eslint + `npx vitest run src/components/ide src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): registre de commandes, poignée de l'éditeur et préférences d'affichage`

## Task 15: Zone terminal visible et explicite

**Files:**
- Create: `gld/ui/src/components/ide/BottomPane.tsx`, `BottomPane.test.tsx`
- Modify: `TerminalPane.tsx` (contenu de la zone), `IdeView.tsx`, `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Task 14 (`useViewPrefs.showTerminal`, `commands`), `api.termInfo`, `api.data`, rights helper (`useCan`).
- Produces: `BottomPane({ dir })` : barre toujours visible (titre, bouton ouvrir/replier, indicateur d'état) enveloppant `TerminalPane`.

- [ ] **Step 1: Write the failing tests** : barre présente dans tous les cas ; terminal désactivé (`/term-info` enabled=false) → état « désactivé (GLD_WEB_TERMINAL=1) » sans autre appel ; sans `exec` → bouton désactivé avec raison, aucun appel ; aucune session vivante dans le dossier → message « lancez `claude` dans ce dossier », zone repliée ; une session vivante + activé + `exec` + pas de préférence mémorisée → ouverte d'emblée en lecture seule, **sans** focus ni écriture ni redimensionnement (le terminal est monté avec `takeHand=false`) ; préférence mémorisée (ouvert/replié par l'utilisateur) prioritaire sur l'ouverture d'emblée ; plusieurs sessions → sélecteur collant (la session montée ne change pas quand `api.data()` réordonne) ; aucun appel `/term-open` tant que la zone est repliée.
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/BottomPane.test.tsx`.
- [ ] **Step 3: Implement** ; réutiliser `TerminalPane`/`FullTerminal` sans les modifier au-delà de ce qui est nécessaire ; l'état ouvert/replié passe par `useViewPrefs.showTerminal` mais ne s'applique qu'après un choix de l'utilisateur (sinon règle d'ouverture d'emblée).
- [ ] **Step 4: Run to verify pass** — + tsc + eslint + `npx vitest run src/components/ide src/components/FullTerminal src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): zone terminal toujours visible avec états explicites`

## Task 16: Barre d'actions de l'éditeur, barre d'outils de l'arbre et corbeille

**Files:**
- Create: `gld/ui/src/components/ide/EditorToolbar.tsx`, `TreeToolbar.tsx`, `TrashDialog.tsx` et leurs tests
- Modify: `FileTree.tsx` (filtre sur les entrées chargées), `useTree.ts` (`collapseAll`, `refreshAll`), `IdeView.tsx`, `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Task 14 (`commands`, `EditorHandle`, `useViewPrefs`), `api.fsTrash`, `api.fsTrashRestore`, `api.fsTrashPurge`, `useTree`.
- Produces: `EditorToolbar({ handle, tab, can })`, `TreeToolbar({ tree, can })`, `TrashDialog({ dir, open, onClose })`, `useTree.collapseAll()` et `useTree.refreshAll()`.

- [ ] **Step 1: Write the failing tests** : la barre d'actions déclenche chaque commande de la poignée (Enregistrer = fsSave exact via le flux existant, Enregistrer tout = chaque onglet sale en série), désactivée avec raison en lecture seule / sans `fs_write` ; barre de l'arbre : nouveau fichier/dossier à la racine (flux de création existant), actualiser (`refreshAll` recharge seulement les dossiers dépliés), tout replier, filtre (sous-chaîne insensible à la casse sur les entrées chargées, aucun appel réseau, vider = tout revoir), Ouverture rapide ouvre le dialogue existant ; `TrashDialog` : liste (`fsTrash`), restaurer (409 → message « existe déjà »), purger avec confirmation, désactivé sans `fs_write`, état vide.
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/EditorToolbar.test.tsx src/components/ide/TreeToolbar.test.tsx src/components/ide/TrashDialog.test.tsx`.
- [ ] **Step 3: Implement** avec `radix-ui` `Toolbar` et `Tooltip` (libellé + raccourci), icônes `lucide-react`.
- [ ] **Step 4: Run to verify pass** — + tsc + eslint + `npx vitest run src/components/ide src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): barres d'outils de l'éditeur et de l'arbre, corbeille`

## Task 17: Menu supérieur et barre d'état

**Files:**
- Create: `gld/ui/src/components/ide/MenuBar.tsx`, `StatusBar.tsx` et leurs tests
- Modify: `IdeView.tsx`, `useWatch.ts` (reconnexion 5 s / 15 s / 30 s), `src/i18n/{fr,en}/ide.json`

**Interfaces:**
- Consumes: Tasks 14-16, `useWatch.connected`, `useIndex.state`, `useGitMarks`/`api.gitStatus` (branche), `EditorStatus`.
- Produces: `MenuBar({ ctx })` (Fichier, Édition, Rechercher, Affichage, Aller à, Terminal ; raccourcis affichés ; entrées désactivées avec raison), `StatusBar({ status, watch, index, branch, dirty, can })`, `useWatch` renvoie `{ connected, state: "open"|"retrying"|"closed", retryIn }`.

- [ ] **Step 1: Write the failing tests** : chaque entrée du menu correspond à une commande du registre (aucune orpheline) ; menu navigable au clavier ; Alt+lettre et F1 non capturés quand `isEditableTarget` ; entrées d'écriture désactivées en lecture seule ; entrées Terminal désactivées sans `exec`/terminal désactivé ; barre d'état : ligne:colonne et sélection, LF/CRLF (+ « lecture seule »), langage, indentation, UTF-8, branche git (absente hors dépôt), flux (connecté / reconnexion dans N s / interrompu), index (en cours / partiel / prêt), onglets modifiés, bouton du terminal ; `useWatch` : fermeture définitive de l'`EventSource` → nouvel essai à 5 s, 15 s, 30 s ; un succès remet à zéro et vaut resynchronisation ; démontage = plus aucun minuteur ni flux en double.
- [ ] **Step 2: Run to verify failure** — `npx vitest run src/components/ide/MenuBar.test.tsx src/components/ide/StatusBar.test.tsx src/components/ide/useWatch.test.ts`.
- [ ] **Step 3: Implement** avec `radix-ui` `Menubar`.
- [ ] **Step 4: Run to verify pass** — + tsc + eslint + `npx vitest run src/components/ide src/hooks src/lib src/i18n`.
- [ ] **Step 5: Commit** — `feat(ide): menu supérieur et barre d'état avec reconnexion du flux`

## Task 18: Documentation et vérification de bout en bout

**Files:**
- Modify: `docs/ide.md`, `README.md`, `CHANGELOG.md`, `docs/superpowers/specs/2026-10-24-ide-shell-toolbars-design.md` (écarts éventuels)

- [ ] **Step 1: Documenter** : menu et barres, raccourcis, préférences en `localStorage` (`gld.ide.view.*`), zone terminal (états, ouverture d'emblée en lecture seule, pas de shell libre — volontairement), reconnexion du flux.
- [ ] **Step 2: Suites ciblées** (une seule passe, sorties filtrées) : depuis `gld/ui`, `npx vitest run src/components/ide src/components/FullTerminal src/components/auth src/hooks src/lib src/i18n` et `npx tsc -p tsconfig.app.json --noEmit` ; depuis `gld/`, `go vet .` et `go test -p 4 -count=1 -run 'TestEveryRouteClassified|TestI18n|TestCatalog|TestIde|TestFs' .` (aucun Go modifié : simple garde-fou).
- [ ] **Step 3: Build de production** dans un dossier temporaire : aucun avertissement de taille, `app.js` ≈ 655 ko sans Monaco, workers présents ; **chargement du graphe de chunks Monaco découpé vérifié dans un vrai navigateur** (jusqu'ici seulement statique).
- [ ] **Step 4: Navigateur réel** (Playwright, instance isolée selon `.superpowers/sdd/2026-10-24-ide/task-12-report.md`) : les quatre barres présentes ; chaque bouton/entrée fonctionne ou est désactivé avec raison (essayer en lecture seule : fichier à fins de ligne mixtes, et un compte sans `fs_write` si le mode multi-utilisateur est cheap à monter, sinon via test) ; Enregistrer / Enregistrer tout / Annuler / Rétablir / Rechercher / Remplacer / Aller à la ligne / F1 / retour à la ligne / zoom ; barre de l'arbre : nouveau fichier et dossier à la racine, actualiser, tout replier, filtre, corbeille (supprimer un fichier puis le restaurer depuis le dialogue, puis purger) ; barre d'état : ligne:colonne suit le curseur, LF/CRLF, langage, branche git ; menu : toutes les entrées, raccourcis affichés ; la frappe dans Monaco n'est pas volée ; zone terminal : sans `GLD_WEB_TERMINAL` message « désactivé », avec `GLD_WEB_TERMINAL=1` mais sans session vivante message d'aide, avec une session tmux factice vivante (cwd = le dossier) ouverture d'emblée en lecture seule sans focus ni redimensionnement, le crayon prend la main ; couper puis relancer le serveur → barre d'état « reconnexion » puis rétablie ; clair/sombre ; largeur 390 px sans défilement horizontal. Captures dans le scratchpad. Nettoyage obligatoire (PID lancés, `.gld-dev-ide/`, sessions tmux factices, onglet du navigateur).
- [ ] **Step 5: Commit** — `docs(ide): barres d'outils, zone terminal et limites`

## Auto-relecture

- **Couverture de la spec :** zone terminal visible/explicite (15) ; barre d'actions (14, 16) ; barre de l'arbre + corbeille + filtre (16) ; barre d'état + flux visible avec reconnexion (17) ; menu à six entrées (17) ; préférences d'affichage (14) ; documentation et vérification (18). Aucun shell, aucune route serveur : confirmé par les contraintes globales.
- **Cohérence de types :** `EditorHandle`/`EditorStatus`/`Command` définis en 14 et consommés en 16-17 ; `useWatch` étendu en 17 sans casser `connected`.
- **Hors périmètre rappelé :** shell libre (écarté par l'opérateur), exécution/débogage, raccourcis personnalisables.
