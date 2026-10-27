//go:build !linux

package main

func hideEnviron() error { return nil }
