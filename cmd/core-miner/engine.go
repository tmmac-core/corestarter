package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmmac-core/corestarter/internal/minercore"
)

type vector struct {
	header string
	nonce  uint64
	want   string
}

// selfTestVectors: the whole hash path (SHA3-512 -> RandomY) against known
// answers before mining. The first three were computed with interpreter and JIT
// (identical); the last two are real shares accepted by Catch That Rabbit, so the
// pool itself confirmed the path (HashSelfTest.kt in core-mobile-miner). A wrong
// JIT would otherwise only show up as rejected shares after half an hour.
var selfTestVectors = []vector{
	{strings.Repeat("00", 32), 0, "1193e25e06f67879d119c8dd17da03659d699011d850c76689caafb823c86fed"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", 0x0e8aa21adaea1f31, "926512322587a4497ace8fcfe89a0373ab618c814e2f59b74b376af5508507b6"},
	{strings.Repeat("ff", 32), ^uint64(0), "e5cd291c9b6ab686e5f943ca987b45940e31ba95c9db32e2a8984d064524dcfd"},
	{"00a4a2a4594f3d9ac20ff1bd73d8659d6b5ecbe2769a2689c7893223d41d1dd5", 0xb2d501cac4596576, "0000048afa6573559e4b75f4fa47e9ff7c4bf30d3460a68e3705984abd0d0d2a"},
	{"b2953b92102404326f9558585b75c7a6c55244d059beb8d534ace0d136a1cea6", 0x8fdae1b19b109dcf, "00001149e24ffedf602cef1ce8f30cde5f5b0aff05347cdc9005a045e09c18ab"},
}

// selfTest returns "" if every vector matches, otherwise the first difference.
func selfTest(vm *rxVM) string {
	for _, v := range selfTestVectors {
		var h [32]byte
		b, _ := hex.DecodeString(v.header)
		copy(h[:], b)
		got := vm.hash(h, v.nonce)
		if g := hex.EncodeToString(got[:]); g != v.want {
			return fmt.Sprintf("self-test: header %s… nonce %d -> %s, want %s", v.header[:8], v.nonce, g, v.want)
		}
	}
	return ""
}

// errSelfTest: not even the interpreter computes the right hashes; never mine then.
type errSelfTest struct{ msg string }

func (e errSelfTest) Error() string { return e.msg }

// initVerified builds the context and checks the hash path. If the JIT computes
// wrong hashes, it rebuilds without JIT; if the interpreter is wrong too, it fails.
func initVerified(fullMem bool, initThreads int, jit bool, logf func(string, ...any)) (*rxContext, error) {
	ctx, err := newContext(fullMem, initThreads, jit)
	if err != nil {
		return nil, err
	}
	vm, _ := ctx.newVM()
	if vm == nil {
		ctx.release()
		return nil, fmt.Errorf("RandomY VM could not be created (memory?)")
	}
	msg := selfTest(vm)
	vm.destroy()
	if msg == "" {
		return ctx, nil
	}
	ctx.release()
	if jit {
		logf("%s; retrying without JIT", msg)
		return initVerified(fullMem, initThreads, false, logf)
	}
	return nil, errSelfTest{"hash path wrong, not mining: " + msg}
}

// batch is small enough that a new job takes effect within a fraction of a second.
const batch = 16

// engine runs the hashing threads. Every thread has its own VM and nonce range
// (random start, so several PCs with the same address do not repeat work).
// Threads with an index >= active, or without a job, wait instead of hashing.
type engine struct {
	ctx        *rxContext
	threads    int
	onSolution func(job *minercore.Job, nonce uint64, hash [32]byte)
	logf       func(string, ...any)

	job       atomic.Pointer[minercore.Job]
	active    atomic.Int32
	paused    atomic.Bool
	running   atomic.Bool
	hashes    atomic.Int64
	perThread []atomic.Int64 // hashes per thread, for the window's thread grid
	live      atomic.Int32
	jitOff    atomic.Int32 // VMs that had to run without JIT
	wg        sync.WaitGroup
	setup     atomic.Int32           // threads past their self-test (passed or not)
	onDead    func(wrongHashes bool) // called once if no thread passed its self-test
	wrong     atomic.Int32           // threads whose VMs computed wrong hashes (not: no memory)
}

func (e *engine) start() {
	e.running.Store(true)
	if e.active.Load() == 0 {
		e.active.Store(int32(e.threads))
	}
	e.perThread = make([]atomic.Int64, e.threads)
	for i := 0; i < e.threads; i++ {
		e.wg.Add(1)
		go e.work(i)
	}
}

// stop waits up to 5 s for the threads; if one is still hashing (very slow PC),
// the dataset is not freed. The process ends right after anyway.
func (e *engine) stop() {
	e.running.Store(false)
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		e.ctx.release()
	case <-time.After(5 * time.Second):
		e.logf("a hashing thread is still running; keeping RandomY memory")
	}
}

func (e *engine) setActive(n int) {
	if n < 1 {
		n = 1
	}
	if n > e.threads {
		n = e.threads
	}
	e.active.Store(int32(n))
}

func (e *engine) work(index int) {
	defer e.wg.Done()
	// RandomY VMs are not thread-safe and the JIT code is per VM: one OS thread per VM.
	runtime.LockOSThread()
	workerThread()
	vm, jit := e.ctx.newVM()
	if vm == nil {
		e.logf("thread %d: no VM (memory?)", index)
		e.setupDone()
		return
	}
	// Check every VM: catches e.g. a stale instruction cache on the first JIT run,
	// which the init test only covers for its own VM. A JIT VM that computes
	// wrong is replaced by an interpreter VM; only if that is wrong too does
	// the thread give up.
	if msg := selfTest(vm); msg != "" && jit {
		e.logf("thread %d (jit): %s; trying without JIT", index, msg)
		vm.destroy()
		if vm = e.ctx.newInterpreterVM(); vm == nil {
			e.logf("thread %d: no VM (memory?)", index)
			e.wrong.Add(1) // the JIT VM did compute wrong hashes
			e.setupDone()
			return
		}
		jit = false
	}
	defer vm.destroy()
	if msg := selfTest(vm); msg != "" {
		e.logf("thread %d: %s", index, msg)
		e.wrong.Add(1)
		e.setupDone()
		return
	}
	if !jit && e.ctx.Flags()&flagJIT != 0 {
		e.jitOff.Add(1)
	}
	e.live.Add(1)
	defer e.live.Add(-1)
	e.setupDone()
	var current *minercore.Job
	var nonce uint64
	for e.running.Load() {
		j := e.job.Load()
		if j == nil || e.paused.Load() || int32(index) >= e.active.Load() {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if j != current {
			current = j
			nonce = randomNonce()
		}
		found, hash := vm.search(&j.Header, nonce, batch, &j.Boundary)
		if found >= 0 {
			n := int64(found + 1)
			e.hashes.Add(n)
			e.perThread[index].Add(n)
			j.Hashes.Add(n)
			e.onSolution(j, nonce+uint64(found), hash)
			nonce += uint64(found) + 1
		} else {
			e.hashes.Add(batch)
			e.perThread[index].Add(batch)
			j.Hashes.Add(batch)
			nonce += batch
		}
	}
}

// setupDone: once every thread is past its self-test and none hashes, the
// miner cannot work at all.
func (e *engine) setupDone() {
	if int(e.setup.Add(1)) == e.threads && e.live.Load() == 0 && e.running.Load() && e.onDead != nil {
		e.onDead(e.wrong.Load() > 0)
	}
}

func randomNonce() uint64 {
	var b [8]byte
	rand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:])
}
