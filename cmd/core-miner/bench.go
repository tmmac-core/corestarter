package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type benchVariant struct {
	name                          string
	secure, largePages, idle, pin bool
	power                         powerMode
}

// benchVariants: what the miner does in the Starter first, then one change at
// a time. On Windows two more rows show what power throttling costs: "Windows
// decides" is CoreStarter 1.0.0 (in a console in the foreground it usually
// matches the first row; the throttling hits when the miner runs hidden in the
// background), "efficiency mode" forces it, as Windows does in the background.
func benchVariants() []benchVariant {
	v := []benchVariant{
		{name: "as in the Starter (idle priority)", idle: true},
		{name: "normal priority"},
		{name: "normal priority, pinned threads", pin: true},
		{name: "with secure JIT", secure: true},
		{name: "large pages", largePages: true},
		{name: "secure JIT + large pages", secure: true, largePages: true},
	}
	if runtime.GOOS == "windows" {
		v = append(v,
			benchVariant{name: "idle priority, Windows decides (1.0.0)", idle: true, power: powerSystem},
			benchVariant{name: "idle priority, efficiency mode", idle: true, power: powerEco})
	}
	return v
}

// runBench measures the hash rate without a pool, once per variant, so a
// slow PC can be explained: what the secure JIT, large pages, the idle
// priority and Windows' power throttling cost or gain. Stop mining in the
// Starter first.
func runBench(seconds, threads int) int {
	if threads < 1 {
		threads = maxThreads()
	}
	fmt.Printf("core-miner bench: %d thread(s), %d s per variant\n", threads, seconds)
	for _, v := range benchVariants() {
		rxBench.secure, rxBench.largePages = v.secure, v.largePages
		setIdlePriority(v.idle)
		if err := setProcessPower(v.power); err != nil {
			fmt.Fprintf(os.Stderr, "%s: power throttling not set: %v\n", v.name, err)
		}
		threadPower = v.power
		ctx, err := newContext(true, threads, true)
		if err != nil {
			fmt.Printf("%-48s init failed: %v\n", v.name, err)
			continue
		}
		hs := benchRun(ctx, threads, time.Duration(seconds)*time.Second, v.pin)
		f := ctx.Flags()
		fmt.Printf("%-48s %8.0f H/s  (jit=%v secure=%v large pages=%v full memory=%v)\n",
			v.name, hs, f&flagJIT != 0, f&flagSecure != 0, f&flagLarge != 0, f&flagFullMem != 0)
		ctx.release()
	}
	setIdlePriority(false)
	setProcessPower(powerFull)
	threadPower = powerFull
	return 0
}

// benchRun hashes on every thread; the first 5 s (JIT warm-up, clock ramp)
// do not count. pin: thread i runs only on logical processor i (on CPUs with
// two different chiplets, e.g. Ryzen X3D, Windows may otherwise crowd one).
func benchRun(ctx *rxContext, threads int, d time.Duration, pin bool) float64 {
	var count atomic.Int64
	var counting, stop atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runtime.LockOSThread() // one OS thread per VM, as in the engine
			workerThread()
			if pin {
				pinThread(i)
			}
			vm, _ := ctx.newVM()
			if vm == nil {
				return
			}
			defer vm.destroy()
			var header, boundary [32]byte // zero boundary: never a share
			nonce := uint64(i) << 40
			for !stop.Load() {
				vm.search(&header, nonce, 16, &boundary)
				nonce += 16
				if counting.Load() {
					count.Add(16)
				}
			}
		}(i)
	}
	time.Sleep(5 * time.Second)
	counting.Store(true)
	t0 := time.Now()
	time.Sleep(d)
	counting.Store(false)
	el := time.Since(t0).Seconds()
	n := count.Load()
	stop.Store(true)
	wg.Wait()
	return float64(n) / el
}
