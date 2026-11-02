package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func subagentTokens(transcriptPath string) Tokens {
	var total Tokens
	sessionDir := strings.TrimSuffix(transcriptPath, ".jsonl")
	subagentsDir := filepath.Join(sessionDir, "subagents")
	// #nosec G703 -- transcript trouvé par gld sous la racine des projets Claude
	if dirInfo, err := os.Stat(subagentsDir); err != nil || !dirInfo.IsDir() {
		return total
	}
	// #nosec G703 -- idem
	filepath.WalkDir(subagentsDir, func(walkedPath string, walkedEntry fs.DirEntry, err error) error {
		if err != nil || walkedEntry.IsDir() || !strings.HasSuffix(walkedPath, ".jsonl") {
			return nil
		}
		subagentInfo, statErr := walkedEntry.Info()
		if statErr != nil || !subagentInfo.Mode().IsRegular() || subagentInfo.Size() == 0 {
			return nil
		}
		subagentTok := info(walkedPath, subagentInfo).Tok
		total = Tokens{total.In + subagentTok.In, total.Out + subagentTok.Out, total.CR + subagentTok.CR, total.CW + subagentTok.CW}
		return nil
	})
	return total
}
