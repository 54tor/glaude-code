# Suivi : migrer les accès fichiers de la passerelle vers `os.OpenRoot`

Ce travail conditionne la sortie de l'état « expérimental » de `GLD_RUN_AS` (voir [auth.md](auth.md#gld_run_as--séparation-par-compte-os-expérimental)).

## Pourquoi

Sous `GLD_RUN_AS`, la passerelle tourne en root et lance les sessions sous un compte moins privilégié. Mais elle continue de lire et d'écrire elle-même, avec des appels `os.*` qui suivent les liens symboliques, dans des dossiers que ce compte peut modifier : le dossier de configuration (`cfgDir()`), `projects/`, les dossiers des homes et les dossiers de projet (`cwd` des sessions). Le compte cible peut donc y poser un lien symbolique vers un fichier quelconque et attendre que root le lise ou l'écrase.

Opérations dangereuses :

- **`chmod` par nom** (par exemple `writeConfig` après création) : il suit un lien posé entre la création et le `chmod`, et change le mode d'un fichier arbitraire.
- **Copier le mode d'un fichier contrôlé par la cible** (sauvegardes, synchronisation entre homes, restauration) : le fichier créé par root reprend des droits choisis par la cible.
- **Lire en root un fichier atteint par un lien** (`configRawRead`, lecture de transcriptions, mémoire, corbeilles) : le contenu d'un fichier de root fuit vers la cible.
- **Écrire, renommer ou supprimer par nom** (`WriteFile`, `Rename`, `Remove`, `RemoveAll`, `MkdirAll`) : le lien est suivi, root écrase ou efface ce qu'il vise.

## Approche recommandée

- Ouvrir chaque dossier de confiance une fois avec `os.OpenRoot` et passer par les méthodes de la racine (`Open`, `OpenFile`, `Create`, `Mkdir`, `Remove`, `Rename`, `Lstat`), qui refusent de sortir de la racine ou de suivre un lien qui en sort.
- Remplacer `chmod` par nom par un `chmod` sur le descripteur ouvert (`File.Chmod`).
- Ne jamais recopier le mode d'un fichier contrôlé par la cible : fixer le mode explicitement.
- Pour les parcours (`filepath.WalkDir`), parcourir via la racine ouverte et ignorer les liens.
- Écrire un test par site avec un lien symbolique planté.

## Liste des sites

Relevée par `grep` des appels `os.*` et `filepath.WalkDir` dans les fichiers non-test, hors `auth_*`, `runas_*`, `owner_*`, `i18n*`, `listen.go`, `notify.go`, au commit `cf8136a`. Les numéros de ligne dérivent avec le code : relancez le relevé avant de travailler, et vérifiez chaque site (le relevé a quelques faux positifs ; `trash.go:25` n'en est pas un, voir plus bas). Exclus car exécutés sous la cible ou hors des dossiers modifiables : `sendkeys.go:69` (/proc), `term.go:160,169`, `185,186`, `loaded.go:53,61,64`, `searchidx.go:46`, `term.go:143` (gardé par `specPath`).

### Écritures, renommages et suppressions (prioritaires)

- trash.go:25 : `journal()` ouvre `gld-web.log` dans `cfgDir()` en suivant les liens symboliques ; un lien posé par la cible (vers `/etc/ld.so.preload` par exemple) fait écrire root où elle veut, jusqu'à l'exécution de code en root. Ce n'est pas un faux positif.
- configwrite.go:191, 195, 218, 240, 252, 259, 267 (chmod après création : course), 268, 271, 272 : `writeConfig` et sauvegardes `gld-web-config-backups`
- claude.go:91 : `RemoveAll(workPath)` ; ce dossier peut retomber sur `cfgDir()` (claude.go:81)
- commands.go:198
- extra.go:152, 173, 339
- tabs.go:93
- memory.go:281
- homes.go:131
- notes.go:44
- reassign.go:41, 44, 49, 76, 105, 177, 191, 196
- homesync.go:431
- images.go:56, 62, 69, 78
- keepwarm.go:68
- tokens.go:58
- trash.go:25, 53, 78, 82, 85, 127, 133, 138
- purge.go:72, 261, 264, 265
- skills.go:386, 426, 429, 494
- trashsessions.go:54, 59, 64, 65, 69, 168, 172, 176, 180, 193

### Lectures et parcours (fuite de fichiers de la passerelle par un lien posé par la cible)

- configraw.go:83, 103 (`configRawRead`)
- auth_acl.go:357 (garde de `POST /trash-restore`) : lit `meta.json` de l'élément dans la corbeille du home visé (`gld-web-trash/<id>/meta.json`) pour décider du droit exigé, en suivant les liens symboliques ; un lien posé par la cible fait lire un fichier arbitraire à la passerelle.
- sendkeys.go:158 (`closeSession`) : le PID vient d'un relevé de `claude agents --json`. `runAsOwns(pid)` (runas_unix.go) refuse un processus qui n'appartient pas au compte cible (quatre uid de `/proc/<pid>/status`, Linux seulement), mais la course entre cette lecture et le signal n'est pas fermée (`pidfd` hors périmètre).
- configwrite.go:208, 295, 337, 365, 372 (restauration d'une sauvegarde)
- account.go:85, 134
- matin.go:84
- cli.go:167
- commands.go:114, 131, 145
- configproj.go:51, 57
- extra.go:214
- tabs.go:72
- memory.go:156, 194
- loaded.go:92
- homes.go:82
- gitstatus.go:195
- convpage.go:19
- config.go:221
- configperm.go:147, 189
- notes.go:28
- reassign.go:70, 162
- memmeta.go:366
- homesync.go:63, 107, 114, 137, 144, 157, 308
- configcheck.go:225, 371, 388
- slashcmds.go:21, 39
- images.go:59, 86
- keepwarm.go:48
- tokens.go:44
- trash.go:90, 93, 104
- sessionlive.go:163, 185, 372
- transcript.go:75, 143, 288, 457
- routes_sessions.go:194
- purge.go:94, 108, 234, 291
- skills.go:102, 196, 236, 250, 266, 304, 342, 474
- trashsessions.go:88, 109, 269
