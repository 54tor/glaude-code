# Multi-utilisateur : comptes, droits par home, double authentification

Par défaut, Glaude Code n'a qu'un jeton (`GLD_WEB_TOKEN`) : qui l'a voit tout. Le mode multi-utilisateur est **optionnel** et permet à plusieurs personnes de se connecter chacune avec leur compte, avec un second facteur obligatoire et des droits indépendants par home (dossier `CLAUDE_CONFIG_DIR`).

Lisez d'abord la section [Ce que les droits protègent, et ce qu'ils ne protègent pas](#ce-que-les-droits-protègent-et-ce-quils-ne-protègent-pas).

## Activer

Rien ne change tant qu'aucun compte n'existe : pas de base, pas de clé, comportement identique à l'ancien. À l'activation, la première connexion efface aussi, dans le navigateur, les brouillons et l'état d'affichage locaux du mode ouvert (ils n'ont pas de propriétaire connu). Le mode s'active à la création du premier compte, en ligne de commande, jamais depuis l'interface web :

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

La base n'est créée qu'au premier `gld user add`. Au démarrage, gld rend d'abord le dossier et la base privés (chmod 0700 et 0600), puis les vérifie ; une base inutilisable arrête le serveur.

**Jamais de retour silencieux au mode ouvert, dans la limite de ce que gld peut prouver.** Un marqueur « comptes » (`meta.accounts`, état `off` ou `on`) est créé avec la base et signé avec la clé maîtresse. Il passe à `on` au premier compte et aucune commande ne le remet à `off`. La valeur signée de `off` est déterministe : qui a lu la base pendant qu'elle était à `off` peut la réécrire après avoir supprimé les comptes (c'est la limite du rejeu, voir « Lignes signées »).

| Altération | Effet |
|---|---|
| `auth.db` supprimée ou remplacée pendant que gld tourne | service refusé (503) jusqu'au redémarrage |
| lignes de `users` supprimées (marqueur à `on`) | 503, y compris après redémarrage |
| marqueur supprimé ou modifié à la main | 503 |
| témoin de clé supprimé alors que la base est initialisée | refus de démarrer, aucune clé régénérée |

Reste un cas **inévitable** : supprimer ensemble le témoin, le marqueur et toutes les lignes signées revient à fabriquer une base neuve, que gld ne distingue pas d'une première installation (aucun état hors de la base ne peut l'en empêcher ; c'est équivalent à supprimer `auth.db` puis redémarrer : mode ouvert). Pour repartir volontairement de zéro : arrêter gld, supprimer `auth.db` et `auth.key` (et l'entrée du coffre-fort si la clé y est), puis redémarrer et recréer le premier compte avec `gld user add --admin`.

### Clé maîtresse

Les secrets TOTP (et eux seuls) sont chiffrés (AES-256-GCM) ; les lignes sensibles sont signées (HMAC) avec une clé dérivée de la même clé maîtresse de 32 octets. Les autres colonnes restent en clair. Ordre de recherche :

1. `GLD_AUTH_KEY` (64 caractères hexadécimaux) : la variable reste dans l'environnement initial de gld. Sous Linux, gld se marque alors non « dumpable » pour que les sessions du même compte ne la lisent pas dans `/proc/<pid>/environ`, et le garde refuse `Read(//proc/*/environ)`. En production, préférez le coffre-fort ou `auth.key` ;
2. le coffre-fort de l'OS (service `gld`) ;
3. sinon, un fichier `auth.key` (0600) à côté de la base, créé à la première utilisation. Il protège moins qu'un coffre-fort : qui lit le dossier lit la clé. gld l'écrit une fois sur la sortie d'erreur au démarrage quand la clé vient de ce fichier.

Si la clé ne correspond pas à la base, gld refuse de démarrer (et la ligne de commande `gld user …` refuse aussi) ; il ne génère jamais de clé (ni dans le coffre-fort, ni dans `auth.key`) sur une base qui possède déjà un témoin de clé. **Perte de la clé** : `gld user reset-mfa` ne peut pas aider, puisqu'elle exige d'ouvrir la base. Retrouvez la clé (sauvegarde, `GLD_AUTH_KEY` d'origine). Sinon, déplacez `auth.db` (et `auth.key`) hors du dossier, puis recréez le premier compte avec `gld user add --admin` : tous les comptes, facteurs et droits sont à refaire.

## Les droits

Neuf droits, indépendants, par couple (compte, home). Un droit absent vaut refusé. Chaque route est classée dans une table unique (`routeRights`, `gld/auth_acl.go`) ; un test échoue si une route n'y figure pas.

| Droit | Ouvre |
|---|---|
| `mem_read` | mémoire, règles, snippets (lecture), notes, corbeille (lecture), santé, lecture du home (configuration, état git, onglets, commandes) |
| `mem_write` | écrire, déplacer, fusionner, supprimer CLAUDE.md, règles, mémoire, notes, et purger la corbeille. Ne crée aucun contenu exécutable : les snippets (commandes slash) et la restauration d'un snippet ou d'un skill exigent `config_write`. `POST /v1/snippets/drafts` (simple brouillon, rien n'est écrit) reste en `mem_write` |
| `mem_sync` | prévisualiser et appliquer la synchronisation entre homes ; exige `mem_read` sur la source et `mem_write` sur la destination, vérifiés home par home. Appliquer un élément autre que CLAUDE.md et les règles (réglages, permissions, skills, commandes) exige en plus `config_write` sur la destination |
| `usage_read` | compte et usages, jamais le contenu des conversations |
| `sessions_read` | transcriptions et état des sessions, recherche (`/v1/sessions?q=`), `/v1/info` et `/v1/config/permissions/suggestions` (qui renvoient des extraits de prompts ou de transcriptions) |
| `config_write` | réglages et skills (hooks, serveurs MCP, permissions), snippets (écriture, suppression, import), édition brute, restauration d'une sauvegarde, restauration d'un snippet ou d'un skill depuis la corbeille |
| `exec` | chat, nouvelle session, terminal web, envoi de touches, confiance d'un dossier, push git, fermeture, renommage, suppression et corbeille de **sessions**, et `/v1/directories` (parcours du disque) |
| `fs_read` | lire l'arborescence et les fichiers d'un projet, y compris `.env` et clés. La racine doit être un dossier de travail connu de gld ; `$HOME`, ses ancêtres, la config Claude de chaque home et le dossier d'auth de gld sont refusés (voir [ide.md](ide.md)) |
| `fs_write` | écrire, créer, renommer et supprimer (vers `.gld-trash/`) des fichiers d'un projet. **N'implique pas** `fs_read` : accorder les deux ensemble. L'espace de travail complet exige en plus `exec` pour le terminal |

Notes :

- `GET /v1/homes` et `PUT /v1/homes/current` ne montrent que les homes où le compte a au moins un droit.
- Créer ou retirer un home (`/v1/homes`) est réservé aux administrateurs. Le nom `main` est réservé. En mode multi-utilisateur, le serveur conserve les homes que la requête ne cite pas : seuls les identifiants du champ `removed` sont retirés (avec leurs droits, en cascade). Une entrée sans identifiant (ou avec un autre) qui porte le nom d'un home existant, même invisible de l'admin, est refusée (« nom déjà pris ») ; seule l'entrée qui porte l'identifiant de ce home le modifie.
- Les droits sont rattachés à l'**identifiant** d'un home, pas à son nom. Supprimer un home supprime ses droits en cascade. Renommer un home via `/v1/homes` en omettant son identifiant en crée un nouveau, donc fait perdre ses droits. Échanger les dossiers de deux homes déplace les droits avec les noms.
- Un administrateur gère les comptes et les droits mais n'a **aucun accès implicite** aux homes : il s'accorde les droits qu'il veut.
- Le jeton (Bearer) donne tous les droits sur **chaque** home mais jamais l'administration, ni les routes `/auth/*` et `/admin/*`.
- `POST /v1/memory/drafts` et `POST /v1/skills/drafts` lisent côté serveur les fichiers que l'appelant désigne et renvoient la réponse du modèle, ce qui peut en restituer le contenu : elles exigent donc `mem_read` en plus de `mem_write` (respectivement `config_write`). Plus généralement, le serveur refuse d'accorder `mem_write` ou `config_write` à un compte sans `mem_read` sur le même home (`gld user grant` comme l'écran d'administration), et de retirer `mem_read` tant que l'un des deux reste ; les lignes déjà enregistrées qui ne respectent pas cette règle continuent de fonctionner, mais les brouillons leur sont refusés.

### Ce que les droits protègent, et ce qu'ils ne protègent pas

**`exec` et `config_write` équivalent au compte système qui fait tourner gld.** Écrire un hook, un serveur MCP ou un skill, ou ouvrir le chat ou le terminal web, exécute du code sous cet utilisateur : la personne peut lire et écrire `auth.db`, `auth.key` et le coffre-fort. Les droits protègent l'interface, pas la machine. N'accordez ces deux droits qu'à des personnes de confiance ; ni l'écran d'administration ni `gld user grant` (confirmation, `--yes` pour automatiser) ne vous en dispensent. Un co-utilisateur limité à la mémoire, aux usages et à la lecture des sessions n'a aucun chemin vers la base.

## Connexion

1. `POST /v1/auth/sessions` : nom et mot de passe, vérifiés en temps constant (un nom inconnu coûte autant qu'un nom existant, réponse identique).
2. Second facteur, **obligatoire** : code TOTP, clé de sécurité WebAuthn (clé matérielle, passkey) ou l'un des 10 codes de secours (à usage unique, affichés une fois). Tant qu'il manque, seules les routes `/auth/*` répondent.
3. Un compte sans aucun facteur en enrôle un à la première connexion (TOTP ou clé de sécurité), et reçoit ses codes de secours. **Attention** : cet enrôlement n'exige que le mot de passe. Quiconque connaît le mot de passe d'un compte encore sans facteur peut y enrôler le sien. Le bit `mfa_enrolled` du compte est signé et posé avec le premier facteur : supprimer les lignes de facteurs en SQL ne rouvre pas cet enrôlement (la connexion est refusée et un événement `tamper` est écrit) ; seul `gld user reset-mfa` le rouvre. Créez donc le compte juste avant la première connexion de la personne (ou enrôlez-la vous-même), et ne laissez pas traîner un compte sans facteur. `gld user reset-mfa` ramène un compte à cet état.
4. Ajouter ou retirer un facteur (un compte garde toujours au moins un facteur), changer son mot de passe ou régénérer ses codes demande une réauthentification : le mot de passe **et** un code (TOTP ou de secours), à chaque action dangereuse. Seule une assertion de clé de sécurité (qui n'a pas de code à saisir) est retenue 5 minutes sur la session, avec le mot de passe toujours redemandé. Révoquer une session en demande aussi une. Ces actions sont des `POST` sur des sous-ressources (`/v1/auth/factors/{factor}/removals`, `/v1/auth/sessions/{sid}/revocations`, `/v1/auth/sessions/revocations`, `/v1/admin/users/{user}/mfa-resets`, `/v1/admin/sessions/revocations`) et non des `DELETE` : la réauthentification voyage dans le corps JSON, que des proxys ou pare-feu applicatifs peuvent retirer des `DELETE`. La liste des sessions du compte (`GET /v1/auth/sessions`) n'en demande pas.

Sessions : cookie `gld_session` (valeur aléatoire de 32 octets ; la base ne garde que son empreinte HMAC-SHA-256 et signe la ligne), `HttpOnly`, `SameSite=Strict`, `Secure` quand la requête arrive en HTTPS (directement, ou via un proxy listé dans `GLD_TRUSTED_PROXIES` qui envoie `X-Forwarded-Proto: https`). Expiration glissante de 12 h, absolue de 30 jours. Les requêtes qui modifient l'état exigent l'en-tête CSRF `X-Gld-CSRF`. Désactiver un compte, réinitialiser ses facteurs ou changer son mot de passe ferme ses sessions. Le cookie `gld_home` (home choisi) est effacé à la déconnexion et à chaque connexion réussie. Les mises à niveau de la base (migrations 0006 et 0007) ferment toutes les sessions.

Jumelage (`POST /v1/pairing/codes`, QR) : la session ouverte par le code dépend de la session qui l'a émis. Elle a la même échéance absolue, et révoquer, déconnecter ou laisser expirer l'émettrice la ferme aussi. Une session jumelée depuis une session jumelée dépend de l'émettrice d'origine (migration 0009).

### Le jeton en mode multi-utilisateur

Le cookie `gld_token` et `?token=` ne servent plus au navigateur. Le jeton ne vaut qu'en en-tête `Authorization: Bearer …`, jamais dans l'URL, et **seulement s'il est fixé par l'opérateur** (`GLD_WEB_TOKEN`) : un jeton généré automatiquement (écoute hors localhost sans la variable) est refusé en mode multi-utilisateur et n'est plus imprimé dans l'adresse de démarrage. Il sert à l'automatisation et aux gld enfants (un home autre que le principal est servi par un gld enfant, joignable seulement par la passerelle : elle retire `Cookie` et `Authorization` avant de relayer).

### Coupure des flux longs

Le terminal web et `/v1/sessions/{id}/chat` sont revérifiés pendant leur durée : session expirée, révoquée, compte désactivé ou droit retiré coupent le flux, et un tour de chat en cours est arrêté. Seul le terminal web **local** est contrôlé à chaque message reçu ; `/v1/sessions/{id}/chat` local et les flux relayés vers un home enfant (terminal, chat) le sont toutes les 5 s : un utilisateur révoqué peut encore agir jusqu'à environ 5 s.

## Anti essais répétés

Le jeton opérateur est limité comme le formulaire de jeton : chaque jeton faux présenté (cookie `gld_token`, `Authorization: Bearer` ou `?token=`) compte pour l'adresse, 10 par minute, et 100 par minute pour tout le serveur ; au-delà, `429`. Un jeton valide ne consomme rien.

Compteurs en base, clés par nom, par adresse (une adresse IPv6 est ramenée à son préfixe /64) et par couple nom + adresse ; la fenêtre est de 15 minutes. La réponse est la même quelle que soit la cause (« identifiants invalides ou compte temporairement bloqué ») avec un `Retry-After`, le détail va dans le journal d'événements.

| Mesure | Valeur |
|---|---|
| Délai progressif | dès 3 échecs : 2^(n-3) s, plafonné à 900 s, avant même de vérifier le mot de passe |
| Bannissement de l'adresse | 8 échecs en 15 min depuis une adresse extérieure, tous noms confondus : l'adresse (ou son /64) est refusée 1 h, y compris là où un compte s'est déjà connecté ; ligne `ban` dans le journal. Jamais appliqué à la machine, aux réseaux privés ni au lien local. Durée fixe, sans escalade |
| Verrou du compte | 10 échecs : 30 min. Ne s'applique pas à une adresse d'où le compte s'est déjà connecté avec succès dans les 30 derniers jours (marqueur `known:` dans `throttle`) |
| Suite d'échecs consécutifs | échecs de mot de passe **et** de second facteur comptés sans fenêtre, remis à zéro par une connexion réussie : au 10e, verrou de 30 min et ligne dans le journal de la passerelle ; au 20e, verrou de 24 h. Levée depuis une adresse connue |
| Second facteur | 5 échecs depuis une même adresse : session partielle détruite, mot de passe à ressaisir, et connexion de ce compte bloquée depuis cette adresse jusqu'à la fin de la fenêtre de 15 min. Une connexion réussie depuis cette adresse remet ce compteur à zéro. 15 échecs du compte, toutes adresses confondues, bloquent sa connexion partout jusqu'à la fin de la fenêtre ; ce compteur-là n'est jamais remis à zéro par une connexion |
| Noms trop longs | un nom de plus de 64 caractères est compté sous une clé commune et refusé sans calcul argon2 |
| Argon2 | 4 calculs simultanés au plus sur toute la passerelle |
| Plafond global | 60 tentatives par minute sur toute la passerelle, en mémoire. Un compte déjà connecté depuis la même adresse (moins de 30 jours) n'y puise pas : une inondation de noms inventés ne bloque pas les habitués |
| Codes TOTP | dérive acceptée de ±1 pas (30 s) ; un code déjà accepté n'est pas rejouable |

Chaque ban ouvre une alerte dans toutes les sessions connectées (l'interface interroge `GET /v1/auth/alerts?since=` toutes les 15 s ; seuls les bans survenus après l'ouverture de l'onglet sont montrés ; l'adresse n'est donnée qu'aux administrateurs, les autres comptes sont prévenus sans elle).

Un déluge de connexions sur un nom peut gêner ce compte (déni de service local, borné par le délai). Le compteur global repart à zéro au redémarrage. La fenêtre de 15 min est fixe (elle part du premier échec). Les lignes de `throttle` sans effet, les sessions échues et les événements de plus de 90 jours sont purgés à l'ouverture de la base puis au plus une fois par 24 h.

**Journal d'événements** : le nom saisi est tronqué à 64 caractères. Les refus et contentions sans compte (`blocked`, `busy`, `denied`) sont limités à 5 par minute et par adresse et 200 par minute en tout (table de 1024 adresses, au-delà elles partagent un compte commun) ; ceux d'un compte, à un par minute et par type. Une ligne `suppressed` par minute donne les totaux perdus, écrite au prochain événement ou à la prochaine requête vérifiée. Les écritures de l'espace de travail réussies y figurent sous `ide_write` (opération, dossier, chemin relatif, octets ; jamais le contenu). Sont toujours écrits : altérations, verrous, échecs et succès du second facteur, succès de connexion, changements de droits, de facteurs et de mots de passe, révocations. Une inondation d'adresses peut donc taire d'autres refus anonymes pendant la minute.

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
2. **Lignes signées.** Chaque ligne de `users`, `home_acl`, `totp`, `webauthn_creds`, `recovery_codes` et `sessions` (identifiant, compte, création, second facteur) porte un HMAC-SHA-256 (clé dérivée de la clé maîtresse) ; seuls les secrets TOTP sont chiffrés. Le marqueur des comptes et le bit `mfa_enrolled` sont signés aussi ; l'expiration et la dernière activité d'une session ne le sont pas. Une ligne modifiée à la main en SQL (signature absente ou fausse) est refusée, journalisée (`tamper`) et signalée aux administrateurs ; elle apparaît marquée altérée, jamais cachée. Limites : **le rejeu d'une ancienne ligne valablement signée n'est pas détecté** (restaurer un droit révoqué, par exemple) ; et qui peut lire la clé maîtresse peut signer. La signature ne stoppe que l'écriture SQL sans la clé.
3. **Garde-fou dans les sessions lancées par gld.** Pour les sessions Claude que gld démarre (chat web, onglets), il injecte des règles de refus `Read` et `Edit` sur le dossier d'authentification, un hook `PreToolUse` sur `Bash`, `PowerShell` et `Monitor` qui refuse les commandes citant `auth.db`, `auth.key`, le dossier d'authentification ou `sqlite`, et la configuration `sandbox.filesystem` (appliquée seulement si le bac à sable de Claude Code est activé, ce que gld ne fait pas). Il retire les `--settings` fournis par l'utilisateur.
4. **Séparation par compte OS** (`GLD_RUN_AS`, expérimentale, voir plus bas).

### Fichier managed-settings

Sans fichier managed, un compte `config_write` peut retirer les règles injectées de ses propres réglages ; gld le détecte et avertit au démarrage. Le fichier appartient à root :

```sh
gld auth managed-settings > managed-settings.json
sudo install -m 644 -o root managed-settings.json /etc/claude-code/managed-settings.json
```

La commande imprime le JSON sur la sortie standard et la cible conseillée sur l'erreur standard, sans rien écrire. Cibles : Linux `/etc/claude-code/managed-settings.json`, macOS `/Library/Application Support/ClaudeCode/managed-settings.json`, Windows `C:\Program Files\ClaudeCode\managed-settings.json`. Le hook référence le binaire gld par chemin absolu et le dossier d'authentification : régénérez le fichier si vous déplacez l'un ou l'autre (un binaire déplacé fait échouer le hook, qui bloque alors les commandes shell). gld ne reconnaît le fichier installé que si son hook porte `onFailure: "block"`. Le hook ne bloque en cas d'échec qu'avec Claude Code 2.1.295 ou plus récent ; avec une version plus ancienne, son comportement n'est pas documenté et il peut être ignoré.

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

Routes `/v1/admin/*` (administrateurs avec second facteur validé) : comptes (création, désactivation, mot de passe, réinitialisation des facteurs), droits (`/v1/admin/acl`), journal d'événements, sessions d'un compte et leur révocation, liste des homes. Points d'attention :

- Les opérations sensibles demandent une réauthentification (mot de passe plus code ; une assertion de clé de sécurité reste valable 5 minutes) : création de compte, réinitialisation de facteurs, changement de mot de passe, attribution de `exec`, de `config_write` ou de `fs_write` (avec confirmation explicite), de `sessions_read` ou de `fs_read`, tout octroi de droits à soi-même, réactivation, désactivation d'un administrateur, révocation des sessions d'un administrateur. Les retraits de droits restent immédiats. Désactiver un compte ordinaire est immédiat. On ne peut ni se désactiver soi-même ni désactiver le dernier administrateur actif. Il n'y a pas de route de rétrogradation d'un administrateur.
- Un mot de passe défini par un administrateur est temporaire par intention, mais **rien ne force son changement** à la première connexion.
- `GET /v1/admin/sessions` (adresse IP et navigateur d'un autre compte) et `GET /v1/auth/sessions` (ses propres sessions) ne demandent pas de réauthentification.
- Le journal d'événements est purgé au-delà de 90 jours (à l'ouverture de la base, puis au plus une fois par 24 h). Chaque action administrative l'écrit (compte visé, administrateur, adresse). `GET /v1/account` (usages) peut déclencher un relevé.

## `GLD_RUN_AS` : séparation par compte OS (expérimental)

**Ce n'est pas encore une frontière de sécurité.** Le mode fait tourner sous un autre compte système les processus qu'un utilisateur peut influencer : claude, tmux, le terminal web, git, les onglets et les gld enfants. Mais la passerelle (root) lit et écrit encore elle-même, en suivant les liens symboliques, des dossiers de configuration et de projet que ce compte peut modifier (écriture de réglages et sauvegardes, synchronisation entre homes, restauration, lecture brute de configuration, corbeilles…). Un lien posé par le compte cible peut donc faire lire ou écrire à root un fichier arbitraire. Corriger cela demande de migrer ces accès vers `os.OpenRoot` ou des opérations par descripteur : voir [auth-followup-openroot.md](auth-followup-openroot.md). Tant que ce n'est pas fait, n'utilisez pas `GLD_RUN_AS` pour contenir un utilisateur hostile.

Activation : unix seulement, passerelle lancée **en root** :

```sh
GLD_RUN_AS=gld-sessions GLD_RUN_AS_EXPERIMENTAL=1 gld
```

`GLD_RUN_AS` accepte un nom, un uid ou `uid:gid`. Sans `GLD_RUN_AS_EXPERIMENTAL=1`, gld refuse de démarrer ; avec, il affiche un avertissement. Sous Windows c'est sans effet (avertissement). Il n'existe plus de mode par capacités (`setcap`) : root est exigé.

Prérequis côté opérateur :

- passerelle lancée par root (systemd : pas de `User=`, pas d'`AmbientCapabilities=`) ;
- dossier d'authentification (`GLD_AUTH_DIR`, par exemple `/var/lib/gld-auth`) sur un chemin absolu ; gld le crée en root 0700 s'il manque ;
- binaire gld exécutable par la cible, mais ni lui, ni son dossier, ni aucun de ses parents modifiables par elle (par exemple `/usr/local/bin`) ;
- compte cible non root, hors groupe 0, différent de celui de la passerelle ; `CLAUDE_CONFIG_DIR` et les dossiers des homes inscriptibles par la cible ;
- avec systemd, `PrivateTmp=` : le serveur tmux de la cible vit dans le /tmp privé du service.

Vérifications au démarrage : toute incohérence arrête gld, jamais de repli silencieux sous le compte de la passerelle.

- Compte valide : introuvable, root, membre du groupe 0 ou identique à la passerelle sont refusés.
- Processus : euid 0 ; sous Linux, `/proc/self/status` lisible avec `CapAmb` et `CapInh` vides ; binaire sans capacités de fichier.
- Binaire : lui et chacun de ses parents jusqu'à `/` ne doivent appartenir ni être modifiables (bits groupe ou autres, selon l'uid et les gids de la cible) par le compte cible, ni porter une ACL POSIX (Linux ; macOS non vérifié). Un parent root avec sticky bit est toléré, jamais le binaire lui-même.
- Dossier d'authentification : chemin absolu ; HOME de la cible et dossier non imbriqués, dans aucun sens. Le premier parent existant est jugé avant toute création, puis le dossier est créé en 0700 s'il manque et relu (vrai dossier, propriété de root, sans droit groupe ni autres, sans sticky). Le chemin tel que configuré et le chemin résolu sont tous deux parcourus jusqu'à `/` avec les mêmes règles, les chaînes de liens symboliques sont suivies (16 liens au plus, une boucle est refusée) et un lien n'est admis que si lui et sa cible sont sûrs.
- Assistant `gld _canread`, lancé sous la cible : `access(2)` ne doit lui donner aucun accès au dossier d'authentification ni à `auth.db`, `auth.db-wal`, `-shm`, `-journal` et `auth.key`, ni d'écriture sur le binaire et son dossier. Cette couche s'ajoute aux autres, jamais seule.

Effets sur les sessions :

- **`umask` 077** : tout ce que créent les processus lancés sous `GLD_RUN_AS` (claude et ses outils, git, tmux, gld enfants, onglets) naît en 0600 (fichiers) et 0700 (dossiers), dépôts de travail compris. Un dépôt partagé par groupe devient inutilisable pour les autres membres.
- **Environnement purgé** : toute variable commençant par `GLD_AUTH_` ou `GLD_WEB_`, `GLD_RUN_AS` et `GLD_RUN_AS_EXPERIMENTAL`, l'identité de la passerelle (HOME, USER, LOGNAME, SHELL, MAIL), `TMUX*`, `XDG_*_HOME`, `XDG_RUNTIME_DIR`, `DBUS_SESSION_BUS_ADDRESS` et `SSH_AUTH_SOCK` ne passent pas. Un gld enfant reçoit seulement son jeton, son port et son adresse d'écoute. `ANTHROPIC_API_KEY` et `CLAUDE_*` passent. **Les autres secrets de l'environnement de l'opérateur (`DATABASE_URL`, `AWS_*`…) passent aussi** : retirez-les de l'environnement du service.

Limites restantes de la vérification : entre le contrôle du parent existant et la création du dossier d'authentification, la passerelle ne garde aucun descripteur sur ce parent (un dossier vide root 0700 peut être créé ailleurs par un lien planté, le contrôle suivant refuse alors le démarrage) ; les comptes autres que la cible ne sont pas pris en compte ; les ACL ne sont vérifiées que sous Linux.

Fermer une session ne signale qu'un processus dont les quatre uid (`/proc/<pid>/status`) sont ceux du compte cible (Linux ; ailleurs, refusé) ; une course entre la lecture et le signal n'est pas traitée.

Non testé en réel : onglets tmux, envoi de touches et terminal web sous un autre uid ; macOS.

## Limites connues

- `exec` et `config_write` valent le compte OS de gld ; la séparation par compte est expérimentale.
- Tous les homes et tous les utilisateurs tournent sous un même compte OS (y compris sous `GLD_RUN_AS`, qui n'a qu'un compte cible) : `home_acl` ne sépare pas deux utilisateurs qui ont `exec`, l'un peut lire et modifier les fichiers des homes de l'autre. Sous Linux, un gld enfant se marque non « dumpable », donc son jeton n'est pas lisible dans `/proc/<pid>/environ` ; macOS et Windows n'ont pas cette protection.
- Séparation par compte OS : voir [auth-followup-openroot.md](auth-followup-openroot.md).
- Rejeu d'une ligne signée ancienne non détecté ; signature inutile contre qui lit la clé. Le nombre de sessions affiché par `GET /v1/admin/users` n'est pas vérifié (affichage seul).
- Retour au mode ouvert possible si témoin, marqueur et lignes signées sont supprimés ensemble (voir « Où sont les données »).
- Un déluge d'adresses peut taire d'autres refus anonymes dans le journal pendant une minute (le résumé `suppressed` en garde le total).
- Une base créée par un état intermédiaire de développement du schéma 7 est refusée et doit être recréée ; les bases des schémas 5 et 6 se mettent à niveau seules.
- `GET /v1/auth/me` et l'enrôlement répondent 500 (et non 403) sur un compte altéré.
- Clé maîtresse en fichier (`auth.key`) : protégée seulement par les droits du dossier.
- `/v1/sessions/{id}/chat` local, terminal et chat relayés vers un home enfant : revérification toutes les 5 s (seul le terminal local est contrôlé par message). `/v1/sessions/{id}/chat` vers un enfant est coupé par la passerelle puis arrêté par un appel `/v1/sessions/{id}/chat` à l'enfant ; le garde ne relit pas un corps de plus de 64 Kio (413).
- Fenêtre de 2 s après la création du premier compte pendant laquelle le serveur reste ouvert.
- Enrôlement au premier login protégé par le seul mot de passe ; aucun changement de mot de passe forcé ; aucune route de rétrogradation d'administrateur.
- `GLD_RUN_AS` : les écritures et lectures de la passerelle ne suivent plus un lien posé par le compte cible hors du dossier de confiance ; restent les dossiers parents d'un projet et quelques `Stat` (détail dans [auth-followup-openroot.md](auth-followup-openroot.md)). Le mode reste expérimental.
- Garde-fou : heuristique contournable, entrée de `Monitor` non vérifiée, `disableAllHooks`, Windows non testé, Claude Code 2.1.295 minimum pour le blocage en cas d'échec du hook.
- `/v1/admin/acl` : quand une requête accorde puis retire des droits, les deux se font en une seule transaction ; en cas d'échec, rien n'est modifié.
- `/v1/cleanup-warning` et `/v1/git/status` exposent titres de sessions et dossiers aux comptes qui ont `mem_read`.
- Si `auth.db` est remplacée pendant que le serveur tourne, il refuse le service puis rouvre la nouvelle base ; les requêtes en cours peuvent échouer.
