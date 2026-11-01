package main

// RandomY (core-coin/RandomY, BSD-3) through cgo. Hash path as in coreminer
// (libethash-cpu/CPUMiner.cpp) and the Android miner:
//
//	seed = SHA3-512(header[32] || nonce uint64 little-endian)
//	hash = RandomY(seed) with the fixed cache key {53,54,55,56,57}
//	share if hash as a 256-bit big-endian number <= boundary
//
// One context holds cache/dataset (once per process, ~256 MiB in full mode);
// every hashing thread gets its own VM.

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/../../third_party/RandomY/src
#cgo LDFLAGS: -lrandomx
#cgo windows LDFLAGS: -lstdc++ -static -lpthread
#cgo darwin LDFLAGS: -lstdc++
#cgo linux LDFLAGS: -Wl,-Bstatic -lstdc++ -Wl,-Bdynamic -static-libgcc -lm -lpthread
#include <stdint.h>
#include <string.h>
#include "randomx.h"
#include "sha3.h"

static const unsigned char coreKey[] = {53, 54, 55, 56, 57};

static void initCache(randomx_cache* c) { randomx_init_cache(c, coreKey, sizeof coreKey); }

static void coreHash(randomx_vm* vm, const uint8_t* header, uint64_t nonce, uint8_t* out) {
	uint8_t input[40], seed[64];
	memcpy(input, header, 32);
	for (int i = 0; i < 8; i++) input[32 + i] = (uint8_t)(nonce >> (8 * i));
	sha3_512(input, sizeof input, seed);
	randomx_calculate_hash(vm, seed, sizeof seed, out);
}

// Hashes count nonces from start. Returns the index of the first share (hash in
// out) or -1. The loop stays in C: one cgo call per batch, not per hash.
static int search(randomx_vm* vm, const uint8_t* header, uint64_t start, int count, const uint8_t* boundary, uint8_t* out) {
	uint8_t h[32];
	for (int i = 0; i < count; i++) {
		coreHash(vm, header, start + (uint64_t)i, h);
		if (memcmp(h, boundary, 32) <= 0) {
			memcpy(out, h, 32);
			return i;
		}
	}
	return -1;
}
*/
import "C"

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"
)

const (
	flagJIT     = int(C.RANDOMX_FLAG_JIT)
	flagFullMem = int(C.RANDOMX_FLAG_FULL_MEM)
	flagHardAES = int(C.RANDOMX_FLAG_HARD_AES)
	flagLarge   = int(C.RANDOMX_FLAG_LARGE_PAGES)
	flagSecure  = int(C.RANDOMX_FLAG_SECURE)
)

// rxBench: settings only the --bench mode changes, to measure what they cost
// on a real PC. Mining always runs with the defaults (plain JIT, no large pages).
var rxBench struct{ secure, largePages bool }

type rxContext struct {
	flags   C.randomx_flags
	cache   *C.randomx_cache
	dataset *C.randomx_dataset
}

// newContext builds cache (and dataset in full mode). The JIT runs without
// RANDOMX_FLAG_SECURE, i.e. with pages that are writable and executable at once,
// as xmrig and coreminer do. The secure mode switches the page protection 16
// times per hash; on Windows every switch interrupts all cores running the
// miner: on a Ryzen 9 7950X3D with 32 threads 14.5 instead of 25.9 kH/s
// (--bench, 19.10.2026). Whether antivirus programs mind the
// plain JIT is not known yet (Norton test pending).
// Without enough memory for the dataset it falls back to light mode.
// appleJIT: macOS on Apple silicon never maps JIT pages writable and
// executable at once (MAP_JIT, W^X per thread); without the secure mode the
// first JIT program dies with SIGBUS. RandomY forces it there too
// (RANDOMX_FORCE_SECURE); the switch is a per-thread register, not mprotect.
const appleJIT = runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"

func newContext(fullMem bool, initThreads int, jit bool) (*rxContext, error) {
	ctx := &rxContext{flags: C.randomx_get_flags()}
	if jit {
		ctx.flags |= C.RANDOMX_FLAG_JIT
		ctx.flags &^= C.RANDOMX_FLAG_SECURE
		if rxBench.secure || appleJIT {
			ctx.flags |= C.RANDOMX_FLAG_SECURE
		}
	} else {
		ctx.flags &^= C.RANDOMX_FLAG_JIT
	}
	if rxBench.largePages {
		// Needs the "Lock pages in memory" right; without it, normal pages.
		if c := C.randomx_alloc_cache(ctx.flags | C.RANDOMX_FLAG_LARGE_PAGES); c != nil {
			ctx.flags |= C.RANDOMX_FLAG_LARGE_PAGES
			ctx.cache = c
		}
	}
	if ctx.cache == nil {
		ctx.cache = C.randomx_alloc_cache(ctx.flags)
	}
	if ctx.cache == nil && jit {
		// JIT memory refused (exploit protection policy): interpreter, same hashes, slower
		ctx.flags &^= C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_SECURE
		ctx.cache = C.randomx_alloc_cache(ctx.flags)
	}
	if ctx.cache == nil {
		return nil, errors.New("RandomY cache could not be allocated (memory?)")
	}
	C.initCache(ctx.cache)
	if !fullMem {
		return ctx, nil
	}
	ds := C.randomx_alloc_dataset(ctx.flags | C.RANDOMX_FLAG_FULL_MEM)
	if ds == nil && ctx.flags&C.RANDOMX_FLAG_LARGE_PAGES != 0 {
		ds = C.randomx_alloc_dataset((ctx.flags &^ C.RANDOMX_FLAG_LARGE_PAGES) | C.RANDOMX_FLAG_FULL_MEM)
	}
	if ds == nil {
		return ctx, nil // light mode
	}
	items := uint32(C.randomx_dataset_item_count())
	if initThreads < 1 {
		initThreads = 1
	}
	per := items / uint32(initThreads)
	var wg sync.WaitGroup
	start := uint32(0)
	for i := 0; i < initThreads; i++ {
		count := per
		if i == initThreads-1 {
			count += items % uint32(initThreads)
		}
		wg.Add(1)
		go func(start, count uint32) {
			defer wg.Done()
			C.randomx_init_dataset(ds, ctx.cache, C.ulong(start), C.ulong(count))
		}(start, count)
		start += count
	}
	wg.Wait()
	ctx.dataset = ds
	ctx.flags |= C.RANDOMX_FLAG_FULL_MEM
	// With a dataset the VMs no longer need the cache.
	C.randomx_release_cache(ctx.cache)
	ctx.cache = nil
	return ctx, nil
}

func (c *rxContext) Flags() int { return int(c.flags) }

func (c *rxContext) release() {
	if c.dataset != nil {
		C.randomx_release_dataset(c.dataset)
		c.dataset = nil
	}
	if c.cache != nil {
		C.randomx_release_cache(c.cache)
		c.cache = nil
	}
}

type rxVM struct{ vm *C.randomx_vm }

// newVM falls back to the interpreter if JIT memory cannot be made executable.
func (c *rxContext) newVM() (*rxVM, bool) {
	vm := C.randomx_create_vm(c.flags, c.cache, c.dataset)
	if vm == nil && c.flags&C.RANDOMX_FLAG_LARGE_PAGES != 0 {
		vm = C.randomx_create_vm(c.flags&^C.RANDOMX_FLAG_LARGE_PAGES, c.cache, c.dataset)
	}
	jit := c.flags&C.RANDOMX_FLAG_JIT != 0
	if vm == nil && jit {
		vm = C.randomx_create_vm(c.flags&^(C.RANDOMX_FLAG_JIT|C.RANDOMX_FLAG_SECURE), c.cache, c.dataset)
		jit = false
	}
	if vm == nil {
		return nil, false
	}
	return &rxVM{vm}, jit
}

// newInterpreterVM is a VM without JIT, for a thread whose JIT VM failed the self-test.
func (c *rxContext) newInterpreterVM() *rxVM {
	vm := C.randomx_create_vm(c.flags&^(C.RANDOMX_FLAG_JIT|C.RANDOMX_FLAG_SECURE), c.cache, c.dataset)
	if vm == nil {
		return nil
	}
	return &rxVM{vm}
}

func (v *rxVM) destroy() {
	if v.vm != nil {
		C.randomx_destroy_vm(v.vm)
		v.vm = nil
	}
}

func (v *rxVM) hash(header [32]byte, nonce uint64) [32]byte {
	var out [32]byte
	C.coreHash(v.vm, (*C.uint8_t)(unsafe.Pointer(&header[0])), C.uint64_t(nonce), (*C.uint8_t)(unsafe.Pointer(&out[0])))
	return out
}

// search hashes count nonces from start; found = index of the first share.
func (v *rxVM) search(header *[32]byte, start uint64, count int, boundary *[32]byte) (found int, hash [32]byte) {
	r := C.search(v.vm, (*C.uint8_t)(unsafe.Pointer(&header[0])), C.uint64_t(start), C.int(count),
		(*C.uint8_t)(unsafe.Pointer(&boundary[0])), (*C.uint8_t)(unsafe.Pointer(&hash[0])))
	return int(r), hash
}
