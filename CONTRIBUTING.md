# Contribuer à Glaude Code

Merci de votre intérêt. Ouvrez d'abord une issue pour toute nouveauté importante : cela évite un travail refusé pour une question de périmètre.

## Préparer l'environnement

- Go (version de `gld/go.mod`), Node 22, `tmux`.
- `make check` lance les vérifications de l'interface et du code ; `make test` les tests Go ; `make cover` la couverture ; `make hooks` active le hook de pré-push qui lance `make check-ui`.
- La CI (`.github/workflows/ci.yml`) doit être verte : `go vet`, `gofmt`, `staticcheck`, `govulncheck`, `gosec`, `osv-scanner`, `gitleaks`, couverture Go et interface.

## Règles du projet

- **Textes visibles** : jamais de chaîne en dur. Côté Go, `L("srv.<fichier>.<clé>")` avec la clé dans `gld/locales/fr.json` ET `en.json` ; côté interface, le système i18n existant.
- **Écritures de données** : passer par `gld/rootfs.go` (`rWriteAtomic`, `mkdirTrusted`), jamais `os.WriteFile` ou `os.MkdirAll` directs, avec un cas dans `rootfs_test.go`.
- **Commentaires** : succincts, seulement pour un *pourquoi* non évident, et aucun dans ce qui part au client (HTML, bundles, réponses d'API).
- **Lisibilité** : noms explicites (y compris variables de boucle et paramètres de callbacks), fonctions courtes, une responsabilité par fichier.
- **Bibliothèques** : ne pas réimplémenter ce qu'une dépendance déjà présente fournit ; toute nouvelle dépendance est justifiée dans la PR.
- **Tests** : toute correction ou fonctionnalité vient avec un test qui échoue sans elle.
- **Documentation** : un changement de comportement met à jour `docs/` et l'`openapi.yaml` si une route change.
- **Changelog** : ajoutez une ligne dans la section non publiée de `CHANGELOG.md`, qui décrit l'effet pour l'utilisateur, pas l'implémentation.

## Confidentialité

Aucun chemin local, nom de compte, e-mail personnel, jeton ni clé dans le code, les tests, les fixtures, les captures ni l'historique. Utilisez des valeurs neutres (`/home/bob`, `alice`, `example.com`).

## Commits et PR

- Format `type(scope): description` (`feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `build`, `ci`), en anglais, une ligne.
- Un changement logique par commit, une PR par sujet, petite et ciblée.
- Signez vos commits avec `git commit -s` (certificat d'origine du développeur, DCO) : vous certifiez avoir le droit de les publier sous la licence MIT du projet.
- Les PR sont fusionnées en *squash*. Le titre de la PR devient le message du commit.
- Une PR qui touche à l'authentification, aux droits, à `rootfs.go` ou aux sessions invitées reçoit une relecture de sécurité et peut prendre plus de temps.

## Failles de sécurité

Ne les signalez jamais dans une issue publique : voir [SECURITY.md](SECURITY.md).
