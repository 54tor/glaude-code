# Dépendances — 2026-10-21 (2e passage)

## Go (`gld-web/go.mod`, `go 1.26.0`, toolchain 1.27.1)
Directes : `doublestar/v4` 4.10.3, `yaml.v3` 3.0.1, `modernc.org/sqlite`. `govulncheck ./...` : aucune vulnérabilité (avant et après).

| Module | Avant | Après | Décision |
|---|---|---|---|
| modernc.org/sqlite | 1.60.1 (marqué `// indirect` à tort → DEP-001) | **1.61.0**, désormais direct | D-006, appliqué |
| modernc.org/libc | 1.77.1 | 1.79.0 | tiré par sqlite |
| go-humanize, ncruces/go-strftime, x/sys | 1.0.1, 1.0.0, 0.48.0 | 1.1.0, 1.1.0, 0.49.0 | tirés par sqlite |
| autres lignes de `go list -m -u all` (x/tools, x/mod, x/sync, pprof, check.v1, modernc cc/ccgo/gc) | — | — | NO-CHANGE : dépendances d'outils, pas du binaire |

`go mod tidy` a aussi ajouté ~30 lignes à `go.sum` (graphe des tests des dépendances) : sans effet sur le binaire.

## npm (`gld-web/ui`)
| Paquet | Avant | Après | Décision |
|---|---|---|---|
| vite | 8.3.4 | **8.3.5** | D-006, appliqué (la contrainte `^8` ne change pas) |
| lucide-react | 1.60.0 | **1.61.0** | D-006, appliqué |
| typescript | 6.0.3 | — (7.0.2 existe) | rejeté : `typescript-eslint` 8.x exige `<6.1.0` |
| @types/node | 24.19.2 | — (26.6.5) | NO-CHANGE : majeure sans besoin |
| shadcn | 4.22.1 | — | en `devDependencies` (D-004) ; `npm audit` complet garde ses alertes d'outil de build |

`npm audit --omit=dev` : 0 vulnérabilité. Aucune note de version de sqlite/vite/lucide n'a pu être obtenue via Context7 : la validation repose sur les tests, pas sur le changelog.

## Commandes réellement exécutées
`go list -m -u all`, `go mod tidy -diff`, `go get modernc.org/sqlite@v1.61.0` puis `go mod tidy` (sur une copie isolée, `go.mod` et `go.sum` recopiés ensuite), `govulncheck ./...`, `staticcheck ./...` (linux, windows), `go vet` (linux, windows), `go test -race -count=1`, `npm outdated`, `npm audit --omit=dev`, `npm install vite@8.3.5 lucide-react@1.61.0`, `tsc --noEmit`, `eslint .`, `npm run build`.

## Résultats
Tous verts après la montée (Go sur copie isolée à `HEAD` plus mes fichiers, à cause des fichiers en cours d'une autre session ; front dans le dépôt). Reste à valider côté utilisateur : `make install`, puis un tour dans le navigateur (la recherche utilise SQLite).
