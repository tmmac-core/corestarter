//go:build !darwin

package main

// workerThread: nothing per thread here; the priority is set process-wide.
func workerThread() {}

// watchParent: Windows (job object) and Linux (Pdeathsig) end the miner with
// the Starter; stdin EOF is the second guard everywhere.
func watchParent() {}
