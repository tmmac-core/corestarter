//go:build !windows && !linux && !darwin

package main

import "runtime"

// Development builds on macOS only; the shipped miner is Windows and Linux.

func maxThreads() int { return max(1, runtime.NumCPU()) }

func availableMemory() uint64 { return 0 }

func onBattery() bool { return false }

func setIdlePriority(bool) {}

func pinThread(int) {}

// minerPriority: nothing here.
func minerPriority() {}
