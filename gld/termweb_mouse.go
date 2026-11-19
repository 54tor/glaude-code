package main

import "regexp"

// Rapport souris SGR (ESC [ < bouton ; x ; y M|m) : le bit 16 du bouton signale Ctrl, que Claude Code utilise pour ouvrir un lien ou un fichier sur l'hôte.
var ctrlMouseReport = regexp.MustCompile(`\x1b\[<(\d+);\d+;\d+[Mm]`)

const mouseCtrlBit = 16

func dropCtrlMouseReports(input []byte) []byte {
	return ctrlMouseReport.ReplaceAllFunc(input, func(report []byte) []byte {
		submatch := ctrlMouseReport.FindSubmatch(report)
		buttonCode := 0
		for _, digit := range submatch[1] {
			buttonCode = buttonCode*10 + int(digit-'0')
			if buttonCode > 1<<20 {
				return report
			}
		}
		if buttonCode&mouseCtrlBit != 0 {
			return nil
		}
		return report
	})
}
