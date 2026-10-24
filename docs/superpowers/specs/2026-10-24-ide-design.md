# IDE web intégré à gld (type Cloud9)

Date : 2026-10-24. Statut : **décisions de l'opérateur prises, document en attente d'approbation**. Les choix marqués « mon choix » sont ceux du rédacteur.

## Décisions de l'opérateur

| Sujet | Décision |
|---|---|
| Architecture | **C. Monaco natif** (éditeur du moteur de VS Code, intégré à gld, sans processus tiers) |
| Droits | Deux nouveaux droits `fs_read` et `fs_write` |
| Fichiers sensibles | **Tout ouvert**, pas de liste de refus. Garde-fous : aucun parcours récursif du disque (pas d'`os.Walk`/`filepath.Walk`) et aucun lien symbolique suivi, sauf l'indexeur ci-dessous |
| Indexation | Oui, du projet ; ignore les dossiers de dépendances (`vendor`, `node_modules`) et tout élément dont le nom commence par `.`. Exception bornée au « pas d'`os.Walk` » (voir Indexation) |
| Suppression | Corbeille `.gld-trash/` dans le projet, ignorée par git |

## Intention

Pendant qu'une session Claude tourne, l'opérateur doit pouvoir, sans quitter gld : parcourir les fichiers du projet, les ouvrir, les corriger à la main, voir ce que l'agent vient de modifier, et lancer une commande dans un terminal. Aujourd'hui gld sait lister, lire des transcriptions, afficher `git-changes` et ouvrir un terminal web (`/term-ws`), mais aucun fichier du projet ne s'édite : il faut un autre outil. L'objectif est un espace de travail « arborescence + éditeur à onglets + terminal » dans le navigateur, rattaché à un projet gld, à la manière de Cloud9.

## Options d'architecture

| | A. IDE léger CodeMirror | B. code-server / OpenVSCode Server derrière gld | **C. Monaco natif (retenu)** |
|---|---|---|---|
| Principe | Arbre + éditeur CodeMirror 6 + terminal existant, API fichiers Go | Processus VS Code serveur, reverse-proxy authentifié par gld, iframe | Comme A, éditeur Monaco |
| Poids | ~300 Ko gzip en plus | Binaire externe de plusieurs centaines de Mo | ~13 Mo non compressés en plus, mesurés (dont ~7 Mo pour le worker TypeScript) |
| Cohérence avec gld | Totale : ACL, home, thème, i18n | Faible : seconde interface, seconde authentification à raccorder, extensions hors contrôle | Totale |
| Surface de sécurité | API fichiers à confiner (maîtrisée) | Un IDE complet exposé, WebSocket opaques, mises à jour à suivre | Idem A |
| Fonctions d'IDE (LSP, débogueur, extensions) | Aucune | Toutes | Aucune (LSP à brancher soi-même) |
| Dépendance à maintenir | 1 bibliothèque front | 1 produit tiers | 1 bibliothèque front |

**Retenu : C.** Même architecture que A (API fichiers Go, ACL gld, terminal existant), avec Monaco comme éditeur : confort d'édition de VS Code (palette `F1`, multi-curseurs, recherche/remplacement, minimap, repliement, diff intégré) sans second produit à héberger. Le coût est le poids (voir Contraintes). B (vrai VS Code serveur) reste écarté : la spec changerait entièrement (proxy, auth, cycle de vie du processus). Le composant éditeur est isolé (`CodeEditor`) pour qu'un changement de moteur ne touche ni l'API ni l'arbre.

La suite spécifie C.

## Buts

1. Une vue **Espace de travail** par projet, ouverte depuis la vue dossier/projet de gld, avec trois zones : arborescence, éditeur à onglets, terminal repliable.
2. Arborescence : lister, ouvrir, créer fichier/dossier, renommer, supprimer (vers une corbeille, pas `rm`), rafraîchie quand le disque change (notamment par l'agent).
3. Éditeur Monaco : coloration syntaxique, recherche/remplacement dans le fichier, palette de commandes, onglets multiples, indicateur de modifications non enregistrées, enregistrement `Ctrl+S`, thème clair/sombre de gld, polices déjà embarquées (Fira Code, JetBrains Mono, Geist Mono).
4. **Détection de conflit** : si le fichier a changé sur disque depuis son ouverture (typiquement l'agent), l'enregistrement est refusé avec choix explicite (recharger, écraser, comparer), jamais d'écrasement silencieux.
5. Marqueurs git dans l'arborescence (modifié, ajouté, non suivi) à partir de `/git-changes` déjà présent, et ouverture d'un fichier modifié par un clic depuis la vue des changements.
6. Terminal : réutilise `/term-open` et `/term-ws` tels quels. Il ne lance pas de shell (comportement de `termweb.go`) : il se branche sur le terminal d'une session Claude **vivante** dont le dossier est celui du projet ; sans session vivante ou sans `GLD_WEB_TERMINAL=1`, la zone explique pourquoi. Un shell libre dans le dossier serait une capacité nouvelle, hors de cette spec.
7. Droits : intégré à l'ACL multi-utilisateur existante ; aucun accès fichier sans droit explicite.
8. i18n : tout texte en `fr` et `en` (clés symétriques, test existant).

## Non-buts

- Pas de LSP externe, de débogueur, de système d'extensions, de tâches/lanceur. Seuls les services de langage embarqués dans Monaco (TypeScript/JavaScript, JSON, CSS, HTML) sont chargés, et ils voient les fichiers TS/JS indexés du projet (voir Indexation), pas le disque entier.
- Pas de recherche/remplacement multi-fichiers dans la première livraison (reste à la charge du terminal : `rg`).
- Pas d'écran de diff git dédié : le diff de Monaco ne sert qu'à comparer la version disque et la version en cours d'édition lors d'un conflit.
- Pas d'édition collaborative temps réel entre utilisateurs.
- Pas d'édition de fichiers binaires ni de fichiers > 2 Mo (ouverts en lecture seule avec message, ou refusés).
- Pas de racine hors projet : jamais d'accès à `~`, à `/` ni aux autres projets depuis cet espace.

## Anti-buts

- Écrire en dehors de la racine du projet (traversée `..`, lien symbolique sortant, chemin absolu, noms exotiques).
- Écraser sans le dire le travail de l'agent, ou l'inverse.
- Ouvrir à un utilisateur ayant seulement `mem_read` ou `sessions_read` la lecture du code source ou de `.env`.
- Faire fuir un secret : le contenu de fichiers n'est jamais journalisé (ni nom de variable, ni extrait).
- Parcourir le disque hors de l'indexeur : un `os.Walk`/`filepath.Walk`, un `find` ou toute descente récursive déclenchée par une requête. L'arbre se charge un dossier à la fois ; la seule descente récursive est l'indexeur (voir Indexation).
- Indexer `vendor/`, `node_modules/` ou tout élément dont le nom commence par `.` ; indexer sans borne (fichiers, profondeur, durée, mémoire).
- Suivre un lien symbolique (sortie de racine, boucle, montage réseau lent).
- Un `.gld-trash/` qui apparaît dans `git status` du projet.
- Un second modèle d'authentification ou de session à côté de celui de gld.

## Contraintes

- **Bibliothèques reconnues** (règle du dépôt) : éditeur `monaco-editor` (paquet npm) ; terminal `@xterm/xterm` déjà présent. Aucun éditeur ni diff écrits à la main : le conflit utilise le `DiffEditor` de Monaco.
- **Monaco servi par gld, jamais par un CDN** (mon choix) : `@monaco-editor/react` charge Monaco depuis jsdelivr par défaut, ce qui violerait la contrainte « aucun script externe ». On utilise le paquet `monaco-editor` local avec ses workers importés par Vite (`?worker`), servis sous `/assets/`, même origine. Seuls les workers `editor`, `ts`, `json`, `css`, `html` sont inclus ; les langages de coloration sont importés à la carte, pas le jeu complet.
- **Poids** : Monaco ajoute plusieurs Mo non compressés. Il est isolé dans un chunk `lazy` chargé à l'ouverture de l'espace de travail ; le bundle principal ne bouge pas. Le binaire Go embarquant `ui/dist`, sa taille augmente d'autant : à mesurer et à consigner dans le CHANGELOG.
- **Go** : confinement via `os.Root` (Go ≥ 1.24, `OpenRoot`) qui interdit les évasions par `..` et liens symboliques, plutôt que `filepath.Clean` + préfixe fait main. Pas de dépendance nouvelle côté serveur.
- Routes enregistrées dans `routeRights` (`gld/auth_acl.go`) : un motif absent est refusé, test existant à compléter.
- Fonctionne en mode multi-home : routes servies par le home enfant concerné comme les autres, jamais par la passerelle.
- Aucun commentaire dans le HTML/bundles servis ; commentaires de source permis s'ils disent un *pourquoi*.
- Builds et tests ciblés (`go test -run`, `vitest run <fichier>`), `-j 4` au plus ; pas de passe globale sans accord.
- Documentation : `docs/ide.md` (il n'y a pas de `docs/reference/`), README et CHANGELOG mis à jour dans le même lot.

## Conception

### API serveur (nouveau fichier `gld/ide.go`)

Toutes relatives à un dossier `dir` (chemin absolu qui doit être un dossier de travail connu de gld : dossier de sessions, dossier ajouté à la main ou dossier d'un projet) ; la racine est ce dossier, ouverte une fois par requête avec `os.OpenRoot`. Sont refusés comme racine `$HOME` et ses ancêtres, le dossier de configuration de Claude et le dossier d'auth de gld (et tout dossier dedans ou qui les contient).

| Route | Rôle | Droit |
|---|---|---|
| `GET /fs-tree?dir=&path=` | enfants **directs** d'un dossier (nom, type, taille, mtime), tri dossiers d'abord, un seul `ReadDir`, jamais récursif ; un lien symbolique est listé comme tel (`type: symlink`, cible non résolue) et non ouvrable | `fs_read` |
| `GET /fs-file?dir=&path=` | contenu texte + `etag` (mtime-ns + taille) ; refus au-delà de 2 Mo ou si binaire | `fs_read` |
| `GET /fs-index?dir=` | liste plate des chemins indexés (`path`, `size`, `mtime`) + `state` (`building`, `ready`, `partial`) ; ne déclenche jamais de parcours, lit l'index en mémoire | `fs_read` |
| `POST /fs-files` | `{paths[]}` : contenu de plusieurs fichiers indexés d'un coup (pour alimenter l'éditeur), mêmes plafonds que `/fs-file` | `fs_read` |
| `POST /fs-save` | `{path, content, etag}` ; écriture atomique (fichier temporaire + rename, permissions conservées) ; `409` si l'`etag` diffère | `fs_write` |
| `POST /fs-new`, `/fs-rename`, `/fs-rm` | création, renommage ; suppression = déplacement dans `<projet>/.gld-trash/` horodaté (restaurable, voir Corbeille) | `fs_write` |
| `GET /fs-trash` | liste de la corbeille (`id`, chemin d'origine, date) | `fs_read` |
| `POST /fs-trash-restore`, `/fs-trash-purge` | restauration (sans écrasement) et purge d'une entrée de la corbeille | `fs_write` |
| `GET /fs-watch?dir=&paths=a&paths=b` | flux SSE d'événements de changement (voir ci-dessous) ; un paramètre `paths` répété par dossier déclaré (vide = racine, 200 au plus) | `fs_read` |

Le terminal garde `/term-open` et `/term-ws` (droit `exec`).

### Nouveaux droits (mon choix)

`fs_read` et `fs_write`, ajoutés à `allRights`. Ils sont **distincts** de `mem_*` et de `exec`, car lire le code d'un projet n'est pas lire sa mémoire, et écrire un fichier est à égalité de gravité avec `config_write` (le code écrit sera exécuté par Claude ou par l'opérateur). `fs_write` implique `fs_read`. L'espace de travail complet exige en plus `exec` pour le terminal ; sans `exec` l'onglet terminal n'apparaît pas. Mode mono-utilisateur : tout est ouvert, comme le reste de gld.

### Indexation

**À quoi elle sert** : (1) ouverture rapide d'un fichier par son nom (`Ctrl+P`) sans déplier l'arbre ; (2) navigation entre fichiers TS/JS (aller à la définition, autocomplétion des imports, erreurs de types entre fichiers) en donnant à Monaco les sources du projet.

**Ce qui est ignoré** (mon choix pour la liste exacte, la règle est la vôtre) :
- dossiers de dépendances : `vendor`, `node_modules` ;
- tout élément — dossier ou fichier — dont le nom commence par `.` (`.git`, `.github`, `.env`, `.gld-trash`, `.vscode`…). Ces éléments restent visibles et ouvrables dans l'arbre ; ils sont seulement hors index ;
- liens symboliques (jamais suivis).

**Comment** (c'est la seule descente récursive de gld, donc très encadrée) :
- Goroutine unique par projet, une construction à la fois (les demandes concurrentes attendent la même), lancée à l'ouverture de l'espace de travail, jamais par `/fs-tree` ni `/fs-file`.
- Parcours itératif (pile explicite) avec `ReadDir` sur `os.Root`, type lu sans suivre les liens (`DirEntry.Type()`), pas de `filepath.Walk`.
- Bornes (mon choix) : 20 000 entrées, profondeur 12, 5 s de temps cumulé. Au-delà : index marqué `partial` et signalé dans l'interface, jamais un échec silencieux. Ces seuils sont des constantes réglables.
- L'index vit en mémoire (chemin, taille, mtime), rien n'est écrit sur disque, rien n'est partagé entre projets, libéré à la fermeture de l'espace de travail.
- Mise à jour incrémentale par l'observation du disque (voir plus bas), pour les dossiers connus de l'index ; un dossier créé est ajouté seulement s'il n'est pas ignoré.
- Le contenu n'est pas indexé côté serveur : pas de recherche plein texte (non-but maintenu).

**Alimentation de Monaco** : une fois l'index prêt, le front charge en arrière-plan, par lots via `/fs-files`, les sources `.ts .tsx .js .jsx .json` et `.d.ts` de l'index (≤ 100 Ko chacune, au plus 300 fichiers et 5 Mo au total — mon choix) et les enregistre comme modèles/bibliothèques du service TypeScript de Monaco. Priorité aux fichiers proches du fichier ouvert. Au-delà du budget, les fichiers restants ne sont chargés que s'ils sont ouverts. Aucun contenu hors index n'est lu.

**Droit** : `fs_read`. Un utilisateur sans ce droit ne déclenche ni n'obtient d'index.

### Observation du disque

Surveillance côté Go par `fsnotify` (bibliothèque reconnue), limitée aux dossiers actuellement dépliés dans l'arbre (le front les déclare), avec regroupement des rafales (~150 ms). Si `fsnotify` échoue ou atteint la limite d'inotify, repli sur un sondage de la liste des dossiers ouverts toutes les 3 s. L'événement porte seulement `{path, type}`, jamais le contenu.

### Front (`gld/ui/src/components/ide/`)

- `IdeView` : volets redimensionnables (arbre / éditeur / terminal), état (onglets ouverts, dossiers dépliés, ratio) mémorisé par projet dans `/tabs-save` existant plutôt que dans un nouveau stockage.
- `FileTree`, `EditorTabs`, `CodeEditor` (seul module important `monaco-editor`, chargé en `lazy` : le bundle principal de gld ne grossit pas). Thème Monaco synchronisé sur `next-themes`, police choisie parmi celles déjà embarquées.
- Conflit : bandeau « modifié sur le disque » avec trois actions (Recharger, Écraser, Voir les différences). Un onglet sale dont le fichier change sur disque ne se recharge jamais seul.
- Fichier modifié par l'agent et onglet propre : rechargement silencieux avec toast discret.
- Fichier qui mélange les fins de ligne : ouvert en lecture seule avec un message (l'enregistrer le réécrirait en entier).
- Route front `/espace/<dossier absolu encodé>` (la racine de l'API est un dossier, pas un nom de projet ; cohérent avec `/projet/`, `/dossier/`), ajoutée au `routeRights` en `rightPublic` comme les autres pages statiques.

### Sécurité

- Tout chemin est résolu **dans** `os.Root` ; une erreur d'évasion donne `403` sans préciser la raison.
- **Aucune liste de refus** (décision de l'opérateur) : `.env`, clés et `.git/` sont ouvrables et modifiables par quiconque a `fs_read`/`fs_write`. C'est pourquoi ces droits sont séparés et ne s'accordent pas par défaut. Les limites sont structurelles : pas de parcours récursif (aucun `os.Walk`), un dossier par requête ; liens symboliques jamais suivis (ni listés comme dossiers, ni ouverts, ni écrasés : `Lstat`, ouverture refusée sur `ModeSymlink`).
- Écriture atomique et conservation du mode du fichier ; refus d'écrire dans un lien symbolique, y compris vers l'intérieur de la racine (plus strict qu'`os.Root`, qui ne bloque que la sortie).
- Taille de requête plafonnée (2 Mo + marge), limitation de débit sur `/fs-save` réutilisant l'infrastructure de `auth_throttle.go`.
- Journal d'audit des écritures (`fs_write`, chemin, utilisateur, octets, jamais le contenu) dans la table d'événements de `/admin/events`.
- CSP et en-têtes de gld inchangés ; l'éditeur ne charge aucun script externe, workers inclus (même origine).

### Tests

- Go : indexeur (ignore `vendor`, `node_modules` et les noms en `.` à tous les niveaux ; ne suit pas un lien vers un dossier ni vers l'extérieur ; respecte les bornes et passe en `partial` ; une seule construction concurrente ; mise à jour incrémentale) ; confinement (`..`, lien sortant, chemin absolu, nom avec NUL), conflit `409`, écriture atomique (échec à mi-parcours ne corrompt pas), droits par route (test d'exhaustivité de `routeRights` existant), corbeille/restauration, plafonds de taille.
- Front : arbre (dépliage paresseux), onglet sale, bandeau de conflit, symétrie `fr`/`en`, absence de clés brutes dans le DOM.
- Fuzz sur la résolution de chemin (le dépôt a déjà du fuzzing).

### Corbeille

`/fs-rm` déplace l'élément vers `<projet>/.gld-trash/<id>/item` et écrit son chemin d'origine dans `<projet>/.gld-trash/<id>/from` (`id` : horodatage en nanosecondes). À la première création, gld écrit `.gld-trash/.gitignore` contenant `*` (mon choix) : le dossier s'ignore lui-même, sans toucher au `.gitignore` ni à `.git/info/exclude` du projet. Une restauration remet le fichier à son chemin d'origine et refuse d'écraser un fichier existant. Le purge est manuel (action explicite dans l'arbre), jamais automatique.

## Hors périmètre de la première livraison

Recherche multi-fichiers, onglets de diff git, raccourcis personnalisables, import/export de configuration d'éditeur, services de langage au-delà de TS/JSON/CSS/HTML. Chacun peut s'ajouter sans changer l'API ci-dessus.

## Laissé au constructeur

Découpage exact des composants, détail visuel des volets, liste précise des langages de coloration importés, ordre de livraison (suggéré : API + arbre + éditeur sans conflit, puis conflit + observation, puis marqueurs git, puis terminal), libellés `fr`/`en`.
