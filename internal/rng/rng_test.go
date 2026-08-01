package rng_test

import (
	"testing"

	"github.com/arinelliquebec/pix-sandbox/internal/rng"
)

// ADR-007: same seed, same sequence — that is what makes a sandbox run
// reproducible from the seed printed at boot.
func TestSameSeedSameSequence(t *testing.T) {
	a, b := rng.New(42), rng.New(42)

	for i := range 5 {
		if got, want := a.Hex(16), b.Hex(16); got != want {
			t.Fatalf("draw %d diverged: %s != %s", i, got, want)
		}
	}
}

func TestDifferentSeedsDiverge(t *testing.T) {
	if rng.New(1).Hex(16) == rng.New(2).Hex(16) {
		t.Error("different seeds produced the same first draw")
	}
}

func TestHexLengthAndSeedAccessor(t *testing.T) {
	s := rng.New(rng.DefaultSeed)

	if s.Seed() != rng.DefaultSeed {
		t.Errorf("Seed() = %d, want %d", s.Seed(), rng.DefaultSeed)
	}
	if got := s.Hex(24); len(got) != 48 {
		t.Errorf("len(Hex(24)) = %d, want 48", len(got))
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	s := rng.New(7)
	done := make(chan struct{})

	for range 8 {
		go func() {
			for range 100 {
				s.Hex(8)
			}
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
}
