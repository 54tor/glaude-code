# Multi-utilisateur, multi-home, ACL, 2FA : conception

Statut : proposition, rien n'est construit. Périmètre de ce lot : cette spec puis un plan d'implémentation (`superpowers:writing-plans`).

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
- Isolation système entre utilisateurs : tous les homes tournent sous le même utilisateur OS. L'ACL protège l'interface, pas le système de fichiers.
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
| Jeton actuel | Mode multi-utilisateur opt-in : sans compte, comportement actuel ; dès le premier admin, le login remplace le jeton pour l'interface, le jeton reste valable en Bearer (enfants, automatisation) |

## Choix du rédacteur (à relire)

- **Dossier de données** : `os.UserConfigDir()/gld/auth.db` (Linux `~/.config/gld`, macOS `~/Library/Application Support/gld`, Windows `%AppData%\gld`), dossier en 0700, fichier en 0600. Surchargeable par `GLD_AUTH_DIR`. Distinct du dossier `CLAUDE_CONFIG_DIR` pour que la sauvegarde ou la synchro d'un home n'emporte pas les comptes.
- **Chiffrement au repos** : les secrets TOTP et tout champ réversible sont chiffrés en AES-256-GCM (stdlib) avec une clé maîtresse de 32 octets. La clé vit dans le coffre-fort de l'OS (`github.com/zalando/go-keyring`) ; à défaut (serveur sans coffre), dans `auth.key` en 0600 à côté de la base, et gld avertit au démarrage que la protection est plus faible. `GLD_AUTH_KEY` (variable d'environnement) l'emporte sur les deux. Les mots de passe ne sont jamais chiffrés mais hachés (argon2id, `golang.org/x/crypto/argon2`, paramètres stockés avec le hachage pour pouvoir les relever). Les jetons de session et codes de secours sont stockés hachés (SHA-256 pour les jetons à 256 bits d'entropie, argon2id pour les codes de secours).
- **Bibliothèques** : `github.com/go-webauthn/webauthn` (WebAuthn), `github.com/pquerna/otp` (TOTP), `golang.org/x/crypto/argon2`. À confirmer au moment du plan via Context7 (versions, API).
- **Sessions** : cookie `gld_session`, valeur opaque de 32 octets aléatoires, `HttpOnly`, `SameSite=Strict`, `Secure` dès que la requête arrive en HTTPS (WebAuthn l'exige de toute façon hors localhost). Expiration glissante 12 h, absolue 30 jours, révocables par l'utilisateur et l'admin. Jeton CSRF pour les requêtes qui modifient l'état, en plus de `SameSite`.
- **Second facteur** : obligatoire pour tout compte en mode multi-utilisateur. Au moins un facteur (TOTP ou clé WebAuthn) enrôlé à la première connexion, plus 10 codes de secours à usage unique affichés une seule fois. Un admin peut réinitialiser les facteurs d'un compte, ce qui force un nouvel enrôlement.
- **Premier admin** : créé en ligne de commande (`gld user add --admin <nom>`, mot de passe saisi sans écho), jamais par l'interface web, ce qui évite la course à l'inscription. `gld user grant <nom> <home> <droits…>` donne des droits depuis la ligne de commande ; sans lui, l'admin n'aurait accès à aucun home au premier lancement. Ensuite l'interface « Utilisateurs » (admin) gère comptes, homes et droits.
- **Anti brute force** : voir section dédiée ; les seuils proposés sont des valeurs de départ réglables.

## Modèle de données

SQLite, migrations numérotées appliquées au démarrage (un seul processus écrit : la passerelle).

- `users(id, name UNIQUE COLLATE NOCASE, pw_hash, is_admin, disabled, created_at, pw_changed_at)`
- `totp(user_id, secret_enc, nonce, confirmed_at, last_step)` : `last_step` interdit de rejouer un code déjà accepté.
- `webauthn_creds(id, user_id, cred_id, public_key, sign_count, transports, label, created_at, last_used_at)`
- `recovery_codes(user_id, hash, used_at)`
- `sessions(id_hash, user_id, created_at, last_seen_at, expires_at, ip, ua, mfa_at)`
- `home_acl(user_id, home_name, mem_read, mem_write, mem_sync, usage_read, exec)` : un droit absent vaut refusé. `home_name` est le nom du home dans `Home.Name`.
- `auth_events(at, user_id NULL, name_tried, ip, kind, outcome)` : connexions, échecs, verrouillages, changements de facteur, modifications d'ACL. Purgé après 90 jours.
- `throttle(key, fails, first_at, locked_until)` : voir anti brute force.

## Droits

| Droit | Donne accès à (lecture ou écriture) |
|---|---|
| `mem_read` | liste et contenu de la mémoire, règles, snippets, notes, recherche, santé, tokens chargés (`GET /memory-*`, `/snippet*`, `/notes`, `/search`, `/skills-list`, `/skill`, `/trash*`) |
| `mem_write` | enregistrer, déplacer, supprimer, réparer, fusionner du texte inerte : CLAUDE.md, règles, entrées de mémoire, notes, snippets, corbeille, renommage et suppression de fichiers mémoire (`POST /memory-*`, `/snippet-*`, `/notes`, `/trash-*`, `/rename`, `/rm`) |
| `config_write` | réglages et skills, qui déclenchent l'exécution de code par Claude Code : `/config-edit`, `/config-raw`, `/config-restore`, `/config-perm-import`, `/skill-save`, `/skill-copy`, `/skill-rm`, `/config-mcp-status` (peut lancer des serveurs MCP). Équivaut en pratique à `exec`, écrit tel quel dans l'écran d'ACL |
| `mem_sync` | prévisualiser et appliquer la synchro entre homes (`/homes-sync-plan`, `/homes-sync-apply`). Exige aussi `mem_read` sur la source et `mem_write` sur la destination ; chaque home est vérifié séparément |
| `usage_read` | compte et usages (`GET /account`, `/info`, statistiques), jamais le contenu des conversations |
| `sessions_read` | transcriptions et état des sessions : `/conv*`, `/session-state`, `/session-live`, `/session-screen`, `/session-loaded`, `/session-trash` (lecture), `/sessions-stamp`, `/session-send-info` |
| `exec` | chat, nouvelle session, terminal web, envoi de touches, confiance de dossier, ouverture externe, push git, fermeture de session (`/chat*`, `/new-session`, `/term-*`, `/session-send`, `/session-key`, `/session-close`, `/trust-*`, `/open`, `/git-push`) |

Règles :
- Les routes sont classées dans une table unique `route → droit`, dans le code, avec un test qui échoue si une route enregistrée dans le `mux` n'y figure pas (refus par défaut).
- Routes non nommées ci-dessus : classées une par une dans le plan (choix du rédacteur). Lecture seule du home (`/config`, `/config-effective`, `/config-projects`, `/slash-commands`, `/git-status|changes|remotes`, `/cleanup-warning`, `/tabs`, `/keepwarm` en lecture) : `mem_read`. Écritures d'état d'interface par home (`/tabs-*`, `/keepwarm`, `/vgroups`, `/reassign`, `/session-trash-*`) : `mem_write`. `/ls` et `/dirs` parcourent le disque hors home : `exec`. `/homes-save` crée des gld enfants avec un `CLAUDE_CONFIG_DIR` arbitraire : admin seulement. `/image-upload`, `/conv-image` : `exec` et `sessions_read`.
- Notes, onglets et keepwarm sont déjà propres à chaque home (chaque enfant a sa configuration) : ils suivent l'ACL du home, pas de portée par utilisateur.
- `home_acl` référence un identifiant de home interne et stable, pas son nom ; suppression d'un home : suppression en cascade de ses lignes, pour qu'un nom réutilisé ne reprenne pas d'anciens droits.
- L'admin global gère comptes et ACL mais n'a pas d'accès aux homes sans ligne `home_acl` (pas de super-droit implicite), il s'en accorde à lui-même.
- La liste des homes (`GET /homes`) et `POST /homes-select` ne montrent et n'acceptent que les homes où l'utilisateur a au moins un droit. Le cookie `gld_home` est revalidé à chaque requête.
- Les chemins de fichiers fournis par le client restent confinés au home choisi par les contrôles existants ; l'ACL s'y ajoute, ne les remplace pas.

## Parcours de connexion

1. `POST /auth/login` {nom, mot de passe} : vérifie en temps constant (hachage factice si le nom n'existe pas). Succès : session créée avec `mfa_at` vide, réponse « second facteur requis » listant les méthodes enrôlées.
2. `POST /auth/mfa/totp` ou `POST /auth/mfa/webauthn/{begin,finish}` ou `POST /auth/mfa/recovery` : succès renseigne `mfa_at`. Tant que `mfa_at` est vide, seules les routes `/auth/*` répondent.
3. Enrôlement (première connexion ou ajout depuis le profil) : `POST /auth/totp/{begin,confirm}`, `POST /auth/webauthn/register/{begin,finish}`. Demande de réauthentification (mot de passe + facteur récent) pour ajouter ou retirer un facteur, changer le mot de passe, voir les sessions.
4. Déconnexion : `POST /auth/logout`, suppression de la ligne de session.
5. Le jeton (décision de l'opérateur) : en mode multi-utilisateur, le cookie `gld_token` et `?token=` cessent de fonctionner pour le navigateur ; seul le login ouvre l'interface. Le Bearer en en-tête reste accepté pour les gld enfants et l'automatisation, jamais dans l'URL, avec toutes les routes d'un home sauf `/auth/*` et `/admin/*`. Un secret interne distinct pour passerelle → enfant n'est pas prévu : le jeton enfant actuel (`GLD_WEB_CHILD_TOKEN`) suffit.

## Anti brute force

Principes : ne jamais distinguer « inconnu » de « mauvais mot de passe », coût constant, réponses lentes plutôt que bavardes.

- **Compteurs** dans `throttle`, clés : `user:<nom normalisé>`, `ip:<adresse>` (IPv6 ramenée au /64), `pair:<nom>|<ip>`. Fenêtre glissante 15 min.
- **Délai progressif** (valeurs de départ) : après 3 échecs sur une clé, délai de 2^(n-3) secondes plafonné à 15 min, avant même de vérifier le mot de passe. Après 10 échecs sur `user:`, verrouillage de 30 min, journalisé. Le compte légitime peut encore se connecter depuis l'IP habituelle (clé `pair:` non fautive) : le verrou `user:` ne s'applique qu'aux IP non vues dans les 30 derniers jours pour ce compte. Choix du rédacteur ; sinon un attaquant verrouille n'importe quel compte à volonté.
- **Limite globale** : plafond de tentatives par minute sur toute la passerelle (par exemple 60) au-delà duquel les nouvelles tentatives attendent, pour borner un essai distribué.
- **Second facteur** : mêmes compteurs, seuil plus bas (5 échecs). Au-delà, la session partielle est détruite et le mot de passe doit être resaisi. Les codes TOTP ne se rejouent pas (`last_step`) ; fenêtre de dérive ±1 pas.
- **Réponses** : message unique « identifiants invalides ou compte temporairement bloqué » ; en-tête `Retry-After`. Aucun détail sur la raison côté client, le détail va dans `auth_events`.
- **IP du client** : `RemoteAddr` par défaut ; `X-Forwarded-For` lu seulement si `GLD_TRUSTED_PROXIES` liste le proxy (sinon forgeable).
- **Mots de passe** : minimum 12 caractères, refus des mots de passe de la liste des plus courants embarquée (liste courte, 10 000 entrées), pas d'autres règles de composition.
- **Notification** : un verrouillage ou 10 échecs consécutifs apparaît dans l'interface admin et dans le journal de la passerelle.

## Architecture

- Paquet logique dans la passerelle, fichiers `auth_*.go` (store SQLite, mots de passe, TOTP, WebAuthn, sessions, throttle, ACL, routes `/auth/*` et `/admin/*`). Un middleware unique enveloppe le `mux` : identifie le principal (session, ou jeton, ou localhost en mode ouvert), applique la table des droits, résout le home et refuse sinon.
- Le middleware existant `withToken` devient une branche du même flux : mode ouvert (aucun compte en base) inchangé.
- Homes enfants : la passerelle ajoute `X-Gld-Child` après l'ACL ; un enfant ne voit jamais de cookie de session. La passerelle retire `Cookie` et `Authorization` de la requête transmise (à vérifier dans `homes.go` au moment du plan).
- Interface : écran de connexion et de second facteur, profil (facteurs, sessions, mot de passe), écran admin (comptes, homes, matrice de droits). Le bouton Compte actuel (usages Claude) reste distinct de ce « profil ».
- Ligne de commande : `gld user add|list|disable|reset-mfa|passwd`, utilisable sans serveur, écrit dans la même base.

## Complément du contrôle de lecture (choix du rédacteur)

- **WebSocket** (`/term-ws`, flux de session) : vérification de l'en-tête `Origin` à l'ouverture, droit revérifié à chaque message et flux coupé si la session expire, est révoquée ou si l'ACL change.
- **Clé maîtresse** : si l'entrée du coffre-fort et `auth.key` existent, le coffre-fort l'emporte. Si la clé ne déchiffre pas les secrets, gld refuse de démarrer en mode multi-utilisateur (jamais de régénération silencieuse). Une clé dans un fichier voisin de la base ne protège pas contre qui lit le dossier : accepté, documenté.
- **Historique d'adresses** (règle « IP connue depuis 30 jours ») : lu dans `auth_events`, filtré sur les connexions réussies. Derrière un proxy sans `GLD_TRUSTED_PROXIES`, tous les clients partagent une adresse : avertissement au démarrage si une adresse de bouclage ou privée est vue avec un en-tête `X-Forwarded-For`.
- **Liste de mots de passe courants** : liste embarquée de 10 000 entrées d'une source publique, détail laissé au constructeur.

## Laissé au constructeur

- Schéma SQL exact, noms de colonnes, outil de migration (simple liste de fichiers embarqués suffit).
- Choix des versions de bibliothèques et API exactes (à relever avec Context7).
- Découpage en tâches et ordre ; recommandation : store + CLI + mots de passe, sessions + middleware + table de routes, TOTP, WebAuthn, anti brute force, interface, ACL par home dans la passerelle.
- Textes et traductions.
- Valeurs finales des seuils anti brute force.

## Risques connus

- Perte de la clé maîtresse : les secrets TOTP deviennent illisibles ; la procédure de repli est `gld user reset-mfa` et réenrôlement.
- Serveur sans coffre-fort OS : la clé maîtresse en fichier protège moins qu'un coffre-fort ; documenté.
- WebAuthn exige HTTPS ou localhost et un `RPID` stable : `GLD_PUBLIC_URL` à fixer derrière un proxy.
- Les droits ne protègent pas contre un utilisateur ayant `exec` : à documenter clairement dans l'écran d'ACL.
