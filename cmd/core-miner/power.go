package main

// powerMode says how far Windows may slow the miner down to save power
// (power throttling, "EcoQoS"). Windows 11 throttles a process without a
// window as soon as the window-owning program it descends from (the Starter)
// is not in the foreground ("Low" QoS, learn.microsoft.com quality-of-service).
// Seen on a 16-thread PC: the miner got 25 % of the CPU (4 threads' worth)
// while the PC idled, ~1.2 kH/s instead of ~8.3 kH/s, and full speed only
// while the tray menu was open. Possibly the E-cores of a hybrid CPU; that
// part is not documented for mains power and not confirmed.
// Only Windows acts on the mode; other systems ignore it.
type powerMode int

const (
	powerFull   powerMode = iota // opted out of throttling: the miner's mode
	powerSystem                  // Windows decides (CoreStarter 1.0.0)
	powerEco                     // always throttled, like Task Manager's efficiency mode
)

// threadPower is the mode every hashing thread applies to itself when it
// starts (workerThread). Only --bench changes it, and only while no thread runs.
var threadPower = powerFull

// powerNote is the result of minerPriority for the miner log ("" = nothing to say).
var powerNote string
