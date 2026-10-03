package storetest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// good is a correct reference store, used as the base of the broken ones.
type good struct {
	mu sync.Mutex
	m  map[string]int64
}

func newGood() *good { return &good{m: map[string]int64{}} }

func (g *good) key(k sequence.Key, p string) string {
	return fmt.Sprintf("%q|%q|%q", k.Name, k.Scope, p)
}

func (g *good) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if by <= 0 {
		return 0, fmt.Errorf("by must be > 0")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.m[g.key(k, p)] += by
	return g.m[g.key(k, p)], nil
}

// racy reads, yields, then writes: a non-atomic read-modify-write.
type racy struct{ *good }

func (r racy) Incr(_ context.Context, k sequence.Key, p string, by int64) (int64, error) {
	r.mu.Lock()
	cur := r.m[r.key(k, p)]
	r.mu.Unlock()
	r.mu.Lock()
	r.m[r.key(k, p)] = cur + by
	r.mu.Unlock()
	return cur + by, nil
}

// noScope ignores Scope.
type noScope struct{ *good }

func (s noScope) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	k.Scope = ""
	return s.good.Incr(ctx, k, p, by)
}

// noPeriod ignores the period.
type noPeriod struct{ *good }

func (s noPeriod) Incr(ctx context.Context, k sequence.Key, _ string, by int64) (int64, error) {
	return s.good.Incr(ctx, k, "", by)
}

// ignoresBy always adds 1.
type ignoresBy struct{ *good }

func (s ignoresBy) Incr(ctx context.Context, k sequence.Key, p string, _ int64) (int64, error) {
	return s.good.Incr(ctx, k, p, 1)
}

// advancesOnCancel bumps the counter before checking the context.
type advancesOnCancel struct{ *good }

func (s advancesOnCancel) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	n, err := s.good.Incr(context.Background(), k, p, by)
	if err != nil {
		return 0, err
	}
	return n, ctx.Err()
}

// allowsNonPositive accepts by <= 0.
type allowsNonPositive struct{ *good }

func (s allowsNonPositive) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	if by <= 0 {
		return 0, nil
	}
	return s.good.Incr(ctx, k, p, by)
}

func TestChecksPassOnGoodStore(t *testing.T) {
	Run(t, func(*testing.T) sequence.Store { return newGood() })
}

func TestChecksCatchBrokenStores(t *testing.T) {
	cases := []struct {
		name  string
		store sequence.Store
		check string
	}{
		{"non-atomic", racy{newGood()}, "Concurrency"},
		{"ignores scope", noScope{newGood()}, "Isolation"},
		{"ignores period", noPeriod{newGood()}, "Isolation"},
		{"ignores by", ignoresBy{newGood()}, "RangeSemantics"},
		{"advances on cancelled ctx", advancesOnCancel{newGood()}, "Context"},
		{"allows non-positive by", allowsNonPositive{newGood()}, "RejectsNonPositiveBy"},
	}
	c := newConfig([]Option{WithConcurrency(50, 200)})
	for _, tc := range cases {
		var run check
		for _, ck := range coreChecks {
			if ck.name == tc.check {
				run = ck.run
			}
		}
		if run == nil {
			t.Fatalf("%s: no check named %s", tc.name, tc.check)
		}
		if err := run(context.Background(), tc.store, c); err == nil {
			t.Errorf("%s: check %s did not catch the broken store", tc.name, tc.check)
		}
	}
}
