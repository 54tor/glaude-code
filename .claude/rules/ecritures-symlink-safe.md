Toute écriture de données gld (config, mémoire, notes, onglets, corbeille…) passe par `gld/rootfs.go` (`rWriteAtomic`, `mkdirTrusted`, os.Root) : jamais `os.WriteFile` / `os.MkdirAll` / `os.Rename` directs sur un chemin que le compte cible (`GLD_RUN_AS`) peut influencer.

**Why:** sous `GLD_RUN_AS`, la passerelle écrit en root dans des dossiers modifiables par la cible ; un lien symbolique posé à n'importe quel niveau du chemin ferait sortir l'écriture du dossier de confiance.

**How to apply:** nouvelle écriture = nouvelle fonction de `rootfs.go` ou appel d'une existante, plus un cas dans `rootfs_test.go`. Un fichier existant mais illisible n'est jamais écrasé (cf. `memory.go`, `skills.go`).
