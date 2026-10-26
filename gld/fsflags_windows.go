//go:build windows

package main

// oNoFollow : Windows n'a pas de O_NOFOLLOW ; le contrôle reste celui de ownedByMe.
const oNoFollow = 0
