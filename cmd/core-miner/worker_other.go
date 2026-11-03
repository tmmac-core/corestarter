//go:build !darwin && !windows

package main

// workerThread: nothing per thread here; the priority is set process-wide.
func workerThread() {}

// watchParent: Linux ends the miner with the Starter (Pdeathsig); stdin EOF
// is the second guard.
func watchParent() {}
