package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// runBench measures the hash rate without a pool, once per variant, so a
// slow PC can be explained: what the secure JIT, large pages and the idle
// priority of the real miner cost or gain. Stop mining in the Starter first.
func runBench(seconds, threads int) int {
	if threads < 1 {
		threads = maxThreads()
	}
	variants := []struct {
		name                          string
		secure, largePages, idle, pin bool
	}{
		{"as in the Starter (idle priority)", false, false, true, false},
		{"normal priority", false, false, false, false},
		{"normal priority, pinned threads", false, false, false, true},
		{"with secure JIT", true, false, false, false},
		{"large pages", false, true, false, false},
		{"secure JIT + large pages", true, true, false, false},
	}
	fmt.Printf("core-miner bench: %d thread(s), %d s per variant\n", threads, seconds)
	for _, v := range variants {
		rxBench.secure, rxBench.largePages = v.secure, v.largePages
		setIdlePriority(v.idle)
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
