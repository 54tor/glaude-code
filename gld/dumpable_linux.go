package main

import "golang.org/x/sys/unix"

// hideEnviron : /proc/<pid>/environ garde le bloc initial malgré os.Unsetenv. Un processus non « dumpable » ne s'y laisse
// plus lire par les autres processus du même uid, qui sans cela y trouveraient le jeton d'un gld enfant.
func hideEnviron() error { return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }
