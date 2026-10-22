//go:build !windows

package main

import (
	"os"
	"syscall"
)

// ownedByMe : le fichier appartient à l'utilisateur qui fait tourner gld.
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}
