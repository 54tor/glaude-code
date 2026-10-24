# IDE de gld : vrai terminal et barres d'outils

Date : 2026-10-24. Statut : **proposition en attente d'approbation**. Complète `2026-10-24-ide-design.md` (qui reste la référence pour tout le reste). Décisions de l'opérateur (TUI du 2026-10-24) : vrai shell dans le dossier du projet ; les quatre barres : actions de l'éditeur, outils de l'arbre, barre d'état, menu supérieur façon Cloud9 ; sur la façon de lancer le shell l'opérateur a répondu « je ne sais pas », le rédacteur a donc tranché : **shell dans tmux** (mon choix), qui réutilise le terminal web existant. Les choix marqués « mon choix » sont ceux du rédacteur.

## Intention

Le premier lot livre l'arbre, l'éditeur et un terminal qui se branche sur une session Claude vivante. À l'usage il manque l'essentiel d'un IDE à la Cloud9 : un terminal où taper des commandes, et des barres pour agir sans connaître les raccourcis. Ce lot ajoute un vrai shell et les quatre barres, sans rien changer au confinement, aux droits ni au flux de fichiers déjà en place.

## Buts

1. **Shell dans le dossier du projet.** Une zone basse à onglets : des shells libres (le shell de l'opérateur) lancés par gld dans une session tmux dédiée, dans la racine de l'espace de travail, plusieurs en même temps, et, à côté, le terminal d'une session Claude vivante du dossier (existant). Ouverte par défaut quand le terminal est disponible, avec un premier shell lancé à la première ouverture. **Un shell survit au rechargement de la page** : à la réouverture, les shells existants du dossier sont listés et se rebranchent.
2. **Barre d'actions de l'éditeur** : enregistrer, enregistrer tout, annuler, rétablir, rechercher, remplacer, aller à la ligne, palette de commandes (F1), retour à la ligne, taille de police −/+.
3. **Barre d'outils de l'arbre** : nouveau fichier, nouveau dossier, actualiser, tout replier, ouverture rapide, filtre par nom (sur ce qui est déjà chargé, mon choix), corbeille (liste, restauration, purge : les routes existent, l'interface non).
4. **Barre d'état** : ligne:colonne et sélection, fins de ligne (LF/CRLF, lecture seule signalée), langage, indentation, encodage (UTF-8), branche git, état de la surveillance disque (connecté / interrompu / resynchronisation), état de l'index, nombre d'onglets modifiés, bouton du terminal.
5. **Menu supérieur** : Fichier, Édition, Rechercher, Affichage, Aller à, Terminal (mon choix : « Terminal » à la place d'« Exécuter » de Cloud9, puisque gld n'a ni configuration d'exécution ni débogueur). Raccourcis affichés.

## Non-buts

- Pas de configuration d'exécution, de débogueur, de tâches, de partage simultané d'un même shell entre deux navigateurs (un seul client attaché par shell, comme le terminal existant), ni de remplacement de tmux : tmux est déjà requis par gld pour ses sessions.
- Pas de réglages personnalisés des raccourcis ; pas de terminal hors du dossier de l'espace de travail comme répertoire de départ imposé (l'utilisateur peut évidemment faire `cd`).
- Pas de nouvel éditeur ni de nouvelle bibliothèque d'interface : Radix déjà installé (`menubar`, `dropdown-menu`), `lucide-react`, xterm déjà présent.

## Anti-buts

- Un shell sans le droit `exec`, sans `GLD_WEB_TERMINAL=1`, ou lancé depuis une racine refusée par `ideKnownDir` (home, config Claude, auth de gld).
- Un shell qui hérite de secrets de gld (`GLD_AUTH_KEY`, jeton enfant, clés d'API) : l'environnement du shell est filtré.
- Des shells oubliés qui s'accumulent : tout shell non attaché depuis 30 minutes est supprimé par gld ; une rafale d'ouvertures est bornée.
- Une barre qui contourne un droit (bouton actif en lecture seule) ou capte les touches de Monaco (les raccourcis globaux restent soumis à `isEditableTarget`).
- Une régression du flux de fichiers, des conflits ou des brouillons.

## Contraintes

- **Réutiliser le terminal web existant tel quel.** `GET /term-ws` (gld/termweb.go) attache déjà un client à un panneau tmux (jeton à usage unique, `Origin`, un seul client, lecture seule tant qu'on ne prend pas la main, redimensionnement, inactivité, coupure à la révocation via `watchedRoutes`). Un shell n'est qu'un panneau tmux de plus : les nouvelles routes se contentent de créer/lister/fermer ces panneaux et de délivrer le même jeton `termTok` (même table `termToks`, identifiant de shell à la place de l'identifiant de session Claude). Nouvelles routes, toutes en droit **`exec`**, dans `routeRights`, servies par le home concerné : `POST /shell-open` `{dir, cols, rows, id?}` (crée un shell, ou rattache le shell `id` existant, et renvoie `{ok, token, id, cols, rows}`), `GET /shell-list?dir=` (shells du dossier), `POST /shell-close` `{id}`.
- Le shell vit dans une session tmux **dédiée** `gld-shell-<id>` (`id` = 12 caractères hexadécimaux aléatoires), créée détachée (`tmux new-session -d -s … -c <racine>`), avec l'option de session `@gld_dir=<racine>` pour retrouver ses shells après un rechargement (jamais un nom de session dérivé du chemin). Le serveur tmux est celui de l'opérateur (socket par défaut ; `GLD_RUN_AS` respecté via `applyRunAs`). Avantage assumé : l'opérateur voit aussi ces shells depuis son propre tmux.
- Commande lancée : le shell de l'opérateur (`$SHELL` s'il existe et est exécutable, sinon `/bin/bash`, sinon `/bin/sh`), interactif, via `env -u GLD_AUTH_KEY -u GLD_AUTH_DIR -u GLD_WEB_MAIN_CONFIG -u <variable du jeton enfant> …` (liste en constante, testée) pour qu'un serveur tmux démarré par gld n'hérite d'aucun secret de gld ; répertoire de départ = racine validée par `ideKnownDir` (donc jamais `$HOME`, config Claude, auth de gld) ; `TERM=xterm-256color`.
- Bornes (mon choix, constantes) : 8 shells gld simultanés au total, 4 par espace de travail (HTTP 429 au-delà), un shell non attaché depuis 30 minutes est tué par un balai périodique (`tmux kill-session`), taille 20–500 colonnes et 5–300 lignes (comme `termWS`). Fermer l'onglet dans l'interface ferme le shell (`/shell-close`) ; recharger la page ne le ferme pas.
- Audit : un événement `shell_open` et `shell_close` (utilisateur, base du dossier, durée), jamais les commandes ni la sortie ; relais par en-tête pour les homes enfants comme `X-Gld-Audit` (même mécanisme que l'audit des écritures).
- Interface : textes en `fr` et `en` (`ide.json`), aucune chaîne en dur, composants accessibles au clavier (`menubar`, `toolbar`, `aria-label`, info-bulles avec raccourci), états désactivés selon `fs_write`, `readOnly`, `exec`.
- Monaco reste importé uniquement par le chunk paresseux : `CodeEditor` expose une poignée impérative (`undo`, `redo`, `find`, `replace`, `gotoLine`, `palette`, `setOptions`, abonnements position/sélection/EOL/langage/indentation) ; aucune autre importation de `monaco-editor`.
- Préférences d'affichage (retour à la ligne, minimap, taille de police, arbre/terminal/barre d'état visibles) en `useLocal` globales `gld.ide.view.*`.
- Même discipline que le premier lot : un commit par sujet, français, sujet seul, aucun trailer, jamais de `git push`, tests ciblés, `-p 4` au plus.

## Conception

### Backend (`gld/ide_shell.go`)

`POST /shell-open` : vérifie `termEnabled()`, la racine (`ideKnownDir`), les bornes ; sans `id`, crée la session tmux dédiée (`new-session -d … -c <racine>`, option `@gld_dir`) ; avec `id` (déjà au format 12 hexadécimaux et appartenant bien à cette racine d'après `@gld_dir`), la retrouve ; résout son panneau (`termGeometry`) et enregistre un `termTok` à usage unique de 30 s dans la table existante, l'`id` du jeton étant l'identifiant du shell (ainsi `termBusy` garantit un seul client). Le navigateur ouvre ensuite le `GET /term-ws?t=` **existant**, sans aucun changement. `GET /shell-list` : `tmux list-sessions -F` filtré sur le préfixe `gld-shell-` et sur `@gld_dir == racine`, renvoie `[{id, created, attached}]`. `POST /shell-close` : `kill-session` après vérification du préfixe et de la racine. Balai : goroutine unique qui supprime toute session `gld-shell-*` non attachée depuis plus de 30 min et s'arrête avec le serveur. Tous les noms de session passent par une expression régulière stricte avant toute commande tmux (arguments en tableau, jamais de shell intermédiaire).

### Interface (`gld/ui/src/components/ide/`)

- `BottomPane` : onglets de terminal (`+` nouveau shell, onglets de shell nommés « Terminal 1… », onglet de la session Claude vivante), fermeture d'un onglet = fermeture du shell ; ouverte par défaut si `termInfo.enabled` et droit `exec`, un premier shell est lancé à l'ouverture ; sinon message explicite (désactivé, droit manquant, ou pas de session).
- `ShellTerminal` : réutilise `FullTerminal` (xterm, jeton, resize) avec un `id` de shell à la place de l'`id` de session Claude et `takeHand=true` (un shell est fait pour taper) ; à l'ouverture de l'espace, appelle `/shell-list` et rebranche les shells existants du dossier avant d'en créer un ; si le shell a disparu (tué par le balai ou depuis tmux), message « session terminée » et bouton pour en relancer un. Pas de reconnexion automatique.
- `MenuBar`, `EditorToolbar`, `TreeToolbar`, `StatusBar`, `TrashDialog` ; commandes déclarées une fois dans `commands.ts` (id, libellé i18n, raccourci, droit requis, état) et réutilisées par le menu, les barres, la palette et les raccourcis (un seul endroit).
- La barre d'état lit l'état du flux de surveillance (`useWatch.connected`) : l'interruption silencieuse du flux, jusqu'ici une limite connue, devient visible, avec une reconnexion par `EventSource` fermé puis rouvert avec attente progressive (5 s, 15 s, 30 s).

### Sécurité

`exec` est déjà équivalent au compte système de gld (`chat`, `open`, `git-push`…) : le shell n'élargit pas ce que ce droit permet, il le rend plus direct. Il reste derrière `GLD_WEB_TERMINAL=1` (désactivé par défaut) et les mêmes verrous que le terminal existant (puisque c'est ce même terminal). Particularité de tmux : un shell reste vivant tant qu'il n'est pas fermé ou balayé, donc **un compte qui perd `exec` perd la main sur ses shells mais ceux-ci continuent de tourner** jusqu'à la fermeture par un compte autorisé ou le balai de 30 min — à dire dans `docs/auth.md`. La documentation (`docs/ide.md`, `docs/auth.md`) le dit sans détour.

## Laissé au constructeur

Découpage exact des composants, icônes, ordre des entrées de menu au-delà de la liste donnée, wording `fr`/`en`, valeurs précises des animations/espacements.

## Questions ouvertes pour l'opérateur

Aucune bloquante : les choix ci-dessus sont les miens et se contestent à la relecture (notamment les bornes de shells, « Terminal » à la place d'« Exécuter », le filtre limité aux entrées chargées).
