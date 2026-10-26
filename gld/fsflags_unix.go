//go:build !windows

package main

import "syscall"

// oNoFollow : refuse un lien symbolique à l'ouverture.
const oNoFollow = syscall.O_NOFOLLOW
