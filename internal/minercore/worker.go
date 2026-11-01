package minercore

import (
	"crypto/rand"
	"strings"
)

// MaxWorkerLen: Catch That Rabbit is only known to accept a-z, 0-9 and '-' up
// to 15 characters (test worker "tmmac-mac-arm64", pool API 15.10.2026).
const MaxWorkerLen = 15

// SanitizeWorker lower-cases, turns spaces and underscores into '-', drops
// everything else and cuts to MaxWorkerLen.
func SanitizeWorker(in string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(in) {
		switch {
		case r == ' ' || r == '_':
			r = '-'
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			continue
		}
		if b.Len() == MaxWorkerLen {
			break
		}
		b.WriteRune(r)
	}
	s := b.String()
	if strings.Trim(s, "-") == "" {
		return ""
	}
	return s
}

// SuggestWorker returns a neutral name like "pc-7k2f". Not the computer name:
// the pool shows worker names publicly next to the address.
func SuggestWorker(prefix string) string {
	const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	buf := make([]byte, 4)
	rand.Read(buf)
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return prefix + "-" + string(buf)
}

// ThreadsFor maps a CPU share (25/50/75/100 %) to mining threads. max is the
// sensible maximum for this CPU (physical cores, bounded by L3 cache).
func ThreadsFor(percent, max int) int {
	if max < 1 {
		max = 1
	}
	n := (max*percent + 50) / 100
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	return n
}
