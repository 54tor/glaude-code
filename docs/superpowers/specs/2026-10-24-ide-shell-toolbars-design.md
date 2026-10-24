# IDE de gld : barres d'outils et zone terminal visible

Date : 2026-10-24. Statut : **retenue par l'opérateur** (« je ne veux pas de shell directement accessible ; je veux un IDE Web avec les limites qu'on a posées »). Complète `2026-10-24-ide-design.md` (qui reste la référence pour tout le reste). Décisions de l'opérateur (TUI et messages du 2026-10-24) : les quatre barres (actions de l'éditeur, outils de l'arbre, barre d'état, menu supérieur façon Cloud9) ; **aucun shell libre** (l'option « vrai shell », un temps étudiée dans tmux ou en pty, est écartée) ; le terminal garde ses limites actuelles. Les choix marqués « mon choix » sont ceux du rédacteur.

## Intention

Le premier lot livre l'arbre, l'éditeur Monaco, le contrôle des conflits, l'index et un terminal qui se branche sur une session Claude vivante. À l'usage, trois manques : l'interface ne montre aucune barre pour agir sans connaître les raccourcis ; la zone terminal est repliée et muette (on ne voit pas qu'elle existe ni pourquoi elle est vide) ; la corbeille n'a pas d'interface. Ce lot ajoute les quatre barres et rend la zone terminal visible et explicite, **sans nouvelle capacité d'exécution** : aucune route serveur ne lance de processus.

## Buts

1. **Zone terminal visible et explicite.** Une barre basse toujours présente (titre « Terminal », bouton d'ouverture, état) avec les messages qui disent pourquoi elle est vide : terminal désactivé (`GLD_WEB_TERMINAL=1`), droit `exec` absent, aucune session Claude vivante dans ce dossier (avec l'indication de lancer `claude` dans ce dossier), plusieurs sessions (sélecteur collant). Repliée jusqu'à la première ouverture explicite (décision de l'opérateur, TUI du 2026-10-24, option A : l'attache, même en lecture seule, peut réduire la fenêtre tmux de l'hôte) : bouton de la barre, menu, barre d'état ou commande. Ouverte, elle est en lecture seule, sans focus ni redimensionnement envoyé, tant que l'utilisateur n'a pas pris la main (crayon) — comportement déjà livré. L'état ouvert/replié choisi par l'utilisateur est ensuite mémorisé.
2. **Barre d'actions de l'éditeur** : enregistrer, enregistrer tout, annuler, rétablir, rechercher, remplacer, aller à la ligne, palette de commandes (F1), retour à la ligne, taille de police −/+.
3. **Barre d'outils de l'arbre** : nouveau fichier, nouveau dossier, actualiser, tout replier, ouverture rapide, filtre par nom (sur ce qui est déjà chargé, mon choix), corbeille (liste, restauration, purge : les routes existent, l'interface non).
4. **Barre d'état** : ligne:colonne et sélection, fins de ligne (LF/CRLF, lecture seule signalée), langage, indentation, encodage (UTF-8), branche git, état de la surveillance disque (connecté / reconnexion / interrompu), état de l'index, nombre d'onglets modifiés, bouton du terminal.
5. **Menu supérieur** : Fichier, Édition, Rechercher, Affichage, Aller à, Terminal (mon choix : « Terminal » à la place d'« Exécuter » de Cloud9, puisque gld n'a ni configuration d'exécution ni débogueur ; ses entrées se limitent à ouvrir/replier la zone et à choisir la session). Raccourcis affichés.

## Non-buts

- **Aucun shell libre, aucun processus lancé depuis l'interface** : pas de `bash`, pas de pty, pas de fenêtre tmux créée par gld pour l'utilisateur, aucune nouvelle route d'exécution. Le terminal reste un simple branchement sur une session Claude déjà ouverte (`/term-open`, `/term-ws` inchangés).
- Pas de configuration d'exécution, de débogueur, de tâches, ni de réglage personnalisé des raccourcis.
- Pas de nouvel éditeur ni de nouvelle dépendance : Radix déjà installé (`Menubar`, `Toolbar`, `Tooltip`, `DropdownMenu`), `lucide-react`, xterm et Monaco existants.

## Anti-buts

- Une barre qui contourne un droit : action d'écriture active en lecture seule ou sans `fs_write`, action de terminal sans `exec` (les boutons sont désactivés avec la raison en info-bulle, jamais masqués).
- Une barre ou un menu qui capte la frappe de Monaco ou de xterm (les raccourcis globaux restent soumis à `isEditableTarget`).
- Le terminal qui s'attache, prend la main, vole le focus ou redimensionne l'hôte sans action explicite de l'utilisateur.
- Une régression du flux de fichiers, des conflits, des brouillons ou des droits.

## Contraintes

- Monaco reste importé uniquement par le chunk paresseux : `CodeEditor` expose une poignée impérative (`undo`, `redo`, `find`, `replace`, `gotoLine`, `palette`, `focus`, `zoom`, abonnement à l'état position/sélection/EOL/langage/indentation) ; aucune autre importation de `monaco-editor`.
- Les commandes sont déclarées une fois (`commands.ts` : id, clé i18n, raccourci, groupe, `enabled`, `reason`, `run`) et réutilisées par le menu, les barres, la palette et les raccourcis.
- Préférences d'affichage (retour à la ligne, minimap, taille de police, arbre / terminal / barre d'état visibles) en `useLocal` globales `gld.ide.view.*`.
- La surveillance disque se reconnecte d'elle-même quand l'`EventSource` est fermé définitivement (attente 5 s, 15 s, 30 s) et l'état se lit dans la barre d'état : la limite connue « le flux peut mourir en silence » disparaît.
- Interface : textes en `fr` et `en` (`ide.json`, mêmes clés), aucune chaîne en dur, « » et accents, `Menubar`/`Toolbar`/`aria-label`, info-bulles avec raccourci, navigation au clavier.
- Même discipline que le premier lot : un commit par sujet, français, sujet seul, aucun trailer, jamais de `git push`, tests ciblés, `-p 4` au plus.

## Conception

Pas de changement serveur. Côté interface (`gld/ui/src/components/ide/`) : `commands.ts` (registre), `useViewPrefs.ts`, `MenuBar.tsx`, `EditorToolbar.tsx`, `TreeToolbar.tsx`, `StatusBar.tsx`, `TrashDialog.tsx`, `BottomPane.tsx` (enveloppe de `TerminalPane` : barre toujours visible, états explicites, ouverture/repli mémorisés). `useWatch` gagne l'état de reconnexion.

### Sécurité

Aucune nouvelle capacité : pas de route d'exécution, droits inchangés. Les boutons du terminal exigent `exec` et `GLD_WEB_TERMINAL=1` comme avant ; la documentation (`docs/ide.md`) le rappelle et précise que gld ne propose volontairement aucun shell libre.

## Laissé au constructeur

Découpage exact des composants, icônes, ordre des entrées de menu au-delà de la liste donnée, wording `fr`/`en`, espacements.

## Questions ouvertes pour l'opérateur

Aucune bloquante. Tranché par l'opérateur : la zone terminal reste repliée jusqu'à la première ouverture explicite, puis le choix est mémorisé. À contester à la relecture : « Terminal » à la place d'« Exécuter », le filtre de l'arbre limité aux entrées chargées.
