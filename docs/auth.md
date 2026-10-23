# Multi-utilisateur : comptes, droits par home, double authentification

Par défaut, Glaude Code n'a qu'un jeton (`GLD_WEB_TOKEN`) : qui l'a voit tout. Le mode multi-utilisateur est **optionnel** et permet à plusieurs personnes de se connecter chacune avec leur compte, avec un second facteur obligatoire et des droits indépendants par home (dossier `CLAUDE_CONFIG_DIR`).

Lisez d'abord la section [Ce que les droits protègent, et ce qu'ils ne protègent pas](#ce-que-les-droits-protègent-et-ce-quils-ne-protègent-pas).

## Activer

Rien ne change tant qu'aucun compte n'existe : pas de base, pas de clé, comportement identique à l'ancien. Le mode s'active à la création du premier compte, en ligne de commande, jamais depuis l'interface web :

```sh
gld user add --admin alice
```

Le mot de passe est demandé deux fois, sans écho. Sans terminal (scripts, tests), `GLD_USER_PASSWORD_STDIN=1` lit une seule ligne de l'entrée standard. Minimum 12 caractères, refus des mots de passe de la liste embarquée des plus courants.

Le serveur en cours d'exécution le remarque au plus tard 2 secondes après la création du compte (le contrôle est mis en cache pendant cette fenêtre : pendant ces 2 s il reste ouvert, avec le jeton toujours exigé s'il est configuré). Dès lors, le formulaire de connexion remplace le jeton pour le navigateur.

Un administrateur n'a aucun accès à un home par défaut. Donnez-vous des droits :

```sh
gld user grant alice main mem_read mem_write usage_read sessions_read
```

`main` désigne le home principal ; pour un autre, donnez son nom (gld le convertit en identifiant). Autres commandes (utilisables sans serveur, elles écrivent dans la même base) :

| Commande | Effet |
|---|---|
| `gld user add [--admin] <nom>` | crée un compte |
| `gld user list` | liste les comptes (administrateur, désactivé, ligne altérée) |
| `gld user disable\|enable <nom>` | désactive ou réactive |
| `gld user passwd <nom>` | change le mot de passe |
| `gld user reset-mfa <nom>` | efface TOTP, clés de sécurité et codes de secours, ferme les sessions |
| `gld user grant [--yes] <nom> <home> <droits…>` | accorde des droits |
| `gld user revoke <nom> <home> <droits…>` | retire des droits |

L'écran « Utilisateurs » de l'interface (administrateurs) fait la même chose ensuite.

## Où sont les données

Dossier d'authentification : `GLD_AUTH_DIR` s'il est fixé, sinon le dossier de configuration de l'OS suivi de `gld` (Linux `~/.config/gld`, macOS `~/Library/Application Support/gld`, Windows `%AppData%\gld`). Il est en 0700 et contient `auth.db` (SQLite, 0600). Il est distinct de `CLAUDE_CONFIG_DIR` : sauvegarder ou synchroniser un home n'emporte pas les comptes. Gld refuse un dossier ou un fichier qui est un lien symbolique, n'appartient pas à l'utilisateur courant, ou est lisible par d'autres (hors Windows).

La base n'est créée qu'au premier `gld user add`. Une fois des comptes vus, une base supprimée, remplacée ou illisible fait **refuser le service** (503) jusqu'au redémarrage : jamais de retour silencieux au mode ouvert. Au démarrage, une base inutilisable arrête le serveur.

### Clé maîtresse

Les secrets TOTP sont chiffrés (AES-256-GCM) et chaque ligne sensible est signée avec une clé maîtresse de 32 octets. Ordre de recherche :

1. `GLD_AUTH_KEY` (64 caractères hexadécimaux) ;
2. le coffre-fort de l'OS (service `gld`) ;
3. sinon, un fichier `auth.key` (0600) à côté de la base, créé à la première utilisation. Il protège moins qu'un coffre-fort : qui lit le dossier lit la clé.

Si la clé ne correspond pas à la base, gld refuse de démarrer (et la ligne de commande `gld user …` refuse aussi) ; il ne régénère jamais une clé en silence. **Perte de la clé** : `gld user reset-mfa` ne peut pas aider, puisqu'elle exige d'ouvrir la base. Retrouvez la clé (sauvegarde, `GLD_AUTH_KEY` d'origine). Sinon, déplacez `auth.db` (et `auth.key`) hors du dossier, puis recréez le premier compte avec `gld user add --admin` : tous les comptes, facteurs et droits sont à refaire.

## Les droits

Sept droits, indépendants, par couple (compte, home). Un droit absent vaut refusé. Chaque route est classée dans une table unique (`routeRights`, `gld/auth_acl.go`) ; un test échoue si une route n'y figure pas.

| Droit | Ouvre |
|---|---|
| `mem_read` | mémoire, règles, snippets, notes, corbeille (lecture), santé, lecture du home (configuration, état git, onglets, commandes) |
| `mem_write` | écrire, déplacer, fusionner, supprimer du texte inerte : CLAUDE.md, règles, mémoire, notes, snippets, corbeille |
| `mem_sync` | prévisualiser et appliquer la synchronisation entre homes ; exige `mem_read` sur la source et `mem_write` sur la destination, vérifiés home par home. Appliquer des réglages, permissions, skills ou commandes exige en plus `config_write` sur la destination |
| `usage_read` | compte et usages, jamais le contenu des conversations |
| `sessions_read` | transcriptions et état des sessions, recherche (`/search`) et `/info` (qui renvoient des extraits de prompts) |
| `config_write` | réglages et skills (hooks, serveurs MCP, permissions), édition brute, restauration |
| `exec` | chat, nouvelle session, terminal web, envoi de touches, confiance d'un dossier, push git, fermeture, renommage, suppression et corbeille de **sessions**, et `/ls`, `/dirs` (parcours du disque) |

Notes :

- `GET /homes` et `POST /homes-select` ne montrent que les homes où le compte a au moins un droit.
- Créer ou retirer un home (`/homes-save`) est réservé aux administrateurs. Le nom `main` est réservé.
- Les droits sont rattachés à l'**identifiant** d'un home, pas à son nom. Supprimer un home supprime ses droits en cascade. Renommer un home via `/homes-save` en omettant son identifiant en crée un nouveau, donc fait perdre ses droits. Échanger les dossiers de deux homes déplace les droits avec les noms.
- Un administrateur gère les comptes et les droits mais n'a **aucun accès implicite** aux homes : il s'accorde les droits qu'il veut.
- Le jeton (Bearer) donne tous les droits d'un home mais jamais l'administration, ni les routes `/auth/*` et `/admin/*`.

### Ce que les droits protègent, et ce qu'ils ne protègent pas

**`exec` et `config_write` équivalent au compte système qui fait tourner gld.** Écrire un hook, un serveur MCP ou un skill, ou ouvrir le chat ou le terminal web, exécute du code sous cet utilisateur : la personne peut lire et écrire `auth.db`, `auth.key` et le coffre-fort. Les droits protègent l'interface, pas la machine. N'accordez ces deux droits qu'à des personnes de confiance ; ni l'écran d'administration ni `gld user grant` (confirmation, `--yes` pour automatiser) ne vous en dispensent. Un co-utilisateur limité à la mémoire, aux usages et à la lecture des sessions n'a aucun chemin vers la base.

## Connexion

1. `POST /auth/login` : nom et mot de passe, vérifiés en temps constant (un nom inconnu coûte autant qu'un nom existant, réponse identique).
2. Second facteur, **obligatoire** : code TOTP, clé de sécurité WebAuthn (clé matérielle, passkey) ou l'un des 10 codes de secours (à usage unique, affichés une fois). Tant qu'il manque, seules les routes `/auth/*` répondent.
3. Un compte sans aucun facteur en enrôle un à la première connexion (TOTP ou clé de sécurité), et reçoit ses codes de secours. **Attention** : cet enrôlement n'exige que le mot de passe. Quiconque connaît le mot de passe d'un compte encore sans facteur peut y enrôler le sien. Créez donc le compte juste avant la première connexion de la personne (ou enrôlez-la vous-même), et ne laissez pas traîner un compte sans facteur. `gld user reset-mfa` ramène un compte à cet état.
4. Ajouter ou retirer un facteur (un compte garde toujours au moins un facteur), changer son mot de passe ou régénérer ses codes demande une réauthentification (mot de passe plus code ou assertion), valable 5 minutes. La liste des sessions du compte (`GET /auth/sessions`) n'en demande pas.

Sessions : cookie `gld_session` (valeur aléatoire de 32 octets, stockée hachée), `HttpOnly`, `SameSite=Strict`, `Secure` quand la requête arrive en HTTPS (directement, ou via un proxy listé dans `GLD_TRUSTED_PROXIES` qui envoie `X-Forwarded-Proto: https`). Expiration glissante de 12 h, absolue de 30 jours. Les requêtes qui modifient l'état exigent l'en-tête CSRF `X-Gld-CSRF`. Désactiver un compte, réinitialiser ses facteurs ou changer son mot de passe ferme ses sessions.

### Le jeton en mode multi-utilisateur

Le cookie `gld_token` et `?token=` ne servent plus au navigateur. Le jeton ne vaut qu'en en-tête `Authorization: Bearer …`, jamais dans l'URL, pour l'automatisation et les gld enfants (un home autre que le principal est servi par un gld enfant, joignable seulement par la passerelle : elle retire `Cookie` et `Authorization` avant de relayer).

### Coupure des flux longs

Le terminal web et `/chat` sont revérifiés pendant leur durée : session expirée, révoquée, compte désactivé ou droit retiré coupent le flux, et un tour de chat en cours est arrêté. Pour un home enfant, la revérification a lieu toutes les 5 s, pas à chaque message : un utilisateur révoqué peut encore écrire jusqu'à environ 5 s dans un terminal enfant.

## Anti essais répétés

Compteurs en base, clés par nom, par adresse et par couple nom + adresse ; la fenêtre est de 15 minutes. La réponse est la même quelle que soit la cause (« identifiants invalides ou compte temporairement bloqué ») avec un `Retry-After`, le détail va dans le journal d'événements.

| Mesure | Valeur |
|---|---|
| Délai progressif | dès 3 échecs : 2^(n-3) s, plafonné à 900 s, avant même de vérifier le mot de passe |
| Verrou du compte | 10 échecs : 30 min. Ne s'applique pas à une adresse d'où le compte s'est déjà connecté avec succès dans les 30 derniers jours |
| Second facteur | 5 échecs : session partielle détruite, mot de passe à ressaisir |
| Plafond global | 60 tentatives par minute sur toute la passerelle, en mémoire |
| Codes TOTP | un code déjà accepté n'est pas rejouable |

Un déluge de connexions sur un nom peut gêner ce compte (déni de service local, borné par le délai). Le compteur global repart à zéro au redémarrage.

### Proxys et adresse du client

L'adresse du client est `RemoteAddr`. `X-Forwarded-For` n'est lu que si l'adresse directe est listée dans `GLD_TRUSTED_PROXIES` (adresses ou préfixes CIDR séparés par des virgules). Derrière un proxy sans cette variable, tous les clients partagent l'adresse du proxy, et gld avertit au démarrage s'il voit un `X-Forwarded-For` venant d'une adresse de bouclage ou privée.

### HTTPS et WebAuthn

WebAuthn exige HTTPS, sauf pour `localhost`. Derrière un proxy ou un nom de domaine, fixez l'adresse publique :

```sh
GLD_PUBLIC_URL=https://gld.exemple.org gld
```

L'origine doit être en `https`, sauf `http://localhost`. Une adresse IP est refusée (un identifiant WebAuthn ne peut pas en être une). Sans la variable, seul `http://localhost:<port>` est accepté. TOTP et codes de secours n'ont pas cette contrainte. La connexion n'est pas chiffrée par gld : passez par un tunnel ou un proxy TLS.

## Protection de la base

Quatre couches, de la plus forte à la plus faible :

1. **Droits `exec` et `config_write` réservés aux gens de confiance** (voir plus haut). C'est la seule vraie mesure sans changement de déploiement.
2. **Lignes signées.** Chaque ligne de `users`, `home_acl`, `totp`, `webauthn_creds` et `recovery_codes` porte un HMAC-SHA-256 (clé dérivée de la clé maîtresse), et les secrets sont chiffrés. Une ligne modifiée à la main en SQL (signature absente ou fausse) est refusée, journalisée (`tamper`) et signalée aux administrateurs ; elle apparaît marquée altérée, jamais cachée. Limites : **le rejeu d'une ancienne ligne valablement signée n'est pas détecté** (restaurer un droit révoqué, par exemple) ; et qui peut lire la clé maîtresse peut signer. La signature ne stoppe que l'écriture SQL sans la clé.
3. **Garde-fou dans les sessions lancées par gld.** Pour les sessions Claude que gld démarre (chat web, onglets), il injecte des règles de refus `Read` et `Edit` sur le dossier d'authentification, un hook `PreToolUse` sur `Bash`, `PowerShell` et `Monitor` qui refuse les commandes citant `auth.db`, `auth.key`, le dossier d'authentification ou `sqlite`, et la configuration `sandbox.filesystem` (appliquée seulement si le bac à sable de Claude Code est activé, ce que gld ne fait pas). Il retire les `--settings` fournis par l'utilisateur.
4. **Séparation par compte OS** (`GLD_RUN_AS`, expérimentale, voir plus bas).

### Fichier managed-settings

Sans fichier managed, un compte `config_write` peut retirer les règles de ses propres réglages ; gld le détecte et avertit au démarrage. Le fichier appartient à root :

```sh
gld auth managed-settings > managed-settings.json
sudo install -m 644 -o root managed-settings.json /etc/claude-code/managed-settings.json
```

La commande imprime le JSON sur la sortie standard et la cible conseillée sur l'erreur standard, sans rien écrire. Cibles : Linux `/etc/claude-code/managed-settings.json`, macOS `/Library/Application Support/ClaudeCode/managed-settings.json`, Windows `C:\Program Files\ClaudeCode\managed-settings.json`. Le hook référence le binaire gld par chemin absolu et le dossier d'authentification : régénérez le fichier si vous déplacez l'un ou l'autre (un binaire déplacé fait échouer le hook, qui bloque alors les commandes shell). Le hook ne bloque en cas d'échec qu'avec Claude Code 2.1.295 ou plus récent ; avec une version plus ancienne, son comportement n'est pas documenté et il peut être ignoré.

Windows : la ligne de commande du hook vise Git Bash ; elle n'a pas été testée sous Windows, et sans Git Bash (hooks lancés par PowerShell) elle échoue en bloquant.

### Ce que le garde-fou ne couvre pas

Il vise l'accident ou la dérive de Claude, pas un utilisateur malveillant qui a `exec`.

- Le terminal web, un script, un `python -c` lancé hors de Claude ou un shell ordinaire ne passent ni par les règles ni par le hook : un utilisateur `exec` lit la base.
- Le hook est une heuristique sur le texte de la commande. Contournements connus : variables et jokers (`cat ~/.config/gl?/au*`), encodages, script écrit puis lancé, lien symbolique, `find ~ -name 'auth*'`. Faux positifs : toute commande citant `.config/gld` ou `sqlite`.
- Le champ d'entrée de l'outil `Monitor` n'est pas documenté par Claude Code ; gld lit `command`, et si ce n'est pas le bon nom, le hook ne voit rien.
- Un `disableAllHooks` dans les réglages d'un compte `config_write` peut désactiver le hook injecté (non vérifié) ; le fichier managed est la parade.
- Un `claude` tapé dans le terminal web n'est pas injecté.
- gld ne vérifie pas que le fichier managed appartient à root.

## Administration

Routes `/admin/*` (administrateurs avec second facteur validé) : comptes (création, désactivation, mot de passe, réinitialisation des facteurs), droits (`/admin/acl`), journal d'événements, sessions d'un compte et leur révocation, liste des homes. Points d'attention :

- Les opérations sensibles demandent une réauthentification de moins de 5 minutes : création de compte, réinitialisation de facteurs, changement de mot de passe, attribution de `exec` ou `config_write` (avec confirmation explicite), réactivation, désactivation d'un administrateur, révocation des sessions d'un administrateur. Désactiver un compte ordinaire est immédiat. On ne peut ni se désactiver soi-même ni désactiver le dernier administrateur actif. Il n'y a pas de route de rétrogradation d'un administrateur.
- Un mot de passe défini par un administrateur est temporaire par intention, mais **rien ne force son changement** à la première connexion.
- `GET /admin/sessions` (adresse IP et navigateur d'un autre compte) et `GET /auth/sessions` (ses propres sessions) ne demandent pas de réauthentification.
- Chaque action administrative écrit le journal d'événements (compte visé, administrateur, adresse). `GET /account` (usages) peut déclencher un relevé.

## `GLD_RUN_AS` : séparation par compte OS (expérimental)

**Ce n'est pas encore une frontière de sécurité.** Le mode fait tourner sous un autre compte système les processus qu'un utilisateur peut influencer : claude, tmux, le terminal web, git, les onglets et les gld enfants. Mais la passerelle (root) lit et écrit encore elle-même, en suivant les liens symboliques, des dossiers de configuration et de projet que ce compte peut modifier (écriture de réglages et sauvegardes, synchronisation entre homes, restauration, lecture brute de configuration, corbeilles…). Un lien posé par le compte cible peut donc faire lire ou écrire à root un fichier arbitraire. Corriger cela demande de migrer ces accès vers `os.OpenRoot` ou des opérations par descripteur ; ce travail n'est pas fait, et la liste des appels concernés est tenue dans le journal du lot (`.superpowers/sdd/2026-10-23-multi-user/task-16-report.md`). Tant qu'il n'est pas fait, n'utilisez pas `GLD_RUN_AS` pour contenir un utilisateur hostile.

Activation : unix seulement, passerelle lancée **en root** :

```sh
GLD_RUN_AS=gld-sessions GLD_RUN_AS_EXPERIMENTAL=1 gld
```

`GLD_RUN_AS` accepte un nom, un uid ou `uid:gid`. Sans `GLD_RUN_AS_EXPERIMENTAL=1`, gld refuse de démarrer. Sous Windows c'est sans effet (avertissement). Prérequis :

- passerelle lancée par root, sans capacités ambiantes ou héritées, et binaire gld sans capacités de fichier (systemd : pas de `User=`, pas d'`AmbientCapabilities=`) ;
- dossier d'authentification (`GLD_AUTH_DIR`, par exemple `/var/lib/gld-auth`) en 0700 à root, aucun parent modifiable par le compte cible (hors dossiers root avec sticky bit), hors du HOME de la cible et réciproquement ;
- binaire gld exécutable par la cible mais modifiable ni par elle, ni dans son dossier (par exemple `/usr/local/bin`) ;
- compte cible non root, hors groupe 0, différent de celui de la passerelle ; `CLAUDE_CONFIG_DIR` et les dossiers des homes inscriptibles par la cible ;
- retirer de l'environnement du service les secrets sans rapport (`DATABASE_URL`, `AWS_*`…) : ils passent aux sessions ; avec systemd `PrivateTmp=`, le serveur tmux de la cible vit dans le /tmp privé du service.

Au démarrage gld vérifie tout cela, y compris, en relançant un assistant sous le compte cible, qu'il n'a aucun accès à `auth.db`, `auth.key` ni au dossier d'authentification, et refuse de démarrer à la moindre incohérence (jamais de repli silencieux). Les processus lancés ont un `umask` de 077 et un environnement purgé (`GLD_AUTH_*`, `GLD_WEB_*`, identité de la passerelle, `TMUX*`, `XDG_*`, `SSH_AUTH_SOCK`…).

Non testé en réel : onglets tmux, envoi de touches et terminal web sous un autre uid ; macOS.

## Limites connues

- `exec` et `config_write` valent le compte OS de gld ; la séparation par compte est expérimentale.
- Rejeu d'une ligne signée ancienne non détecté ; signature inutile contre qui lit la clé.
- Clé maîtresse en fichier (`auth.key`) : protégée seulement par les droits du dossier.
- Terminal web proxifié vers un home enfant : revérification toutes les 5 s. `/chat` vers un enfant est coupé par la passerelle puis arrêté par un appel `/chat-stop` à l'enfant ; le garde ne relit pas un corps de plus de 64 Kio (413).
- Fenêtre de 2 s après la création du premier compte pendant laquelle le serveur reste ouvert.
- Enrôlement au premier login protégé par le seul mot de passe ; aucun changement de mot de passe forcé ; aucune route de rétrogradation d'administrateur.
- Garde-fou : heuristique contournable, entrée de `Monitor` non vérifiée, `disableAllHooks`, Windows non testé, Claude Code 2.1.295 minimum pour le blocage en cas d'échec du hook.
- `/admin/acl` : quand une requête accorde puis retire des droits, les deux se font en une seule transaction ; en cas d'échec, rien n'est modifié.
- `/cleanup-warning` et `/git-status` exposent titres de sessions et dossiers aux comptes qui ont `mem_read`.
- Si `auth.db` est remplacée pendant que le serveur tourne, il refuse le service puis rouvre la nouvelle base ; les requêtes en cours peuvent échouer.
