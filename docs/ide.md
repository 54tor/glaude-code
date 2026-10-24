# Espace de travail (éditeur dans le navigateur)

L'espace de travail ouvre un dossier de travail de gld dans le navigateur : arborescence, éditeur à onglets (Monaco, le moteur de VS Code) et terminal branché sur une session Claude vivante du dossier. On l'ouvre par le bouton **Espace de travail** de la vue dossier ; l'adresse est `/espace/<dossier absolu encodé>`.

Ce que l'on y fait : parcourir le dossier, ouvrir et corriger un fichier, voir les marqueurs git (`M`, `A`, `D`, `R`, `U`, `?`), créer, renommer, supprimer vers une corbeille, ouvrir un fichier par son nom (`Ctrl+P`). Ce que l'on n'y fait pas : recherche multi-fichiers, LSP externe, débogueur, extensions, diff git (le diff ne sert qu'aux conflits).

## Droits

| Droit | Ouvre |
|---|---|
| `fs_read` | arborescence, lecture des fichiers, index, flux d'évènements, liste de la corbeille |
| `fs_write` | enregistrer, créer, renommer, supprimer (vers la corbeille), restaurer et purger la corbeille. Implique `fs_read` |
| `exec` | le terminal de l'espace de travail (sans `exec`, la zone terminal n'apparaît pas) |

En mode multi-utilisateur, **aucun de ces droits n'est accordé par défaut**. Les accorder demande une réauthentification, et `fs_write` une confirmation explicite. En mode ouvert (sans compte), tout est ouvert, comme le reste de gld. Voir [auth.md](auth.md).

### Sécurité : aucune liste de refus

C'est un choix assumé : **il n'y a pas de liste de fichiers interdits**. Quiconque a `fs_read` sur un home lit `.env`, les clés privées, `.git/` et tout ce qui se trouve dans le dossier ; `fs_write` permet de les modifier. N'accordez ces droits qu'à des personnes qui pourraient de toute façon lire ce code.

Les garde-fous sont structurels :

- **Racines admises** : seulement un dossier déjà connu de gld (dossier de sessions, dépôt découvert dessous, dossier ajouté à la main ou dossier d'un projet), donné en chemin absolu propre. Sont **refusés comme racine** : `$HOME` et tout ancêtre de `$HOME`, le dossier de configuration de Claude (`CLAUDE_CONFIG_DIR` ou `~/.claude`) et le dossier d'authentification de gld (`GLD_AUTH_DIR`, sinon `~/.config/gld`), ainsi que tout dossier situé dedans ou qui les contient. Sans cette règle, un compte `fs_read` lirait `auth.db`, `auth.key` ou les identifiants de Claude.
- Chaque chemin est résolu dans un `os.Root` ouvert sur la racine : `..`, chemin absolu, `\` et NUL sont refusés.
- **Aucun lien symbolique n'est suivi**, même vers l'intérieur de la racine : listé comme tel dans l'arbre, jamais ouvert, jamais écrasé.
- Aucun parcours récursif déclenché par une requête : l'arbre se charge un dossier à la fois. La seule descente récursive est l'indexeur, bornée (voir plus bas).
- Le contenu des fichiers n'est jamais journalisé.
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
| `GET /fs-watch` | `fs_read` | `dir`, `paths` répété (un par dossier observé, vide = racine) | flux SSE (voir Observation) |

Erreurs : `404` dossier inconnu ou refusé comme racine, élément absent ; `400` chemin invalide ; `403` lien symbolique ; `409` conflit ou élément existant ; `413` trop gros ; `415` binaire. La raison exacte d'un refus de confinement n'est pas donnée.

`GET /git-changes` renvoie aussi `prefix` : le chemin du dossier demandé relativement à la racine du dépôt (`git rev-parse --show-prefix`), pour placer les marqueurs git quand l'espace de travail est un sous-dossier du dépôt.

## Index

L'index sert à l'ouverture rapide (`Ctrl+P`) et à donner à Monaco les sources TypeScript/JavaScript du projet (imports résolus d'un fichier à l'autre, erreurs de types entre fichiers). Il vit en mémoire, n'est jamais écrit sur disque, et il est libéré environ 2 minutes après la fermeture du dernier espace de travail sur ce dossier.

- **Ignorés** : tout élément dont le nom commence par `.` (`.git`, `.env`, `.gld-trash`…), `vendor`, `node_modules`, et les liens symboliques. Ils restent visibles et ouvrables dans l'arbre ; ils sont seulement hors index.
- **Bornes** : **20 000 entrées, profondeur 12, 5 s**. Au-delà, l'index est marqué `partial` et l'interface le signale.
- Une seule construction à la fois par dossier ; jamais déclenchée par `/fs-tree` ni `/fs-file`.
- **Alimentation de Monaco** (côté navigateur) : fichiers `.ts .tsx .js .jsx .json` de l'index, **300 fichiers, 100 Kio chacun, 5 Mio au total** au plus, les plus proches du fichier ouvert d'abord.

## Observation du disque

`/fs-watch` utilise `fsnotify` sur les dossiers dépliés dans l'arbre (et ceux de l'index), avec regroupement des rafales (~150 ms). Si inotify échoue ou sature, gld bascule sur un sondage toutes les 3 s.

- Au plus **200 dossiers** par abonnement (`paths` répété). Le navigateur garde l'adresse sous **~6000 octets** : au-delà, il observe moins de dossiers.
- Chaque évènement est `data: {"path": "...", "type": "create|write|remove|rename"}`, jamais le contenu.
- `{"path": "", "type": "resync"}` signifie que des évènements ont pu être perdus (tampon plein, débordement d'inotify, passage au sondage) : le client relit tout ce qu'il affiche (dossiers dépliés, onglets propres), et le prochain `/fs-index` reconstruit l'index.
- Le flux est coupé à la révocation des droits ou de la session.

## Corbeille

Supprimer déplace l'élément dans `<racine>/.gld-trash/<id>/item`, avec son chemin d'origine dans `<racine>/.gld-trash/<id>/from` (`id` : horodatage en nanosecondes). À la première suppression, gld écrit `.gld-trash/.gitignore` contenant `*` : le dossier s'ignore lui-même, sans toucher au `.gitignore` ni à `.git/info/exclude` du projet, et n'apparaît pas dans `git status`. Restaurer remet l'élément à son chemin d'origine et refuse d'écraser. La purge est manuelle, jamais automatique.

## Éditeur

- **Conflits** : chaque onglet garde l'`etag` (mtime en ns et taille) lu à l'ouverture. Si le fichier change sur disque (typiquement l'agent), un onglet propre se recharge seul ; un onglet modifié ne se recharge jamais seul et affiche un bandeau avec **Recharger** (prendre la version du disque), **Écraser** (enregistrer la vôtre, `force: true`) et **Voir les différences** (diff de Monaco, disque à gauche, la vôtre à droite). Un enregistrement qui reçoit `409` affiche le même bandeau.
- **Fins de ligne** : un fichier en `CRLF` ou en `LF` s'édite et s'enregistre avec ses fins de ligne. Un fichier qui **mélange** les fins de ligne s'ouvre en **lecture seule** avec un message, pour ne pas le réécrire en entier.
- Fichiers de plus de 2 Mio ou binaires : ouverts en lecture seule avec un message, sans contenu.
- Thème clair/sombre et polices de gld.

## Terminal

Le terminal **ne lance pas de shell**. Il réutilise `/term-open` et `/term-ws` et s'attache au terminal d'une session Claude **vivante** dont le dossier est celui de l'espace de travail. Il exige le droit `exec` et `GLD_WEB_TERMINAL=1` ; sans session vivante ou sans cette variable, la zone explique pourquoi.

## Poids et dépendances

- Dépendances ajoutées : `monaco-editor` (npm) et `github.com/fsnotify/fsnotify` (Go).
- Monaco est chargé à la demande, à l'ouverture de l'espace de travail : le bundle principal (`assets/app.js`, ~650 Ko) ne bouge pas. Monaco et ses workers pèsent **~13 Mo** non compressés, dont **~7 Mo pour le worker TypeScript** (`ts.worker.js`), chargé seulement quand l'éditeur s'ouvre. Le réseau ne paie donc rien tant que l'espace de travail n'est pas ouvert ; ce poids s'ajoute surtout au **binaire Go**, qui embarque `ui/dist` au `make install`.
- Les workers (`editor`, `ts`, `json`, `css`, `html`) sont servis sous `/assets/` avec des noms stables (`ts.worker.js`…), même origine. Le petit `assets/editorWebWorkerMain.js` (0,5 Ko) est une copie brute que Vite émet mais que gld ne charge jamais.
