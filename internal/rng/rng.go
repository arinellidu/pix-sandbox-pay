// Package rng is the single source of randomness in the sandbox.
//
// The emulator is deterministic by default (ADR-007): every non-deterministic
// value — tokens, txids, e2eIds — comes from a seeded source whose seed is
// printed at boot, so any run can be reproduced exactly.
package rng

import (
	"encoding/hex"
	"math/rand/v2"
	"sync"
)

// DefaultSeed is used when no seed is supplied. Fixed on purpose: an
// unconfigured sandbox still produces the same sequence on every run.
const DefaultSeed uint64 = 0x50495853414E4442 // "PIXSANDB"

// Source is a concurrency-safe seeded random source.
type Source struct {
	mu   sync.Mutex
	seed uint64
	r    *rand.Rand
}

// New returns a Source seeded with seed.
func New(seed uint64) *Source {
	return &Source{
		seed: seed,
		r:    rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
	}
}

// Seed returns the seed this source was created with.
func (s *Source) Seed() uint64 { return s.seed }

// Bytes returns n random bytes.
func (s *Source) Bytes(n int) []byte {
	b := make([]byte, n)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range b {
		b[i] = byte(s.r.UintN(256))
	}
	return b
}

// Hex returns n random bytes hex-encoded (2n characters).
func (s *Source) Hex(n int) string {
	return hex.EncodeToString(s.Bytes(n))
}
