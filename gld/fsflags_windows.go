//go:build windows

package main

// oNoFollow : Windows n'a pas de O_NOFOLLOW ; le contrôle reste celui de ownedByMe.
const oNoFollow = 0

// oNonBlock : pas de FIFO nommé ouvrable ainsi sous Windows.
const oNonBlock = 0
