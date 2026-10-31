//go:build !windows

package main

import (
	"os"
	"syscall"
)

// ownedByMe : le fichier appartient à l'utilisateur qui fait tourner gld.
func ownedByMe(fileInfo os.FileInfo) bool {
	stat, isUnixStat := fileInfo.Sys().(*syscall.Stat_t)
	return !isUnixStat || int(stat.Uid) == os.Getuid()
}
