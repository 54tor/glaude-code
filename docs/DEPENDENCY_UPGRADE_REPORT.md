# Dépendances — 2026-10-21

## Go
`go.mod` : `go 1.25`, **aucune dépendance**. Toolchain locale 1.27.1. `govulncheck ./...` (toolchain 1.27.1) : aucune vulnérabilité. Les API utilisées exigent Go ≥ 1.25 (`http.CrossOriginProtection`) et ≥ 1.24 (`os.OpenRoot`) : cohérent avec `go.mod`.

## npm (`gld-web/ui`, versions résolues par `package-lock.json`)

| Paquet | Résolue | Dernière | Décision | Raison |
|---|---|---|---|---|
| vite | 8.3.4 | 8.3.5 | NO-CHANGE (différé) | correctif ; aucun besoin démontré, notes de version non obtenues ; changerait `dist/` |
| lucide-react | 1.60.0 | 1.61.0 | NO-CHANGE (différé) | idem |
| typescript | 6.0.3 | 7.0.2 | **rejeté pour l'instant** | `typescript-eslint` 8.72 déclare `typescript >=4.8.4 <6.1.0` et lève une erreur sous TS ≥ 7 (Context7, source du parseur) ; `tsc -b` est dans le build |
| @types/node | 24.19.2 | 26.6.5 | NO-CHANGE | majeure sans besoin ; sert seulement à la config Vite |
| shadcn | 4.22.1 | — | **D-004** | porteur des 7 alertes |

### `npm audit` : 7 « high », une seule cause
`shadcn` (direct) → `@shadcn/registry`, `ts-morph` → `@ts-morph/common` → `fast-glob` → `micromatch` → `braces`
(GHSA-vfj7-8cjw-p6xm : déni de service par épuisement de pile sur des motifs glob très imbriqués).

- **Applicabilité** : `shadcn` n'est utilisé que pour `@import "shadcn/tailwind.css"` (`src/index.css:3`), à la compilation. Le bundle servi (`dist/`) n'embarque aucun de ces paquets et le serveur Go ne les charge pas. Seul le poste de build est exposé, via un motif glob qu'un attaquant devrait contrôler : pas de vecteur réaliste.
- **`npm audit fix --force` rejeté** : il propose `shadcn@1.0.0`, soit une rétrogradation de 4.22.1 vers 1.0.0 (majeure) qui casserait `tailwind.css`.
- **Décision D-004 (A) appliquée** : `shadcn` déplacé en `devDependencies` ; `npm audit --omit=dev` → 0 vulnérabilité ; `npm audit` complet garde 7 alertes (outil de build, non exploitables ici).

## Commandes exécutées
`npm audit`, `npm audit --json`, `npm outdated`, `npm ls shadcn typescript-eslint`, `npm view typescript-eslint peerDependencies`, `npx tsc --noEmit`, `npx eslint .`, `npx vite build` (vers un dossier jetable, puis `npm run build`). **Aucune montée de version** ; seul `shadcn` change de section (D-004) dans `package.json` et `package-lock.json` (drapeaux `dev` recalculés par script, validé sur le lockfile d'origine : 0 écart ; npm ne pouvait pas régénérer hors réseau).
