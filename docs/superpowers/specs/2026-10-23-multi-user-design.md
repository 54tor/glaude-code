# Multi-utilisateur, multi-home, ACL, 2FA : conception

Statut : construit. Cette spec est la conception d'origine ; les passages marqués « tel que construit » donnent l'état réel du code, qui diffère sur quelques points. Guide d'usage : `docs/auth.md`.

## Intention

Aujourd'hui gld a un jeton unique (`GLD_WEB_TOKEN`) : qui l'a voit tout, sur tous les homes (dossiers `CLAUDE_CONFIG_DIR`, `homes.go`). On veut que plusieurs personnes se connectent chacune avec leur compte, et que chaque compte n'ait accès qu'à certains homes, avec des droits fins : lire la mémoire, l'écrire, la synchroniser, lire les usages. La connexion se fait par identifiant et mot de passe, plus un second facteur (TOTP ou WebAuthn), avec une défense contre les essais répétés.

## Objectifs

- Comptes locaux (identifiant, mot de passe), un ou plusieurs facteurs : TOTP, clés WebAuthn (passkeys, clés matérielles), codes de secours.
- ACL par couple (utilisateur, home) avec des droits indépendants. Rôle admin global pour gérer comptes et accès.
- Application des droits côté serveur, refus par défaut pour toute route non classée.
- Heuristique anti brute force sur le mot de passe et sur le second facteur.
- Stockage SQLite dans le dossier de données standard de l'OS, secrets chiffrés au repos.
- Mode multi-utilisateur optionnel : sans compte créé, gld se comporte exactement comme aujourd'hui.

## Hors périmètre

- Fédération (OIDC, SAML, LDAP), inscription libre, réinitialisation par e-mail (aucun envoi de courrier).
- Isolation système entre utilisateurs : tous les homes tournent sous le même utilisateur OS. L'ACL protège l'interface, pas le système de fichiers (voir « Modèle de menace »).
- Refonte des sessions Claude Code ou de leur format.
- Journal d'audit exportable (seul un journal d'événements d'authentification est prévu).

## Anti-objectifs

- Un droit qui paraît limité mais donne en réalité plus. Exemples centraux : écrire réglages et skills (hooks, serveurs MCP) revient à exécuter du code, d'où `config_write` ; et le chat, le terminal web et l'envoi de touches (`/chat`, `/term-ws`, `/session-send`, `/session-key`, `/new-session`, `/open`) exécutent du code sous l'utilisateur OS. Ils ont leur propre droit `exec`, jamais impliqué par l'écriture de mémoire.
- Réécrire à la main TOTP, WebAuthn ou le hachage de mots de passe (règle du dépôt : bibliothèques reconnues, le code maison se limite à la colle).
- Révéler si un identifiant existe (réponses et durées identiques).
- Casser l'usage local actuel (localhost sans compte, jeton).

## Contraintes

- Go, sans CGO : `modernc.org/sqlite` est déjà une dépendance.
- Architecture existante : le processus principal est la passerelle, chaque home autre que le principal est servi par un gld enfant joignable seulement avec `X-Gld-Child` (`homes.go`, `childAuthorized`). L'authentification et l'ACL vivent dans la passerelle ; les enfants restent de confiance via leur jeton interne.
- Fonctionne sous Linux, macOS, Windows (chemins, permissions de fichier, coffre-fort OS).
- Interface traduite (i18n fr/en, clés dans `ui/src/i18n` et `locales/`).

## Décisions prises avec l'opérateur

| Sujet | Décision |
|---|---|
| Livrable | Spec + plan seulement, validation avant toute construction |
| Stockage | SQLite, dans le dossier de données standard de l'OS (Windows, macOS, Linux), informations sensibles chiffrées |
| Modèle d'accès | Droits indépendants par couple utilisateur/home, rôle admin pour gérer comptes et accès |
| Réglages et skills | Droit séparé `config_write` (hooks, MCP, permissions, skills), distinct de `mem_write` et de `exec` |
| Transcriptions de sessions | Droit séparé `sessions_read` |
| Jeton en mode multi-utilisateur | Réservé aux enfants et à l'automatisation : ni cookie `gld_token` ni `?token=` pour le navigateur ; Bearer en en-tête seulement |
| Jeton actuel | Mode multi-utilisateur opt-in : sans compte, comportement actuel ; dès le premier admin, le login remplace le jeton pour l'interface, le jeton reste valable en Bearer (enfants, automatisation). Tel que construit : seul un jeton fixé par `GLD_WEB_TOKEN` vaut en Bearer, un jeton généré est refusé |

## Choix du rédacteur (à relire)

- **Dossier de données** : `os.UserConfigDir()/gld/auth.db` (Linux `~/.config/gld`, macOS `~/Library/Application Support/gld`, Windows `%AppData%\gld`), dossier en 0700, fichier en 0600. Surchargeable par `GLD_AUTH_DIR`. Distinct du dossier `CLAUDE_CONFIG_DIR` pour que la sauvegarde ou la synchro d'un home n'emporte pas les comptes.
- **Chiffrement au repos** : les secrets TOTP et tout champ réversible sont chiffrés en AES-256-GCM (stdlib) avec une clé maîtresse de 32 octets. La clé vit dans le coffre-fort de l'OS (`github.com/zalando/go-keyring`) ; à défaut (serveur sans coffre), dans `auth.key` en 0600 à côté de la base, et gld avertit au démarrage (une fois, sur la sortie d'erreur) que la protection est plus faible. `GLD_AUTH_KEY` (variable d'environnement) l'emporte sur les deux. Les mots de passe ne sont jamais chiffrés mais hachés (argon2id, `golang.org/x/crypto/argon2`, paramètres stockés avec le hachage pour pouvoir les relever). Les codes de secours sont stockés hachés (argon2id). Tel que construit : l'identifiant de session stocké est `HMAC-SHA-256(clé d'intégrité, cookie)`, et la ligne est signée. Seuls les secrets TOTP sont chiffrés.
- **Bibliothèques** : `github.com/go-webauthn/webauthn` (WebAuthn), `github.com/pquerna/otp` (TOTP), `golang.org/x/crypto/argon2`. À confirmer au moment du plan via Context7 (versions, API).
- **Sessions** : cookie `gld_session`, valeur opaque de 32 octets aléatoires, `HttpOnly`, `SameSite=Strict`, `Secure` dès que la requête arrive en HTTPS (WebAuthn l'exige de toute façon hors localhost). Expiration glissante 12 h, absolue 30 jours, révocables par l'utilisateur et l'admin. Jeton CSRF pour les requêtes qui modifient l'état, en plus de `SameSite`.
- **Second facteur** : obligatoire pour tout compte en mode multi-utilisateur. Au moins un facteur (TOTP ou clé WebAuthn) enrôlé à la première connexion, plus 10 codes de secours à usage unique affichés une seule fois. Un admin peut réinitialiser les facteurs d'un compte, ce qui force un nouvel enrôlement.
- **Premier admin** : créé en ligne de commande (`gld user add --admin <nom>`, mot de passe saisi sans écho), jamais par l'interface web, ce qui évite la course à l'inscription. `gld user grant <nom> <home> <droits…>` donne des droits depuis la ligne de commande ; sans lui, l'admin n'aurait accès à aucun home au premier lancement. Ensuite l'interface « Utilisateurs » (admin) gère comptes, homes et droits.
- **Anti brute force** : voir section dédiée ; les seuils proposés sont des valeurs de départ réglables.

## Modèle de données

SQLite, migrations numérotées appliquées au démarrage (un seul processus écrit : la passerelle).

- `users(id, name UNIQUE COLLATE NOCASE, pw_hash, is_admin, disabled, created_at, pw_changed_at, mfa_enrolled, sig)` : `mfa_enrolled` (signé) est posé avec le premier facteur et empêche de rouvrir l'enrôlement en supprimant les facteurs.
- `totp(user_id, secret_enc, nonce, confirmed_at, last_step)` : `last_step` interdit de rejouer un code déjà accepté.
- `webauthn_creds(id, user_id, cred_id, public_key, sign_count, transports, label, created_at, last_used_at)`
- `recovery_codes(user_id, hash, used_at)`
- `sessions(id_hash, user_id, created_at, last_seen_at, expires_at, ip, ua, mfa_at, sig)` : tel que construit, `sig` couvre id_hash, user_id, created_at et mfa_at.
- `home_acl(user_id, home_id, mem_read, mem_write, config_write, mem_sync, usage_read, sessions_read, exec, sig)` : un droit absent vaut refusé. Tel que construit : la clé est l'identifiant stable du home (`home_id`), pas son nom, et il y a sept droits.
- Colonne `sig` (HMAC) sur les tables sensibles : voir « Modèle de menace ».
- `auth_events(at, user_id NULL, name_tried, ip, kind, outcome)` : connexions, échecs, verrouillages, changements de facteur, modifications d'ACL. Purgé après 90 jours.
- `throttle(key, fails, first_at, locked_until, last_at)` : voir anti brute force.
- `meta` : version du schéma, témoin de la clé maîtresse et marqueur signé des comptes (`off` ou `on`), tel que construit.

## Droits

| Droit | Donne accès à (lecture ou écriture) |
|---|---|
| `mem_read` | liste et contenu de la mémoire, règles, snippets, notes, recherche, santé, tokens chargés (`GET /memory-*`, `/snippet*`, `/notes`, `/search`, `/skills-list`, `/skill`, `/trash*`) |
| `mem_write` | enregistrer, déplacer, supprimer, réparer, fusionner du texte inerte : CLAUDE.md, règles, entrées de mémoire, notes, purge de la corbeille (`POST /memory-*`, `/notes`, `/trash-*`). Tel que construit : les snippets (écriture, suppression, import) et la restauration d'un snippet ou d'un skill depuis la corbeille exigent `config_write` (ils créent des commandes exécutables) ; `/snippet-draft` reste en `mem_write` |
| `config_write` | réglages et skills, qui déclenchent l'exécution de code par Claude Code : `/config-edit`, `/config-raw`, `/config-restore`, `/config-perm-import`, `/snippet-save`, `/snippet-rm`, `/snippets-import`, `/skill-save`, `/skill-copy`, `/skill-rm`, `/config-mcp-status` (peut lancer des serveurs MCP). Équivaut en pratique à `exec`, écrit tel quel dans l'écran d'ACL |
| `mem_sync` | prévisualiser et appliquer la synchro entre homes (`/homes-sync-plan`, `/homes-sync-apply`). Exige aussi `mem_read` sur la source et `mem_write` sur la destination ; chaque home est vérifié séparément. Appliquer des éléments de type réglage, permission, skill ou commande exige en plus `config_write` sur la destination |
| `usage_read` | compte et usages (`GET /account`, statistiques), jamais le contenu des conversations |
| `sessions_read` | transcriptions et état des sessions, recherche et détail : `GET /search`, `GET /info`, `GET /config-perm-suggest` (tel que construit : elle renvoie des extraits de transcriptions), `/conv*`, `/session-state`, `/session-live`, `/session-screen`, `/session-loaded`, `/session-trash` (lecture), `/sessions-stamp`, `/session-send-info` |
| `exec` | chat, nouvelle session, terminal web, envoi de touches, confiance de dossier, ouverture externe, push git, fermeture, renommage, suppression, réaffectation et corbeille de sessions, maintien au chaud (`/chat*`, `/rename`, `/rm`, `/reassign`, `/session-trash-add|restore|purge`, `POST /keepwarm`, `/new-session`, `/term-*`, `/session-send`, `/session-key`, `/session-close`, `/trust-*`, `/open`, `/git-push`) |

Règles :
- Les routes sont classées dans une table unique `route → droit`, dans le code, avec un test qui échoue si une route enregistrée dans le `mux` n'y figure pas (refus par défaut).
- Routes non nommées ci-dessus : classées une par une dans le plan (choix du rédacteur). Lecture seule du home (`/config`, `/config-effective`, `/config-projects`, `/slash-commands`, `/git-status|changes|remotes`, `/cleanup-warning`, `/tabs`, `/keepwarm` en lecture) : `mem_read`. Écritures d'état d'interface par home (`/tabs-*`, `/vgroups`) : `mem_write`. `/ls` et `/dirs` parcourent le disque hors home : `exec`. `/homes-save` crée des gld enfants avec un `CLAUDE_CONFIG_DIR` arbitraire : admin seulement. `/image-upload`, `/conv-image` : `exec` et `sessions_read`.
- Notes, onglets et keepwarm sont déjà propres à chaque home (chaque enfant a sa configuration) : ils suivent l'ACL du home, pas de portée par utilisateur.
- `home_acl` référence un identifiant de home interne et stable, pas son nom ; suppression d'un home : suppression en cascade de ses lignes, pour qu'un nom réutilisé ne reprenne pas d'anciens droits.
- L'admin global gère comptes et ACL mais n'a pas d'accès aux homes sans ligne `home_acl` (pas de super-droit implicite), il s'en accorde à lui-même.
- La liste des homes (`GET /homes`) et `POST /homes-select` ne montrent et n'acceptent que les homes où l'utilisateur a au moins un droit. Le cookie `gld_home` est revalidé à chaque requête ; tel que construit, il est effacé à la déconnexion et à chaque connexion réussie.
- Les chemins de fichiers fournis par le client restent confinés au home choisi par les contrôles existants ; l'ACL s'y ajoute, ne les remplace pas.

## Parcours de connexion

1. `POST /auth/login` {nom, mot de passe} : vérifie en temps constant (hachage factice si le nom n'existe pas). Succès : session créée avec `mfa_at` vide, réponse « second facteur requis » listant les méthodes enrôlées.
2. `POST /auth/mfa/totp` ou `POST /auth/mfa/webauthn/{begin,finish}` ou `POST /auth/mfa/recovery` : succès renseigne `mfa_at`. Tant que `mfa_at` est vide, seules les routes `/auth/*` répondent.
3. Enrôlement (première connexion ou ajout depuis le profil) : `POST /auth/totp/{begin,confirm}`, `POST /auth/webauthn/register/{begin,finish}`. Demande de réauthentification (mot de passe + facteur récent) pour ajouter ou retirer un facteur, changer le mot de passe. Tel que construit : lister ses sessions n'en demande pas ; en révoquer une, oui ; la réauthentification exige le mot de passe et un code à chaque fois, seule une assertion WebAuthn est retenue 5 minutes.
4. Déconnexion : `POST /auth/logout`, suppression de la ligne de session.
5. Le jeton (décision de l'opérateur) : en mode multi-utilisateur, le cookie `gld_token` et `?token=` cessent de fonctionner pour le navigateur ; seul le login ouvre l'interface. Le Bearer en en-tête reste accepté pour les gld enfants et l'automatisation, jamais dans l'URL, avec tous les droits de chaque home sauf `/auth/*` et `/admin/*` (tel que construit : seulement si le jeton est fixé par l'opérateur). Un secret interne distinct pour passerelle → enfant n'est pas prévu : le jeton enfant actuel (`GLD_WEB_CHILD_TOKEN`) suffit.

## Anti brute force

Principes : ne jamais distinguer « inconnu » de « mauvais mot de passe », coût constant, réponses lentes plutôt que bavardes.

- **Compteurs** dans `throttle`, clés : `user:<nom normalisé>`, `ip:<adresse>` (IPv6 ramenée au /64), `pair:<nom>|<ip>`. Fenêtre de 15 min, tel que construit fixe (elle part du premier échec) et non glissante.
- **Délai progressif** (valeurs de départ) : après 3 échecs sur une clé, délai de 2^(n-3) secondes plafonné à 15 min, avant même de vérifier le mot de passe. Après 10 échecs sur `user:`, verrouillage de 30 min, journalisé. Le compte légitime peut encore se connecter depuis l'IP habituelle (clé `pair:` non fautive) : le verrou `user:` ne s'applique qu'aux IP non vues dans les 30 derniers jours pour ce compte. Tel que construit : une suite d'échecs consécutifs (mot de passe ou second facteur, sans fenêtre) verrouille 30 min au 10e échec et 24 h au 20e, avec la même exception d'adresse connue. Choix du rédacteur ; sinon un attaquant verrouille n'importe quel compte à volonté.
- **Limite globale** : plafond de tentatives par minute sur toute la passerelle (par exemple 60) au-delà duquel les nouvelles tentatives attendent, pour borner un essai distribué.
- **Second facteur** : mêmes compteurs, seuil plus bas (5 échecs). Au-delà, la session partielle est détruite et le mot de passe doit être resaisi ; tel que construit, ces 5 échecs bloquent aussi la connexion par mot de passe du compte, depuis toute adresse, jusqu'à la fin de la fenêtre. Les codes TOTP ne se rejouent pas (`last_step`) ; fenêtre de dérive ±1 pas.
- **Réponses** : message unique « identifiants invalides ou compte temporairement bloqué » ; en-tête `Retry-After`. Aucun détail sur la raison côté client, le détail va dans `auth_events`.
- **IP du client** : `RemoteAddr` par défaut ; `X-Forwarded-For` lu seulement si `GLD_TRUSTED_PROXIES` liste le proxy (sinon forgeable).
- **Mots de passe** : minimum 12 caractères, refus des mots de passe de la liste des plus courants embarquée (tel que construit : 19 927 entrées d'au moins 12 caractères, la liste de 10 000 étant sans effet avec ce minimum), pas d'autres règles de composition.
- **Notification** : un verrouillage ou 10 échecs consécutifs apparaît dans l'interface admin (événement `lock`) et dans le journal de la passerelle.

## Architecture

- Paquet logique dans la passerelle, fichiers `auth_*.go` (store SQLite, mots de passe, TOTP, WebAuthn, sessions, throttle, ACL, routes `/auth/*` et `/admin/*`). Un middleware unique enveloppe le `mux` : identifie le principal (session, ou jeton, ou localhost en mode ouvert), applique la table des droits, résout le home et refuse sinon.
- Le middleware existant `withToken` devient une branche du même flux : mode ouvert inchangé. Tel que construit : le mode ouvert exige aucun compte **et** un marqueur signé des comptes à `off` ; des comptes supprimés avec un marqueur `on` ferment le service (503).
- Homes enfants : la passerelle ajoute `X-Gld-Child` après l'ACL ; un enfant ne voit jamais de cookie de session. La passerelle retire `Cookie` et `Authorization` de la requête transmise (à vérifier dans `homes.go` au moment du plan).
- Interface : écran de connexion et de second facteur, profil (facteurs, sessions, mot de passe), écran admin (comptes, homes, matrice de droits). Le bouton Compte actuel (usages Claude) reste distinct de ce « profil ».
- Ligne de commande : `gld user add|list|disable|enable|passwd|reset-mfa|grant|revoke` et `gld auth managed-settings`, utilisable sans serveur, écrit dans la même base.

## Complément du contrôle de lecture (choix du rédacteur)

- **WebSocket** (`/term-ws`, flux de session) : vérification de l'en-tête `Origin` à l'ouverture, flux coupé si la session expire, est révoquée ou si l'ACL change. Tel que construit : le droit est relu à chaque message pour le terminal local seulement, toutes les 5 s pour `/chat` local et pour les flux relayés à un home enfant.
- **Clé maîtresse** : si l'entrée du coffre-fort et `auth.key` existent, le coffre-fort l'emporte. Si la clé ne déchiffre pas les secrets, gld refuse de démarrer en mode multi-utilisateur (jamais de régénération silencieuse : tel que construit, aucune clé n'est générée sur une base qui a un témoin). Une clé dans un fichier voisin de la base ne protège pas contre qui lit le dossier : accepté, documenté.
- **Historique d'adresses** (règle « IP connue depuis 30 jours ») : tel que construit, des marqueurs `known:<nom>|<ip>` dans `throttle`, posés à chaque connexion réussie, et non une lecture de `auth_events`. Derrière un proxy sans `GLD_TRUSTED_PROXIES`, tous les clients partagent une adresse : avertissement au démarrage si une adresse de bouclage ou privée est vue avec un en-tête `X-Forwarded-For`.
- **Liste de mots de passe courants** : liste embarquée d'une source publique (tel que construit : 19 927 entrées).

## Modèle de menace et protection de la base (décision de l'opérateur)

Constat : un utilisateur qui a `exec` ou `config_write` exécute du code sous l'utilisateur OS de gld, donc peut lire et écrire `auth.db`, `auth.key` et le coffre-fort. L'ACL protège l'interface, pas la machine. Pas d'isolation par compte OS dans ce lot ; la protection repose sur trois mesures obligatoires et une défense complémentaire :

1. **Droits `exec` et `config_write` réservés aux personnes de confiance.** Un co-utilisateur « mémoire + usages + sessions » n'a aucun chemin vers la base. L'écran d'ACL signale en clair que `exec` et `config_write` équivalent au compte OS de gld, demande une confirmation à l'attribution, et la CLI `gld user grant` affiche le même avertissement. Aucun droit n'est accordé par défaut.
2. **Intégrité détectée par signature.** Chaque ligne de `users`, `home_acl`, `totp`, `webauthn_creds`, `recovery_codes` et `sessions` porte un HMAC-SHA-256 calculé avec une clé d'intégrité dérivée de la clé maîtresse (HKDF, contexte distinct du chiffrement). Au démarrage et à chaque lecture d'une ligne pour décider d'un accès, une signature absente ou fausse fait refuser l'accès, journalise `auth_events` (`kind=tamper`) et alerte l'admin. L'identifiant de ligne et la table entrent dans le message signé, pour qu'une ligne ne soit pas copiable ailleurs. Limite assumée : rejouer une ancienne ligne valablement signée (par exemple avant la révocation d'un droit) n'est pas détecté, la signature couvrant chaque ligne isolément ; un compteur de génération signé par table serait l'évolution si ce risque compte. Autre limite : si le même compte OS peut lire la clé maîtresse, un attaquant la lit aussi ; la signature n'arrête que l'écriture sans la clé (édition SQL directe), et c'est dit dans la documentation.

3. **Garde-fou dans les sessions lancées par gld (défense en profondeur, décision de l'opérateur).** Pour chaque session Claude démarrée par gld, la passerelle injecte :
   - des règles `permissions.deny` sur `Read` et `Edit` du dossier d'authentification (tel que construit : `Edit` couvre `Write`, une règle `Write(chemin)` n'est jamais appliquée) ;
   - un hook `PreToolUse` sur `Bash`, `PowerShell` et `Monitor` (tel que construit) qui refuse toute commande citant `auth.db`, `auth.key`, le dossier d'authentification ou lançant `sqlite3`, avec `onFailure: "block"` ; gld ne tient le fichier managed pour installé que si le hook porte ce réglage ;
   - la configuration `sandbox.filesystem` (lecture et écriture refusées sur ce dossier), seule partie appliquée par l'OS.
   Ces réglages sont fournis par un fichier « managed » appartenant à root (`/etc/claude-code/managed-settings.json` ou équivalent macOS/Windows) que l'administrateur installe ; gld en génère le contenu (`gld auth managed-settings`) mais ne peut pas l'installer seul. Sans ce fichier, un utilisateur avec `config_write` peut retirer les règles injectées : gld le détecte au démarrage et avertit. Limites assumées : le terminal web, un script ou un shell ordinaire ne passent pas par les outils de Claude, donc contournent règles et hook ; le garde-fou vise l'accident ou la dérive de Claude, pas un utilisateur malveillant ayant `exec`.

4. **Séparation par compte OS (défense complémentaire, facultative).** C'est la seule mesure qui arrête un utilisateur malveillant ayant `exec`. Le processus qui détient `auth.db` et `auth.key` (la passerelle) tourne sous un compte dédié (par exemple `gld-auth`), dossier en 0700 ; les sessions Claude, le terminal web et les gld enfants des co-utilisateurs tournent sous un autre compte sans droit sur ce dossier, lancés via `sudo -u`, `systemd-run --uid` ou un conteneur. La clé maîtresse reste inaccessible à ce second compte (coffre-fort ou `auth.key` en 0600 du compte dédié). Gld la propose sans l'exiger : un déploiement mono-compte reste valide, avec les mesures 1 à 3. Tel que construit : `GLD_RUN_AS` est expérimental et n'est **pas** une frontière de sécurité (la passerelle, qui doit tourner en root, suit encore des liens symboliques posés par le compte cible ; voir `docs/auth-followup-openroot.md`), et le démarrage est refusé sans `GLD_RUN_AS_EXPERIMENTAL=1`. Le plan la traitait comme un lot séparé et activable par configuration (`GLD_RUN_AS`), après le reste ; sa mise en œuvre change la façon dont gld lance tmux, claude et les enfants (`homes.go`, `term*.go`, `chat.go`).

Hors périmètre, notés pour plus tard : bac à sable du terminal web et de tout shell lancé par `exec`.

## Laissé au constructeur

- Schéma SQL exact, noms de colonnes, outil de migration (simple liste de fichiers embarqués suffit).
- Choix des versions de bibliothèques et API exactes (à relever avec Context7).
- Découpage en tâches et ordre ; recommandation : store + CLI + mots de passe, sessions + middleware + table de routes, TOTP, WebAuthn, anti brute force, interface, ACL par home dans la passerelle.
- Textes et traductions.
- Valeurs finales des seuils anti brute force.

## Risques connus

- Perte de la clé maîtresse : les secrets TOTP deviennent illisibles ; la ligne de commande a besoin de la clé pour ouvrir la base, donc `gld user reset-mfa` ne sert à rien. Procédure : retrouver la clé, sinon déplacer `auth.db` et `auth.key` hors du dossier et recréer le premier administrateur (comptes, facteurs et droits à refaire).
- Serveur sans coffre-fort OS : la clé maîtresse en fichier protège moins qu'un coffre-fort ; documenté.
- WebAuthn exige HTTPS ou localhost et un `RPID` stable : `GLD_PUBLIC_URL` à fixer derrière un proxy.
- Les droits ne protègent pas contre un utilisateur ayant `exec` : à documenter clairement dans l'écran d'ACL.
