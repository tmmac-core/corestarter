//go:build !darwin

package main

// tooHot: only macOS reports thermal pressure to normal programs; Windows and
// Linux need drivers or root (docs/TECHNICAL.de.md "Grenzen").
func tooHot() bool { return false }
