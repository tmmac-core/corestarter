//go:build !windows && !linux && !darwin

package main

// systemTexts: the app runs on Windows and Linux; other systems (tests) get English.
func systemTexts() texts { return textsEN }
