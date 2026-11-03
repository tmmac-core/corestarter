//go:build !windows

package main

// setProcessPower: only Windows throttles background processes this way.
func setProcessPower(powerMode) error { return nil }
