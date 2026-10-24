# Espace de travail (éditeur dans le navigateur)

L'espace de travail ouvre un dossier de travail de gld dans le navigateur : arborescence, éditeur à onglets (Monaco, le moteur de VS Code) et terminal branché sur une session Claude vivante du dossier. On l'ouvre par le bouton **Espace de travail** de la vue dossier ; l'adresse est `/espace/<dossier absolu encodé>`.

Ce que l'on y fait : parcourir et filtrer le dossier, ouvrir et corriger un fichier, voir les marqueurs git (`M`, `A`, `D`, `R`, `U`, `?`), créer, renommer, supprimer vers une corbeille puis restaurer ou purger, ouvrir un fichier par son nom (`Ctrl+P`), suivre la session Claude du dossier dans la zone terminal. Ce que l'on n'y fait pas : shell libre, exécution ou débogage, recherche multi-fichiers, LSP externe, extensions, diff git (le diff ne sert qu'aux conflits).

Tant que l'espace est affiché, `Ctrl+P` (`Cmd+P`) ouvre l'ouverture rapide où que soit le focus, au lieu de l'impression du navigateur. Les onglets ouverts (chemins seulement, jamais le contenu, 30 au plus) et l'onglet actif sont mémorisés par navigateur et par dossier, puis rouverts au rechargement ; un fichier devenu illisible est sauté sans message. Le contenu non enregistré n'est gardé qu'en `sessionStorage` (brouillons, onglet du navigateur seulement, ~2 Mo au plus, jamais en `localStorage`) : au rechargement un brouillon n'est repris que si le fichier n'a pas changé sur le disque depuis, sinon il est abandonné avec un message ; tant qu'un onglet est modifié, le navigateur demande confirmation avant de quitter ou recharger la page.

## Barres et menu

L'espace se présente comme un petit IDE : un menu en haut, une barre d'actions au-dessus de l'éditeur, une barre d'outils au-dessus de l'arborescence, une barre d'état en bas et la zone terminal entre l'éditeur et la barre d'état. Toutes les actions viennent d'un seul registre de commandes : le menu, les barres et les raccourcis proposent les mêmes, avec les mêmes conditions. Une action impossible n'est jamais masquée : elle est désactivée et dit pourquoi (info-bulle des boutons, ligne sous l'entrée du menu) : aucun onglet, fichier en lecture seule, droit `fs_write` ou `exec` absent, terminal intégré désactivé, rien à enregistrer, arborescence masquée.

- **Menu** : Fichier (nouveau fichier, nouveau dossier, enregistrer, tout enregistrer, fermer l'onglet, fermer tous les onglets, ouverture rapide), Édition (annuler, rétablir), Rechercher (rechercher, remplacer), Affichage (retour à la ligne, minimap, taille du texte −/+/défaut, arborescence, zone terminal, barre d'état), Aller à (ligne, palette de commandes), Terminal (ouvrir ou replier, aller au terminal de session). Il n'y a pas de menu « Exécuter » : gld n'a ni configuration d'exécution ni débogueur. Les raccourcis sont affichés à droite des entrées. Une commande qui agit sur l'éditeur lui rend le focus : après Annuler on continue de taper, après Rechercher ou Aller à la ligne le focus est dans le champ de Monaco. Tout enregistrer, Fermer l'onglet, Fermer tous les onglets et les bascules de zones (arborescence, terminal, barre d'état) rendent le focus au menu : l'éditeur qu'elles ferment ou masquent ne le garde pas. Les raccourcis s'affichent dans la langue de l'interface (`Ctrl+Maj+Z` en français, `Ctrl+Shift+Z` en anglais).
- **Barre de l'éditeur** : enregistrer, tout enregistrer, annuler, rétablir, rechercher, remplacer, aller à la ligne, palette (F1), retour à la ligne automatique, taille du texte −/+, afficher l'arborescence ; à droite, aller au terminal de session, ouvrir ou replier le terminal, fermer tous les onglets. Menu et boutons suivent une seule règle par commande : ceux qui agissent dans l'éditeur (enregistrer, annuler, rétablir, rechercher, remplacer, aller à la ligne, palette, retour à la ligne, taille du texte) lui donnent le focus avant la commande, la saisie de ligne et la palette s'ouvrent donc même si le focus était dans l'arborescence ; tout enregistrer, fermer tous les onglets et les bascules laissent le focus sur le bouton.
- **Barre de l'arborescence** : nouveau fichier et nouveau dossier **à la racine** (le menu contextuel d'un dossier crée dedans), actualiser (relit les dossiers dépliés), tout replier, ouverture rapide, corbeille, et un **filtre par nom**. Le filtre porte sur les entrées **déjà chargées** (dossiers dépliés au moins une fois), garde visibles les dossiers parents d'une entrée trouvée, ne modifie pas l'état déplié mémorisé (effacer le filtre, par la croix ou `Échap`, rend l'arbre tel qu'il était) ; les flèches suivent ce qui est affiché.
- **Corbeille** : la fenêtre liste les entrées supprimées de ce dossier, les plus récentes d'abord, avec **Restaurer** (refusé si le chemin d'origine est occupé : renommez ou supprimez d'abord l'entrée en place) et **Supprimer définitivement** (après confirmation). Sans `fs_write`, la liste reste lisible et les deux actions sont désactivées.
- **Barre d'état** : ligne et colonne du curseur, nombre de caractères sélectionnés, fins de ligne (`LF` ou `CRLF`, et « lecture seule » quand l'onglet l'est), langage, indentation, encodage (toujours UTF-8 : gld refuse les autres), branche git du dépôt le plus proche, état de la surveillance du disque, état de l'index (en cours, prêt, partiel), nombre d'onglets modifiés, bouton du terminal. Sur écran étroit, l'indentation et l'encodage disparaissent et les éléments passent à la ligne.

### Raccourcis

| Raccourci | Effet | Où |
|---|---|---|
| `Ctrl+S` (`Cmd+S`) | Enregistrer l'onglet | partout dans l'espace (jamais « Enregistrer la page » du navigateur) |
| `Ctrl+P` (`Cmd+P`) | Ouverture rapide | partout dans l'espace (jamais l'impression du navigateur) |
| `Ctrl+Z`, `Ctrl+Maj+Z` | Annuler, rétablir | dans l'éditeur |
| `Ctrl+F`, `Ctrl+H` | Rechercher, remplacer | dans l'éditeur |
| `Ctrl+G` | Aller à la ligne | dans l'éditeur |
| `F1` | Palette de commandes de Monaco | dans l'éditeur |
| `Alt+F`, `Alt+E`, `Alt+R`, `Alt+A`, `Alt+L`, `Alt+T` | Ouvrir Fichier, Édition, Rechercher, Affichage, Aller à, Terminal (en anglais : `Alt+F`, `E`, `S`, `V`, `G`, `T`) | hors d'un champ de saisie |
| Flèches, `Entrée`, `Échap` | Parcourir le menu ouvert, choisir, fermer (le focus revient au menu) | menu |

Les raccourcis globaux ne prennent jamais la frappe destinée à Monaco, à xterm ou à un champ : `Alt+lettre` tapé dans l'éditeur reste à l'éditeur. Les raccourcis ne sont pas personnalisables.

### Accessibilité

Le menu est un `menubar` ARIA (flèches pour passer d'un menu à l'autre, `Échap` rend le focus au déclencheur) ; les barres sont des `toolbar` nommées (« Actions de l'éditeur », « Actions de l'arborescence ») ; chaque bouton a un nom accessible, son raccourci en info-bulle (et en `aria-keyshortcuts`) et, s'il est désactivé, la raison reliée par `aria-describedby` (`aria-disabled` : il reste atteignable au clavier). Les bascules (retour à la ligne, arborescence, terminal) exposent leur état (`aria-pressed`, cases cochées dans le menu). Seuls l'état de la surveillance et celui de l'index sont annoncés (`role="status"`) : la position du curseur, qui change à chaque frappe, ne l'est pas.

### Préférences d'affichage

Retour à la ligne, minimap, taille du texte (10 à 28, 13 par défaut) et visibilité de l'arborescence, de la zone terminal et de la barre d'état sont gardés dans le `localStorage` du navigateur, sous les clés `gld.ide.view.*` (une clé par valeur ; une valeur illisible revient à son défaut). Ces préférences valent pour tous les dossiers de ce navigateur ; elles ne suivent pas le compte d'un navigateur à l'autre. La largeur de l'arborescence et la hauteur de la zone terminal sont gardées par dossier (`gld.ide.<dossier>.*`).

## Droits

| Droit | Ouvre |
|---|---|
| `fs_read` | arborescence, lecture des fichiers, index, flux d'évènements, liste de la corbeille |
| `fs_write` | enregistrer, créer, renommer, supprimer (vers la corbeille), restaurer et purger la corbeille. **N'implique pas** `fs_read` : accorder les deux ensemble (sans `fs_read`, l'espace ne s'ouvre pas, l'écriture par l'API reste possible) |
| `exec` | le terminal de l'espace de travail (sans `exec`, la barre Terminal reste visible et dit pourquoi, la zone ne s'ouvre pas) |
| `sessions_read` | avec `exec`, la liste des sessions vivantes du dossier, à laquelle la zone terminal s'attache (`GET /api`) ; sans lui, la barre le dit et rien n'est demandé au serveur |

En mode multi-utilisateur, **aucun de ces droits n'est accordé par défaut**. Les accorder demande une réauthentification, et `fs_write` une confirmation explicite. En mode ouvert (sans compte), tout est ouvert, comme le reste de gld. Voir [auth.md](auth.md).

### Sécurité : aucune liste de refus

C'est un choix assumé : **il n'y a pas de liste de fichiers interdits**. Quiconque a `fs_read` sur un home lit `.env`, les clés privées, `.git/` et tout ce qui se trouve dans le dossier ; `fs_write` permet de les modifier. N'accordez ces droits qu'à des personnes qui pourraient de toute façon lire ce code.

Les garde-fous sont structurels :

- **Racines admises** : seulement un dossier déjà connu de gld (dossier de sessions, dépôt découvert dessous, dossier ajouté à la main ou dossier d'un projet), donné en chemin absolu propre. Sont **refusés comme racine** : `$HOME` et tout ancêtre de `$HOME`, le dossier de configuration de Claude (`CLAUDE_CONFIG_DIR` ou `~/.claude`) **et celui de chaque home** déclaré (un home enfant refuse aussi la config du processus principal, transmise par `GLD_WEB_MAIN_CONFIG`), le dossier d'authentification de gld (`GLD_AUTH_DIR`, sinon `~/.config/gld`), ainsi que tout dossier situé dedans ou qui les contient (comparaison par segments, chemins résolus compris : `~/.claudex` reste admis). Si `$HOME` est introuvable, tout est refusé. Sans cette règle, un compte `fs_read` lirait `auth.db`, `auth.key` ou les identifiants de Claude d'un autre home.
- Chaque chemin est résolu dans un `os.Root` ouvert sur la racine : `..`, chemin absolu, `\` et NUL sont refusés.
- **Aucun lien symbolique n'est suivi**, même vers l'intérieur de la racine : listé comme tel dans l'arbre, jamais ouvert, jamais écrasé.
- Aucun parcours récursif déclenché par une requête : l'arbre se charge un dossier à la fois. La seule descente récursive est l'indexeur, bornée (voir plus bas).
- Le contenu des fichiers n'est jamais journalisé. Avec les comptes activés, chaque écriture réussie (`save`, `create`, `new`, `rename`, `rm`, `restore`, `purge`) ajoute un événement `ide_write` au journal d'`/admin/events` : `<op> <dossier>:<chemin> [octets]` (nom du dossier racine seul, jamais le chemin disque ; octets pour `save`/`create`), plafonné à 300 octets. Un refus ou un conflit n'y figure pas ; un jeton porteur est écrit sans compte. Pour un home autre que le principal, le gld enfant renvoie la ligne déjà nettoyée à la passerelle dans un en-tête interne de réponse, que la passerelle écrit pour l'utilisateur de la requête puis retire avant de répondre ; un client ne peut ni la voir ni la forger. Sans comptes, aucun journal.
- La CSP de gld est inchangée : Monaco et ses workers sont servis par gld (`/assets/`), **jamais par un CDN**.

## Routes

Toutes prennent `dir`, le dossier racine (chemin absolu). `path` est relatif à cette racine, séparé par `/`. Les `POST` prennent un corps JSON et suivent les règles CSRF et d'origine habituelles de gld.

| Route | Droit | Requête | Réponse |
|---|---|---|---|
| `GET /fs-tree` | `fs_read` | `dir`, `path` (vide = racine) | `{entries: [{name, type, size, mtime}], truncated}` ; `type` = `dir`, `file` ou `symlink` ; dossiers d'abord ; au plus **5000 entrées par dossier** (`truncated: true` au-delà) |
| `GET /fs-file` | `fs_read` | `dir`, `path` | `{content, etag, size, mtime}` ; **2 Mio au plus** (`413` au-delà), `415` si binaire (NUL dans les 8 premiers Kio ou UTF-8 invalide) |
| `POST /fs-save` | `fs_write` | `{dir, path, content, etag, force}` | `{ok, etag}` ; `409 {conflict, deleted, etag}` si l'`etag` ne correspond plus (voir Conflits). Contenu de 2 Mio au plus. Écriture atomique (temporaire puis renommage), mode du fichier conservé |
| `POST /fs-new` | `fs_write` | `{dir, path, kind}` (`file` ou `dir`) | `{ok}` ; `409` si l'élément existe |
| `POST /fs-rename` | `fs_write` | `{dir, from, to}` | `{ok}` ; `409` si la cible existe (jamais d'écrasement) |
| `POST /fs-rm` | `fs_write` | `{dir, path}` | `{ok, id}` : l'élément part dans la corbeille |
| `GET /fs-trash` | `fs_read` | `dir` | `{items: [{id, path, mtime}]}` |
| `POST /fs-trash-restore` | `fs_write` | `{dir, id}` | `{ok}` ; `409` si le chemin d'origine est occupé (jamais d'écrasement) |
| `POST /fs-trash-purge` | `fs_write` | `{dir, id}` | `{ok}` : suppression définitive de cette entrée |
| `GET /fs-index` | `fs_read` | `dir` | `{state, files: [{path, size, mtime}]}` ; `state` = `building`, `ready` ou `partial` |
| `POST /fs-files` | `fs_read` | `{dir, paths}` | `{files: [{path, content}]}` : contenu de fichiers **de l'index** seulement, **300 chemins** par requête, **100 Kio** par fichier (les autres sont omis) |
| `GET /fs-watch` | `fs_read` | `dir`, `paths` répété (un par dossier observé ; `paths=` avec une valeur vide = racine ; **aucun** `paths` = rien n'est observé) | flux SSE (voir Observation) |

Erreurs : `404` dossier inconnu ou refusé comme racine, élément absent ; `400` chemin invalide ; `403` lien symbolique ; `409` conflit ou élément existant ; `413` trop gros ; `415` binaire. La raison exacte d'un refus de confinement n'est pas donnée.

`GET /git-changes` renvoie aussi `prefix` : le chemin du dossier demandé relativement à la racine du dépôt (`git rev-parse --show-prefix`), pour placer les marqueurs git quand l'espace de travail est un sous-dossier du dépôt.

## Index

L'index sert à l'ouverture rapide (`Ctrl+P`) et à donner à Monaco les sources TypeScript/JavaScript du projet (imports résolus d'un fichier à l'autre, erreurs de types entre fichiers). Il vit en mémoire, n'est jamais écrit sur disque, et il est libéré environ 2 minutes après la fermeture du dernier espace de travail sur ce dossier.

- **Ignorés** : tout élément dont le nom commence par `.` (`.git`, `.env`, `.gld-trash`…), `vendor`, `node_modules`, et les liens symboliques. Ils restent visibles et ouvrables dans l'arbre ; ils sont seulement hors index.
- **Bornes** : **20 000 entrées, profondeur 12, 5 s**. Au-delà, l'index est marqué `partial` et l'interface le signale.
- Une construction par dossier ; après un resync, la reconstruction peut chevaucher l'ancienne, dont le résultat périmé est écarté. Jamais déclenchée par `/fs-tree` ni `/fs-file`.
- **Alimentation de Monaco** (côté navigateur) : fichiers `.ts .tsx .js .jsx .json` de l'index, **300 fichiers, 100 Kio chacun, 5 Mio au total** au plus, les plus proches du fichier ouvert d'abord.

## Observation du disque

`/fs-watch` utilise `fsnotify` sur les dossiers dépliés dans l'arbre (et ceux de l'index), avec regroupement des rafales (~150 ms). Si inotify échoue ou sature, gld bascule sur un sondage toutes les 3 s.

- Au plus **200 dossiers** par abonnement (`paths` répété). Le navigateur garde l'adresse sous **~6000 octets** : au-delà, il observe moins de dossiers.
- Chaque évènement est `data: {"path": "...", "type": "create|write|remove|rename"}`, jamais le contenu.
- `{"path": "", "type": "resync"}` signifie que des évènements ont pu être perdus (tampon plein, débordement d'inotify, passage au sondage) : le client relit tout ce qu'il affiche (dossiers dépliés, onglets propres), et le prochain `/fs-index` reconstruit l'index.
- Le flux est coupé à la révocation des droits ou de la session, et se termine aussitôt à l'arrêt du serveur.
- Un dossier illisible (droits) est seulement sauté ; seules les limites d'inotify (`ENOSPC`, `EMFILE`, `ENFILE`) font passer au sondage.
- **Reconnexion** : coupure réseau, le navigateur réessaie seul (« Surveillance interrompue » dans la barre d'état). Si le flux est fermé pour de bon (serveur arrêté, réponse autre que `200`, session expirée), l'espace le rouvre lui-même après 5 s, puis 15 s, puis toutes les 30 s ; la barre d'état affiche « Surveillance coupée, reconnexion dans N s » (attente prévue, non décomptée), puis « Surveillance active ». Chaque réouverture vaut resync : l'arbre et les onglets propres sont relus, un onglet modifié dont le fichier a changé affiche le bandeau de conflit.

## Corbeille

Supprimer déplace l'élément dans `<racine>/.gld-trash/<id>/item`, avec son chemin d'origine dans `<racine>/.gld-trash/<id>/from` (`id` : horodatage en nanosecondes). À la première suppression, gld écrit `.gld-trash/.gitignore` contenant `*` : le dossier s'ignore lui-même, sans toucher au `.gitignore` ni à `.git/info/exclude` du projet, et n'apparaît pas dans `git status`. Restaurer remet l'élément à son chemin d'origine et refuse d'écraser. La purge est manuelle, jamais automatique.

## Éditeur

- **Conflits** : chaque onglet garde l'`etag` (mtime en ns et taille) lu à l'ouverture. Si le fichier change sur disque (typiquement l'agent), un onglet propre se recharge seul ; un onglet modifié ne se recharge jamais seul et affiche un bandeau avec **Recharger** (prendre la version du disque), **Écraser** (enregistrer la vôtre, `force: true`) et **Voir les différences** (diff de Monaco, disque à gauche, la vôtre à droite). Un enregistrement qui reçoit `409` affiche le même bandeau.
- **Fins de ligne** : un fichier en `CRLF` ou en `LF` s'édite et s'enregistre avec ses fins de ligne. Un fichier qui **mélange** les fins de ligne s'ouvre en **lecture seule** avec un message, pour ne pas le réécrire en entier.
- Fichiers de plus de 2 Mio ou binaires : ouverts en lecture seule avec un message, sans contenu.
- Thème clair/sombre et polices de gld.

## Limites connues

- Pas de marqueurs git pour une racine connue seulement par les dossiers ajoutés à la main ou ceux d'un projet (`/git-changes` n'accepte que les dépôts des sessions).
- Une instance inotify par espace de travail vivant, gardée jusqu'à 2 minutes après le dernier usage ; la limite par défaut du noyau est de 128 instances par utilisateur (`fs.inotify.max_user_instances`).
- Les changements faits pendant la toute première construction de l'index n'y figurent qu'au prochain resync ou à la prochaine reconstruction.
- Le filtre de l'arborescence ne cherche que dans les dossiers déjà chargés ; pour chercher un fichier n'importe où, utiliser `Ctrl+P`.
- À moins de ~600 px de large, l'arborescence et l'éditeur restent côte à côte : l'éditeur devient étroit et la barre de l'éditeur défile horizontalement.
- Quand l'utilisateur ouvre la zone terminal, même en lecture seule, la fenêtre tmux de l'hôte peut perdre quelques lignes (comportement du terminal web existant : l'attache ajoute un client tmux dont la ligne d'état compte). C'est pourquoi la zone reste repliée tant que l'utilisateur ne l'a pas ouverte lui-même.
- Le journal `ide_write` couvre tous les homes (relais par la passerelle pour les homes enfants) ; il n'est pas soumis au limiteur des événements d'authentification, seulement à la purge à 90 jours.

## Terminal

gld **ne propose volontairement aucun shell libre** : pas de `bash`, pas de pty ouvert pour l'utilisateur, pas de fenêtre tmux créée à sa demande, aucune route qui lance un processus depuis l'espace de travail. Le terminal réutilise `/term-open` et `/term-ws` et s'attache au terminal d'une session Claude **vivante** dont le dossier est celui de l'espace de travail ; pour un shell, ouvrez votre propre terminal.

Une barre « Terminal » est toujours visible au-dessus de la barre d'état, avec l'état de la zone :

| État | Message | Zone |
|---|---|---|
| `GLD_WEB_TERMINAL=1` absent | terminal intégré désactivé (`GLD_WEB_TERMINAL=1`) | fermée, bouton désactivé |
| droit `exec` absent | le terminal demande le droit « exec » | fermée, bouton désactivé ; aucune requête au serveur |
| droit `sessions_read` absent, ou liste des sessions refusée (403) | liste des sessions inaccessible (droit « sessions_read » requis) | fermée, bouton désactivé ; aucune lecture de la liste (après un 403, plus de sondage ; nouvel essai au retour sur l'onglet) |
| liste des sessions injoignable | liste des sessions indisponible | fermée, bouton désactivé ; le sondage continue |
| aucune session vivante dans ce dossier | lancez `claude` dans ce dossier | repliée, peut être dépliée |
| une ou plusieurs sessions vivantes, zone jamais ouverte | N session(s) en direct — ouvrir le terminal | **repliée**, rien n'est attaché |
| une ou plusieurs sessions vivantes, zone ouverte | N session(s) en direct (sélecteur si plusieurs, choix gardé par dossier) | ouverte, en lecture seule |

Règle d'ouverture : la zone ne s'ouvre **jamais seule**. Tant que l'utilisateur ne l'a pas ouverte (bouton de la barre Terminal, menu Terminal ou Affichage, bouton de la barre d'état, « Aller au terminal de session »), elle reste repliée et rien ne s'attache à la session (pas de `/term-open`). Dès qu'il l'a ouverte ou repliée, ce choix est gardé (`gld.ide.view.showTerminal`) et vaut aux visites suivantes. Ouverte, la zone est **en lecture seule** : elle ne prend pas le focus (la frappe reste dans l'éditeur) et n'envoie pas de taille à la fenêtre tmux de l'hôte (l'attache elle-même peut toutefois la réduire de quelques lignes, voir Limites connues) ; le bouton « Lecture seule · prendre la main » donne la main (frappes, taille de la fenêtre, focus). En lecture seule, le terminal garde la taille de l'hôte et peut donc s'afficher tronqué. La liste des sessions vivantes est relue toutes les 10 s, et pas du tout quand l'onglet du navigateur est en arrière-plan.

## Poids et dépendances

- Dépendances ajoutées : `monaco-editor` (npm) et `github.com/fsnotify/fsnotify` (Go).
- Monaco est chargé à la demande, à l'ouverture de l'espace de travail : le bundle principal (`assets/app.js`, ~600 Ko mesurés) ne contient pas Monaco. Monaco et ses workers pèsent **~13 Mo** non compressés, dont **~7 Mo pour le worker TypeScript** (`ts.worker.js`), chargé seulement quand l'éditeur s'ouvre. Le réseau ne paie donc rien tant que l'espace de travail n'est pas ouvert ; ce poids s'ajoute surtout au **binaire Go**, qui embarque `ui/dist` au `make install`. Le code de Monaco est découpé en chunks `assets/monaco1.js` … `monacoN.js` (≈ 500 Ko chacun, 8 aujourd'hui) tous sous la limite de 700 Ko de `chunkSizeWarningLimit` ; la limite reste donc active et signale toute croissance réelle de `assets/app.js`, qui reste l'entrée. Le découpage suit l'ordre d'exécution des modules de Monaco (`monacoSlice` dans `vite.config.ts`) : un chunk n'importe que les précédents. Un découpage par taille seule (`maxSize`) produisait des chunks qui s'importaient mutuellement et laissait une page blanche en production (`TypeError: n is not a constructor`), invisible au build. `npm run build` lance donc `scripts/check-chunks.mjs`, qui échoue sur tout cycle d'imports entre chunks, une entrée `app.js` vidée ou du Monaco chargé avec la page. Après un changement de `vite.config.ts` ou une mise à jour de `monaco-editor`, vérifier aussi dans un navigateur, avec le build de production servi par gld : ouvrir un `.ts`, taper, voir le soulignement d'erreur de TypeScript.
- Les workers (`editor`, `ts`, `json`, `css`, `html`) sont servis sous `/assets/` avec des noms stables (`ts.worker.js`…), même origine. Le petit `assets/editorWebWorkerMain.js` (0,5 Ko) est une copie brute que Vite émet mais que gld ne charge jamais.
