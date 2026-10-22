//go:build windows

package main

import "os"

// ownedByMe : sous Windows, git applique lui-même son contrôle de propriété (safe.directory).
func ownedByMe(os.FileInfo) bool { return true }
