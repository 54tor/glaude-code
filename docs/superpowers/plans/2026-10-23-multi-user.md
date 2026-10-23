# Multi-utilisateur, multi-home, ACL, 2FA : plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Comptes locaux avec mot de passe et second facteur (TOTP, WebAuthn), droits indépendants par home, défense anti brute force, base SQLite chiffrée et signée, sans rien changer au mode actuel tant qu'aucun compte n'existe.

**Architecture:** Tout vit dans la passerelle (package `main` de `gld/`, fichiers `auth_*.go`). Un middleware `authGate` s'insère entre `CrossOriginProtection` et `withHomes` dans `handler()` (`main.go`) : il identifie le principal, retrouve le droit requis par le motif de route (`mux.Handler(r)`), vérifie le home du cookie `gld_home`, puis transmet. Les gld enfants (`childToken != ""`) ne portent pas de garde d'authentification.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (déjà présent), `github.com/go-webauthn/webauthn`, `github.com/pquerna/otp`, `golang.org/x/crypto` (argon2, hkdf), `github.com/zalando/go-keyring`, React/shadcn pour l'interface.

**Spec:** `docs/superpowers/specs/2026-10-23-multi-user-design.md`

## Global Constraints

- Pas de CGO ; Linux, macOS, Windows (chemins et permissions de fichier).
- Dossier de données : `os.UserConfigDir()/gld` (0700), `auth.db` et `auth.key` en 0600 ; `GLD_AUTH_DIR` le surcharge.
- Droits : `mem_read`, `mem_write`, `config_write`, `mem_sync`, `usage_read`, `sessions_read`, `exec` ; absent = refusé ; routes non classées = refusées.
- Mots de passe : argon2id, 12 caractères minimum, 1024 octets maximum ; jamais chiffrés.
- Sessions : cookie `gld_session`, 32 octets aléatoires, `HttpOnly`, `SameSite=Strict`, `Secure` si HTTPS ; expiration glissante 12 h, absolue 30 jours ; stockées hachées (SHA-256).
- TOTP : secrets chiffrés AES-256-GCM, dérive ±1 pas, `last_step` interdit le rejeu. Codes de secours : 10, usage unique, hachés argon2id.
- Anti brute force : fenêtre 15 min ; délai 2^(n-3) s plafonné à 15 min dès 3 échecs ; verrou de 30 min après 10 échecs `user:` (seulement pour les adresses non vues depuis 30 jours) ; 5 échecs au second facteur détruisent la session partielle ; plafond global 60 tentatives par minute ; message unique « identifiants invalides ou compte temporairement bloqué » avec `Retry-After`.
- Jeton en mode multi-utilisateur : Bearer en en-tête seulement, jamais cookie `gld_token` ni `?token=`, sans routes `/auth/*` ni `/admin/*`.
- Textes : clés i18n `locales/{fr,en}.json` (serveur) et `ui/src/i18n/{fr,en}/` (interface), jamais de texte en dur.
- Commits : `type(scope): sujet` seul, sans corps ni trailer, scope `auth` ; un commit par tâche ; jamais de push. Tests : `go test -count=1 -run <Nom> .` ciblé ; un seul `make check` à la fin.
- Commentaires de code : un *pourquoi* non évident, aucun renvoi à la spec.

## Review Focus

- Nom inconnu et mauvais mot de passe : même statut, même corps, durées comparables.
- Route enregistrée sans entrée dans la table des droits : le test échoue ; variantes `//memory-save`, `/MEMORY-SAVE`, `/memory-save/` ne contournent pas le classement.
- Cookie `gld_home` désignant un home sans droit : 403, jamais le repli sur le home principal que `withHomes` fait aujourd'hui.
- Code TOTP déjà accepté, et code à la limite de la dérive : rejeu refusé, ±1 pas accepté, ±2 refusé.
- Droit retiré ou session révoquée pendant un WebSocket `/term-ws` ouvert : flux coupé.
- Ligne `home_acl` copiée depuis un autre utilisateur ou modifiée en SQL : signature invalide, accès refusé, événement `tamper`.
- Mot de passe vide, de 5 000 octets, avec octet nul ; nom d'utilisateur en Unicode équivalent (casse, NFKC) : un seul compte.
- Deux connexions concurrentes ratées sur le même nom : compteur exact, pas de course.

---

### Task 1: Dépendances et magasin SQLite

**Files:**
- Create: `gld/auth_store.go`, `gld/auth_store_test.go`
- Modify: `gld/go.mod`, `gld/go.sum`

**Interfaces:**
- Produces: `func authDir() string` ; `type authDB struct{ *sql.DB }` ; `func openAuthDB(dir string) (*authDB, error)` ; table `schema_version`.

- [ ] **Step 1:** Relever les API et versions courantes de `go-webauthn/webauthn`, `pquerna/otp`, `zalando/go-keyring`, `golang.org/x/crypto` via Context7 ; `go get` chaque module ; noter les versions dans le message du commit n'est pas requis.
- [ ] **Step 2: Test** `TestOpenAuthDB` : `openAuthDB(t.TempDir())` crée `auth.db` en 0600 (hors Windows) et le dossier en 0700, rouvrir est idempotent, `schema_version` vaut le nombre de migrations ; `TestAuthDirEnv` : `GLD_AUTH_DIR` l'emporte sur `os.UserConfigDir()/gld`.
- [ ] **Step 3: Run** `go test -count=1 -run 'TestOpenAuthDB|TestAuthDirEnv' .` → FAIL (symboles absents).
- [ ] **Step 4: Implement** `authDir`, `openAuthDB` (`modernc.org/sqlite`, `PRAGMA journal_mode=WAL; foreign_keys=ON; busy_timeout=5000`) et les migrations numérotées embarquées (`//go:embed auth_migrations/*.sql`). Schéma `0001` : tables `users`, `totp`, `webauthn_creds`, `recovery_codes`, `sessions`, `home_acl`, `auth_events`, `throttle` avec les colonnes de la spec, plus `sig BLOB` sur les cinq tables sensibles.
- [ ] **Step 5: Run** la même commande → PASS.
- [ ] **Step 6: Commit** `feat(auth): magasin SQLite et migrations`.

### Task 2: Clé maîtresse, chiffrement, signature des lignes

**Files:**
- Create: `gld/auth_crypto.go`, `gld/auth_crypto_test.go`

**Interfaces:**
- Produces: `func loadMasterKey(dir string) ([]byte, error)` (ordre : `GLD_AUTH_KEY` hexadécimal, coffre-fort OS, `auth.key` 0600 créé au besoin) ; `func seal(key, plain []byte) []byte` et `func open(key, boxed []byte) ([]byte, error)` (AES-256-GCM, nonce en tête) ; `func rowSig(key []byte, table, id string, fields ...string) []byte` (HMAC-SHA-256, clé dérivée par HKDF, contexte `"gld-auth-integrity"`) ; `func rowSigOK(key []byte, table, id string, sig []byte, fields ...string) bool`.

- [ ] **Step 1: Tests** : `TestSealOpen` (aller-retour, nonce distinct à chaque appel, `open` échoue sur un octet modifié) ; `TestMasterKeyOrder` (variable d'environnement > fichier ; fichier créé en 0600, 32 octets ; une clé qui ne déchiffre pas un secret connu fait échouer `checkMasterKey`) ; `TestRowSig` (la signature change si `table`, `id` ou un champ change ; copier la signature d'une ligne sur une autre `id` échoue).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** avec `crypto/aes`, `crypto/cipher`, `crypto/hkdf` (stdlib) ; coffre-fort via `go-keyring` (service `gld`, usage `auth-master-key`), repli fichier si l'appel échoue ; `checkMasterKey(db *authDB, key []byte) error` relit un témoin chiffré dans `meta` et refuse de démarrer si le déchiffrement échoue (jamais de régénération silencieuse).
- [ ] **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): clé maîtresse, chiffrement et signature des lignes`.

### Task 3: Mots de passe

**Files:**
- Create: `gld/auth_password.go`, `gld/auth_password_test.go`, `gld/auth_commonpw.txt` (10 000 entrées d'une liste publique, embarquée)

**Interfaces:**
- Produces: `func hashPassword(pw string) (string, error)` (argon2id, paramètres dans la chaîne) ; `func checkPassword(hash, pw string) bool` (temps constant) ; `func dummyCheck()` (un hachage factice pour égaliser le temps d'un nom inconnu) ; `func validPassword(pw string) error` (12 à 1024 octets, pas d'octet nul, hors liste courante) ; `func normName(s string) string` (NFKC puis minuscules, rognage).

- [ ] **Step 1: Tests** : `TestPasswordRoundTrip` ; `TestPasswordRules` (11 caractères, 1025 octets, `"password1234"`, octet nul → erreurs ; 12 caractères corrects → nil) ; `TestNormName` (`"Éa"` et `"Éa"` donnent la même valeur ; `"ＡＬＩＣＥ"` donne `"alice"`) ; `TestDummyCheckTiming` (durée de `dummyCheck` dans un facteur 3 de `checkPassword` sur un vrai hachage).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** avec `golang.org/x/crypto/argon2` (m=64 Mio, t=3, p=2 : valeurs de départ, relevables), `golang.org/x/text/unicode/norm`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): mots de passe argon2id`.

### Task 4: Utilisateurs, droits stockés et commande `gld user`

**Files:**
- Create: `gld/auth_users.go`, `gld/auth_users_test.go`, `gld/auth_cli.go`
- Modify: `gld/cli.go` (enregistrer la commande `user`), `gld/locales/fr.json`, `gld/locales/en.json`

**Interfaces:**
- Consumes: `openAuthDB`, `loadMasterKey`, `rowSig`, `hashPassword`, `validPassword`, `normName`.
- Produces: `type Right string` avec les constantes `RightMemRead`, `RightMemWrite`, `RightConfigWrite`, `RightMemSync`, `RightUsageRead`, `RightSessionsRead`, `RightExec` ; `type authStore struct{ db *authDB; key []byte }` ; méthodes `AddUser(name, pw string, admin bool) (int64, error)`, `UserByName(name string) (*User, error)` (vérifie la signature), `SetDisabled(id int64, off bool) error`, `SetPassword(id int64, pw string) error`, `Grant(userID int64, homeID string, rights ...Right) error`, `Rights(userID int64, homeID string) (map[Right]bool, error)` (signature vérifiée, sinon erreur `errTamper` et ligne `auth_events` `kind=tamper`), `ResetMFA(userID int64) error`, `HasUsers() bool`.
- CLI : `gld user add [--admin] <nom>` (mot de passe lu sans écho depuis le terminal, deux fois), `list`, `disable|enable <nom>`, `passwd <nom>`, `reset-mfa <nom>`, `grant <nom> <home> <droits…>` (avertit et demande confirmation pour `exec` et `config_write`, drapeau `--yes` pour l'automatisation).

- [ ] **Step 1: Tests** : `TestAddUserNameUnique` (deux noms équivalents par `normName` → erreur) ; `TestRightsTamper` (modifier une ligne `home_acl` en SQL direct → `Rights` renvoie `errTamper` et une ligne `tamper` existe) ; `TestRightsRowCopy` (copier la ligne d'un autre utilisateur → `errTamper`) ; `TestGrantDefaultNone` (aucun droit tant qu'il n'est pas accordé) ; `TestUserCLIGrantNeedsConfirmation` (`exec` sans `--yes` et sans terminal → échec).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** ; la signature couvre `(table, id, user_id|home_id|champs de droits)`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): utilisateurs, droits signés et commande gld user`.

### Task 5: Anti brute force

**Files:**
- Create: `gld/auth_throttle.go`, `gld/auth_throttle_test.go`

**Interfaces:**
- Produces: `type throttle struct{ db *authDB; now func() time.Time }` ; `func (t *throttle) Check(user, ip string) (wait time.Duration, blocked bool)` ; `func (t *throttle) Fail(user, ip string, stage string)` (`stage` vaut `"password"` ou `"mfa"`) ; `func (t *throttle) Success(user, ip string)` ; `func (t *throttle) KnownIP(userID int64, ip string) bool` (lit les connexions réussies de `auth_events` sur 30 jours) ; `func clientIP(r *http.Request) string` (`RemoteAddr`, IPv6 ramenée au /64, `X-Forwarded-For` lu seulement si `GLD_TRUSTED_PROXIES` contient l'adresse du pair).

- [ ] **Step 1: Tests** (horloge injectée) : `TestBackoff` (2 échecs : 0 s ; 3 : 1 s ; 4 : 2 s ; 8 : 32 s ; plafond 900 s) ; `TestUserLockUnknownIP` (10 échecs depuis des IP variées → verrou 30 min pour une IP inconnue, mais l'IP déjà vue réussit) ; `TestMFAThreshold` (5 échecs `mfa` → `blocked`) ; `TestGlobalCap` (61e tentative en une minute attend) ; `TestThrottleConcurrent` (20 goroutines `Fail` sur la même clé → compteur égal à 20) ; `TestClientIPXFF` (`X-Forwarded-For` ignoré sans proxy de confiance, pris avec).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** en transactions SQLite (`INSERT … ON CONFLICT DO UPDATE`), clés `user:`, `ip:`, `pair:` selon la spec. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): heuristique anti brute force`.

### Task 6: Sessions, cookies et CSRF

**Files:**
- Create: `gld/auth_sessions.go`, `gld/auth_sessions_test.go`

**Interfaces:**
- Consumes: `authStore`, `clientIP`.
- Produces: `type Principal struct{ User *User; SessionID string; MFA bool; CSRF string; Token bool }` ; `func (s *authStore) NewSession(userID int64, ip, ua string) (cookie string, err error)` ; `func (s *authStore) Session(cookie string) (*Principal, error)` (met à jour `last_seen_at`, refuse expirée) ; `func (s *authStore) MarkMFA(sessionID string) error` ; `func (s *authStore) Revoke(sessionID string)` ; `func (s *authStore) RevokeUser(userID int64)` ; `func setSessionCookie(w http.ResponseWriter, r *http.Request, v string)` (`Secure` si `r.TLS != nil` ou `X-Forwarded-Proto: https` d'un proxy de confiance) ; en-tête `X-Gld-CSRF` exigé sur les requêtes non sûres, valeur dérivée de la session.

- [ ] **Step 1: Tests** : `TestSessionExpiry` (glissante 12 h, absolue 30 jours) ; `TestSessionHashed` (le cookie en clair n'apparaît dans aucune colonne) ; `TestRevokeUser` ; `TestCSRFRequired` (POST sans `X-Gld-CSRF` → 403, GET passe).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement. Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): sessions et jeton CSRF`.

### Task 7: TOTP et codes de secours

**Files:**
- Create: `gld/auth_totp.go`, `gld/auth_totp_test.go`

**Interfaces:**
- Produces: `func (s *authStore) TOTPBegin(userID int64, account string) (secret, uri string, err error)` (secret en attente, chiffré) ; `func (s *authStore) TOTPConfirm(userID int64, code string) error` ; `func (s *authStore) TOTPVerify(userID int64, code string, at time.Time) bool` (essaie les pas −1, 0, +1, refuse un pas `<= last_step`) ; `func (s *authStore) NewRecoveryCodes(userID int64) ([]string, error)` (10 codes, affichés une fois) ; `func (s *authStore) UseRecoveryCode(userID int64, code string) bool`.

- [ ] **Step 1: Tests** : `TestTOTPDrift` (pas ±1 acceptés, ±2 refusés) ; `TestTOTPReplay` (même code deux fois → second refusé) ; `TestTOTPSecretEncrypted` (la colonne ne contient pas le secret en clair) ; `TestRecoveryOnce` (second usage refusé ; 10 codes distincts).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** avec `github.com/pquerna/otp/totp` (`GenerateCodeCustom` par pas, pour connaître le pas accepté). **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): TOTP et codes de secours`.

### Task 8: WebAuthn

**Files:**
- Create: `gld/auth_webauthn.go`, `gld/auth_webauthn_test.go`

**Interfaces:**
- Produces: `func newWebAuthn() (*webauthn.WebAuthn, error)` (`RPID` et origines tirés de `GLD_PUBLIC_URL`, repli `localhost` en local) ; méthodes `(s *authStore) WebAuthnBeginRegister(userID int64) (options any, err error)`, `WebAuthnFinishRegister(userID int64, label string, r *http.Request) error`, `WebAuthnBeginLogin(userID int64) (options any, err error)`, `WebAuthnFinishLogin(userID int64, r *http.Request) error` ; la session de défi est gardée en mémoire 5 minutes, liée à la session HTTP ; `sign_count` mis à jour, régression de compteur refusée.

- [ ] **Step 1: Tests** avec l'authentificateur logiciel de la bibliothèque (ou un double signé par `webauthn/protocol/webauthncose`) : `TestWebAuthnRegisterLogin` (enrôlement puis connexion) ; `TestWebAuthnCounterRegression` (compteur plus bas → refus) ; `TestWebAuthnChallengeExpiry` ; `TestWebAuthnNeedsHTTPS` (origine `http://` non locale → `newWebAuthn` renvoie une erreur claire).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** ; les clés publiques ne sont pas secrètes mais portent `sig`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): WebAuthn`.

### Task 9: Routes `/auth/*` (connexion, second facteur, profil)

**Files:**
- Create: `gld/auth_routes.go`, `gld/auth_routes_test.go`
- Modify: `gld/locales/fr.json`, `gld/locales/en.json`

**Interfaces:**
- Consumes: Tasks 3 à 8.
- Produces: `func routesAuth(mux *http.ServeMux, s *authStore, th *throttle)` enregistrant `POST /auth/login`, `POST /auth/mfa/totp`, `POST /auth/mfa/recovery`, `POST /auth/mfa/webauthn/begin|finish`, `POST /auth/logout`, `GET /auth/me`, `POST /auth/totp/begin|confirm`, `POST /auth/webauthn/register/begin|finish`, `POST /auth/password`, `GET /auth/sessions`, `POST /auth/sessions/revoke`. Réponse d'échec unique : 401 `{"ok":false,"out":L("srv.auth.invalide")}` avec `Retry-After`. Réauthentification (mot de passe plus facteur de moins de 5 minutes) pour changer un facteur ou le mot de passe.

- [ ] **Step 1: Tests** : `TestLoginUnknownVsWrong` (mêmes statut et corps ; durées dans un facteur 3) ; `TestLoginThenMFAGate` (après mot de passe seul, `/auth/me` répond « second facteur requis » et aucune autre route n'ouvre) ; `TestLoginThrottled` (4e échec → `Retry-After` > 0 sans vérifier le hachage) ; `TestMFAFailuresKillSession` (5 échecs détruisent la session partielle) ; `TestLoginPasswordLimits` (vide, 5 000 octets, octet nul → 401 sans calcul argon2) ; `TestEnrollRequiresReauth`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement**, journalisant chaque issue dans `auth_events`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): routes de connexion et de second facteur`.

### Task 10: Table des droits, identifiant stable de home et garde `authGate`

**Files:**
- Create: `gld/auth_acl.go`, `gld/auth_acl_test.go`
- Modify: `gld/homes.go` (champ `ID` de `Home`, généré et migré dans `loadHomes`/`saveHomes`, valeur `"main"` pour le home principal ; `withHomes` refuse au lieu de replier quand le garde est actif), `gld/main.go`

**Interfaces:**
- Produces: `var routeRights = map[string]Right` (motif `"METHOD /chemin"` → droit ; `""` pour une route publique du garde comme `GET /` statique ; `rightAdmin` pour `/admin/*` et `POST /homes-save`) ; `func authGate(mux *http.ServeMux, s *authStore, next http.Handler) http.Handler` ; `func homeIDFor(name string) (string, bool)` ; `GET /homes` filtré aux homes où l'utilisateur a au moins un droit ; `POST /homes-select` refuse un home sans droit ; `/homes-sync-*` exigent `mem_read` sur la source et `mem_write` sur la destination plus `mem_sync` sur les deux.
- Classement des routes selon la section « Droits » de la spec (tableau et règles de classement, `/ls` et `/dirs` en `exec`, `/image-upload` en `exec`, `/conv-image` en `sessions_read`).

- [ ] **Step 1: Tests** : `TestEveryRouteClassified` (analyse les fichiers `*.go` pour chaque littéral `HandleFunc("<motif>"` et exige une entrée dans `routeRights`, hors tests) ; `TestGateDeniesByDefault` (route fictive absente de la table → 403) ; `TestGatePathVariants` (`//memory-save`, `/MEMORY-SAVE`, `/memory-save/` n'obtiennent jamais un droit plus faible que `/memory-save`) ; `TestGateHomeCookieNoRights` (cookie `gld_home` d'un home sans droit → 403, pas de repli) ; `TestGateMemSyncNeedsBoth` ; `TestGateRightsMatrix` (un utilisateur `mem_read` seul : GET mémoire 200, POST mémoire 403, `/chat` 403).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** ; `Rights` relu à chaque requête (pas de cache, pour que la révocation soit immédiate). **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): table des droits et garde par home`.

### Task 11: Branchement dans `main.go` et mode jeton

**Files:**
- Modify: `gld/main.go`, `gld/listen.go`, `gld/auth_routes.go` (montage), `gld/auth_acl.go`
- Test: `gld/auth_mode_test.go`

**Interfaces:**
- Produces: `func authMode(s *authStore) bool` (vrai si `HasUsers()` et processus non enfant) ; en mode actif : `withToken` est remplacé par `tokenForChildren` (Bearer en en-tête seulement, hors `/auth/*` et `/admin/*`, jamais cookie ni `?token=`) ; la page de connexion est servie à toute requête non authentifiée de navigateur (`Accept: text/html`) ; WebSocket : `Origin` contrôlé à l'ouverture, droit revérifié par `authWS(r, right)` toutes les 5 secondes et à chaque message, flux coupé sur échec.

- [ ] **Step 1: Tests** : `TestOpenModeUnchanged` (sans utilisateur, comportement identique à `withToken` actuel) ; `TestTokenIgnoredInURLWhenAuth` (`?token=` valide → 401 en mode multi-utilisateur) ; `TestBearerStillWorks` (en-tête Bearer ouvre les routes de home, pas `/auth/*`) ; `TestChildHasNoGate` (processus avec `childToken` : pas de garde d'authentification) ; `TestWSCutOnRevoke` (révoquer la session coupe le `/term-ws` ouvert en moins de 6 s).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement**, ouvrir `authStore` dans `main()` après `takeToken`, avertissement au démarrage si `X-Forwarded-For` est vu sans `GLD_TRUSTED_PROXIES`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): mode multi-utilisateur dans le serveur`.

### Task 12: Routes `/admin/*`

**Files:**
- Create: `gld/auth_admin.go`, `gld/auth_admin_test.go`

**Interfaces:**
- Produces: `GET /admin/users`, `POST /admin/users` (créer), `POST /admin/users/disable`, `POST /admin/users/reset-mfa`, `POST /admin/acl` (accorder ou retirer des droits sur un home ; `exec` et `config_write` exigent `{"confirm":true}`), `GET /admin/events` (derniers `auth_events`, filtrables par `kind`), `POST /admin/sessions/revoke`. Réservées à `is_admin` avec second facteur validé ; un admin sans ligne `home_acl` n'a aucun accès aux homes.

- [ ] **Step 1: Tests** : `TestAdminOnly` (non-admin → 403) ; `TestAdminNoImplicitHomeAccess` ; `TestGrantExecNeedsConfirm` ; `TestACLChangeLogged` ; `TestDisableRevokesSessions`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement. Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): administration des comptes et des droits`.

### Task 13: Interface (connexion, second facteur, profil, administration)

**Files:**
- Create: `gld/ui/src/components/auth/Login.tsx`, `Mfa.tsx`, `Profile.tsx`, `AdminUsers.tsx`, `gld/ui/src/lib/auth.ts`, `gld/ui/src/components/auth/auth.test.tsx`
- Modify: `gld/ui/src/App.tsx`, `gld/ui/src/components/PageHeader.tsx`, `gld/ui/src/lib/api.ts` (en-tête `X-Gld-CSRF`), `gld/ui/src/i18n/{fr,en}/auth.json` (nouveau, référencé dans `index.ts`)

**Interfaces:**
- Consumes: routes `/auth/*` et `/admin/*`.
- Produces: `useAuth(): { state: "open"|"anon"|"mfa"|"ok"; user?: {...}; rights: Record<string, Record<Right, boolean>> }` ; l'interface masque les actions dont le droit manque ; l'écran d'ACL affiche l'avertissement « `exec` et `config_write` équivalent au compte du système » ; WebAuthn côté navigateur via `navigator.credentials` (sans bibliothèque), TOTP avec QR rendu depuis l'URI `otpauth://`.

- [ ] **Step 1: Tests** (vitest) : `Login` affiche une seule erreur générique quelle que soit la cause ; `Mfa` propose les méthodes enrôlées et les codes de secours ; `AdminUsers` demande confirmation avant d'accorder `exec` ; `keys.test.ts` existant passe (clés fr/en identiques).
- [ ] **Step 2: Run** `cd gld/ui && npx vitest run src/components/auth` → FAIL. **Step 3: Implement** avec les composants shadcn existants. **Step 4: Run** `npx tsc -b && npx eslint src && npx vitest run` → PASS. **Step 5: Commit** `feat(auth): interface de connexion, profil et administration`.

### Task 14: Garde-fou dans les sessions lancées par gld

**Files:**
- Create: `gld/auth_guard.go`, `gld/auth_guard_test.go`
- Modify: `gld/chat.go`, `gld/term.go`, `gld/homes.go` (injection au lancement), `gld/cli.go`

**Interfaces:**
- Produces: `func guardSettings(authDir string) map[string]any` (règles `permissions.deny` `Read|Edit|Write` du dossier, hook `PreToolUse` `Bash`, `sandbox.filesystem` refus lecture et écriture) ; `func guardHook(cmd string, authDir string) (deny bool)` (refuse `auth.db`, `auth.key`, le dossier, `sqlite3`) ; commande `gld auth managed-settings` (imprime le JSON à placer dans `/etc/claude-code/managed-settings.json`) ; `func managedInstalled() bool` et avertissement au démarrage si absent en mode multi-utilisateur ; sous-commande interne `_guard` appelée par le hook.

- [ ] **Step 1: Tests** : `TestGuardHookDenies` (`cat ~/.config/gld/auth.db`, `sqlite3 x`, `python -c "open('auth.key')"` → refus ; `ls` → autorisé) ; `TestGuardSettingsShape` ; `TestManagedWarningOnlyInAuthMode`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement. Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): garde-fou deny, hook et bac à sable des sessions`.

### Task 15: Documentation et version

**Files:**
- Modify: `README.md`, `CHANGELOG.md`
- Create: `docs/auth.md`

- [ ] **Step 1:** Rédiger `docs/auth.md` : activation (premier admin en CLI), droits et leur équivalence à l'OS, variables (`GLD_AUTH_DIR`, `GLD_AUTH_KEY`, `GLD_PUBLIC_URL`, `GLD_TRUSTED_PROXIES`), fichier `managed-settings`, perte de clé et `reset-mfa`, limites du modèle de menace.
- [ ] **Step 2:** Entrée `CHANGELOG.md` sous « Non publié ». **Step 3: Run** `make check` (une seule passe) → vert. **Step 4: Commit** `docs(auth): guide du multi-utilisateur`.

### Task 16 (lot séparé, activable) : séparation par compte OS

**Files:**
- Create: `gld/runas_unix.go`, `gld/runas_windows.go`, `gld/runas_test.go`
- Modify: `gld/homes.go` (`startHomeProc`), `gld/term_unix.go`, `gld/chat.go`

**Interfaces:**
- Produces: `func runAs() (uid, gid uint32, ok bool)` lu de `GLD_RUN_AS` ; `func applyRunAs(cmd *exec.Cmd)` (positionne `SysProcAttr.Credential` sur Unix ; sous Windows, no-op avec avertissement) ; au démarrage, vérification que le compte cible ne peut pas lire `authDir()` (sinon refus de démarrer).

- [ ] **Step 1: Tests** : `TestApplyRunAsSetsCredential` ; `TestRunAsRefusesReadableAuthDir` ; `TestRunAsUnsetIsNoop`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** ; documentation des prérequis (`CAP_SETUID` ou démarrage root puis abandon des privilèges) dans `docs/auth.md`. **Step 4: Run** → PASS. **Step 5: Commit** `feat(auth): exécution des sessions sous un autre compte`.
