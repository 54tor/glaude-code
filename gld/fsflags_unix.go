//go:build !windows

package main

import "syscall"

// oNoFollow : refuse un lien symbolique à l'ouverture.
const oNoFollow = syscall.O_NOFOLLOW

// oNonBlock : l'ouverture d'un FIFO ne bloque pas ; sans effet sur la lecture d'un fichier ordinaire.
const oNonBlock = syscall.O_NONBLOCK
