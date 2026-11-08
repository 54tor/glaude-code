package main

import (
	"path/filepath"
	"strings"
)

const (
	outputStylesDirName  = "output-styles"
	outputStyleMaxLength = 100
)

type OutputStyle struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	Project     string `json:"project,omitempty"`
}

var builtinOutputStyles = []string{"default", "Proactive", "Explanatory", "Learning"}

// scanOutputStyles lit les fichiers .md d'un dossier output-styles ; le nom vient du front matter, sinon du nom du fichier.
func scanOutputStyles(base, scope, project string) []OutputStyle {
	entries, readErr := readDirTrusted(base)
	if readErr != nil {
		return nil
	}
	var styles []OutputStyle
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".md") {
			continue
		}
		style := OutputStyle{Name: strings.TrimSuffix(fileName, ".md"), Scope: scope, Project: project}
		if content := readSmall(filepath.Join(base, fileName)); content != nil {
			if meta, _ := parseMeta(string(content)); meta != nil {
				if declaredName := textField(meta, "name"); declaredName != "" {
					style.Name = declaredName
				}
				style.Description = cut(textField(meta, "description"), skillTextSnippetMax)
			}
		}
		if style.Name == "" || len(style.Name) > outputStyleMaxLength || strings.ContainsAny(style.Name, "\n\r") {
			continue
		}
		styles = append(styles, style)
	}
	return styles
}

// listOutputStyles : les styles intégrés, puis ceux de l'utilisateur, puis ceux de chaque projet connu.
func listOutputStyles() []OutputStyle {
	styles := []OutputStyle{}
	for _, builtinName := range builtinOutputStyles {
		styles = append(styles, OutputStyle{Name: builtinName, Scope: "builtin"})
	}
	styles = append(styles, scanOutputStyles(filepath.Join(cfgDir(), outputStylesDirName), "user", "")...)
	for _, liveProject := range pickerProjects() {
		styles = append(styles, scanOutputStyles(filepath.Join(liveProject.Project, ".claude", outputStylesDirName), "project", liveProject.Project)...)
	}
	return styles
}
