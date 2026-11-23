package main

// syncProjectDirs : chaque dossier d'un projet liste les autres dans les additionalDirectories de ses réglages locaux.
// Passe unique au démarrage ; les ajouts suivants sont écrits par l'interface. Un dossier sans session (donc sans
// dossier de transcripts) ou dont le fichier de réglages est illisible est laissé tel quel.
func syncProjectDirs() {
	for _, project := range loadStore().Projects {
		for _, owner := range project.Dirs {
			ownerKey := encodeDirName(owner)
			if cwd, known := knownProjectCwd(ownerKey); !known || cwd != owner {
				continue
			}
			for _, other := range project.Dirs {
				if other == owner || !isDirPath(other) {
					continue
				}
				// « existe déjà » est le cas normal : l'erreur est ignorée
				_, _, _ = configEdit(CfgEdit{File: "l/" + ownerKey, Op: "add", Path: []string{"permissions", "additionalDirectories"}, Value: other})
			}
		}
	}
}
