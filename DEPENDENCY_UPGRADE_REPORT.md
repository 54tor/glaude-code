# Rapport de mise à niveau des dépendances

Date : 2026-10-23. Périmètre : module Go `gld` (`gld/go.mod`) et interface `gld/ui` (`package.json`, `package-lock.json`).
Versions résolues lues avec `go list -m -u -json all` et `npm ls --depth=0` ; avis de sécurité vérifiés avec
`govulncheck` et `npm audit --omit=dev`. Documentation vérifiée via Context7 (MCP) pour TypeScript 7 et typescript-eslint.

## Environnement

| Outil | Déclaré | Résolu localement | CI |
|---|---|---|---|
| Go | `go 1.26.0` (`gld/go.mod`) | go1.27.1 | `go-version-file: gld/go.mod` → 1.26.x |
| Node | — | v26.11.1 | `node-version: 22` |
| TypeScript | `~6.0.2` | 6.0.3 | idem |

## Inventaire Go (module `gld`)

| Module | Résolu | Dernière | Statut | Décision |
|---|---|---|---|---|
| github.com/bmatcuk/doublestar/v4 | v4.10.3 | v4.10.3 | direct, à jour | NO-CHANGE |
| github.com/coder/websocket | v1.8.15 | v1.8.15 | direct, à jour | NO-CHANGE |
| github.com/creack/pty | v1.1.24 | v1.1.24 | direct, à jour | NO-CHANGE |
| github.com/nicksnyder/go-i18n/v2 | v2.6.1 | v2.6.1 | **importé directement** (`gld/i18n.go:16`) mais marqué `// indirect` | AUTO-FIX (DEP-001) |
| golang.org/x/text | v0.43.0 | v0.43.0 | **importé directement** (`gld/i18n.go:17`) mais marqué `// indirect` | AUTO-FIX (DEP-001) |
| gopkg.in/yaml.v3 | v3.0.1 | v3.0.1 (dépôt archivé) | direct, un seul usage (`gld/memmeta.go:51`) | TUI-DECISION (DEP-002) |
| modernc.org/sqlite | v1.61.0 | v1.61.0 | direct, à jour | NO-CHANGE |
| indirects (libc, mathutil, memory, uuid, strftime, sys, bigfft…) | — | ccgo v4.38.0, yaml.in v3.0.5, check.v1 (outils seulement) | hors graphe de build | NO-CHANGE |

### DEP-001 — `go.mod` non « tidy »

- Preuve : `go mod tidy -diff` déplace `go-i18n/v2` et `x/text` du bloc indirect au bloc direct et ajoute 4 lignes à `go.sum`
  (`BurntSushi/toml`, `go.yaml.in/yaml/v3`, requis par le graphe de modules).
- Risque : aucun (aucune version ne change). Correction : `go mod tidy`. Validation : `go build ./...` et `go test -count=1 .`.
- Décision : AUTO-FIX.

### DEP-002 — `gopkg.in/yaml.v3` archivé

- `gopkg.in/yaml.v3` n'est plus maintenu ; le fork maintenu `go.yaml.in/yaml/v3` (même API, v3.0.5) est déjà dans le graphe
  comme dépendance indirecte. Usage unique : `yaml.Unmarshal` du front matter dans `memmeta.go`.
- Aucune vulnérabilité connue sur v3.0.1 (govulncheck : « No vulnerabilities found »).
- Options : (A) migrer l'import vers `go.yaml.in/yaml/v3` (changement d'une ligne d'import, même API) ; (B) garder tel quel.
- Décision : TUI-DECISION (changement de fournisseur de dépendance, pas de bénéfice fonctionnel immédiat).

## Inventaire npm (`gld/ui`)

`npm outdated` ne signale que deux paquets ; `npm audit --omit=dev` : 0 vulnérabilité.

| Paquet | Résolu | Dernière | Analyse | Décision |
|---|---|---|---|---|
| typescript | 6.0.3 | 7.0.2 | TS 7 (compilateur natif) supprime `baseUrl`, `outFile`, `target ES5`, `moduleResolution node10/classic`, `esModuleInterop:false` (Context7, `/microsoft/typescript`). Les `tsconfig` du projet n'utilisent aucune option supprimée (`moduleResolution: bundler`, `target: es2023`). **Mais** `typescript-eslint@8.72.0` déclare `peerDependencies.typescript: ">=4.8.4 <6.1.0"` et `shadcn@4.22.1` dépend de `typescript ^5.9.2` : la montée casserait le lint. | NO-CHANGE (réévaluer quand typescript-eslint acceptera TS 7) |
| @types/node | 24.19.2 | 26.6.5 | Utilisé seulement par `vite.config.ts` (`tsconfig.node.json`). La CI tourne sous Node 22 : des types 26 décriraient des API absentes du runtime CI. | NO-CHANGE |
| react / react-dom | 19.3.1 | 19.3.1 | à jour | NO-CHANGE |
| vite | 8.3.5 | 8.3.5 | à jour (rolldown) | NO-CHANGE |
| tailwindcss / @tailwindcss/vite | 4.3.3 | 4.3.3 | à jour | NO-CHANGE |
| i18next / react-i18next | 26.5.6 / 17.1.3 | idem | à jour (chantier i18n en cours) | NO-CHANGE |
| next-themes | 0.4.6 | 0.4.6 | utilisé uniquement par `src/components/ui/sonner.tsx` (thème des toasts) alors que le thème est géré par `theme-provider.tsx` maison ; dépendance redondante mais vivante | NO-CHANGE (voir UI-xxx si simplification souhaitée) |
| cn | 0.5.0 | 0.5.0 | paquet officiel shadcn-ui (`github.com/shadcn-ui/cn`), remplace clsx + tailwind-merge | NO-CHANGE |
| @fontsource-variable/* (6 familles) | 5.3.0 | 5.3.0 | toutes importées dans `index.css` au démarrage (choix de police dans Réglages) | voir audit UI |

## Avis de sécurité vérifiés

| Commande | Résultat |
|---|---|
| `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` | No vulnerabilities found |
| `npm audit --omit=dev` | found 0 vulnerabilities |
| `~/go/bin/staticcheck ./...` | 0 signalement |
| `~/go/bin/gosec ./...` | 184 signalements (83 G104 erreurs ignorées, 34 G304, 28 G703, 19 G204, 7 G301, 3 G702, 3 G124, 2 G115, 2 G101, 2 G122, 1 G120) : triés dans `AUDIT_REPORT.md` ; les G101 (`tokens.go:21`, `homes.go:30`) sont des faux positifs (constantes de libellé, pas de secret) |

## Mises à niveau appliquées

Voir `AUDIT_CHANGES.md` (DEP-001 si appliqué).

## Décisions à soumettre

- DEP-002 : migration `gopkg.in/yaml.v3` → `go.yaml.in/yaml/v3`.
