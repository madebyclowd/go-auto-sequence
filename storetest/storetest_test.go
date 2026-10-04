package storetest

import (
	"context"
	"fmt"
	"strings"
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

func (g *good) Current(ctx context.Context, k sequence.Key, p string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.m[g.key(k, p)]
	return v, ok, nil
}

func (g *good) Set(ctx context.Context, k sequence.Key, p string, raw int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if raw < 0 {
		return fmt.Errorf("raw must be >= 0")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.m[g.key(k, p)] = raw
	return nil
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

// Broken Resetters: each must be caught by the named check.
type setIgnoresScope struct{ *good }

func (s setIgnoresScope) Set(ctx context.Context, k sequence.Key, p string, raw int64) error {
	k.Scope = ""
	return s.good.Set(ctx, k, p, raw)
}

type setIsSeparate struct{ *good } // Set writes somewhere Incr never looks

func (s setIsSeparate) Set(ctx context.Context, k sequence.Key, p string, raw int64) error {
	return s.good.Set(ctx, sequence.Key{Name: k.Name + "#shadow", Scope: k.Scope}, p, raw)
}

type currentAlwaysOK struct{ *good }

func (s currentAlwaysOK) Current(ctx context.Context, k sequence.Key, p string) (int64, bool, error) {
	v, _, err := s.good.Current(ctx, k, p)
	return v, true, err
}

type setAllowsNegative struct{ *good }

func (s setAllowsNegative) Set(_ context.Context, k sequence.Key, p string, raw int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[s.key(k, p)] = raw
	return nil
}

type setAdvancesOnCancel struct{ *good }

func (s setAdvancesOnCancel) Set(ctx context.Context, k sequence.Key, p string, raw int64) error {
	_ = s.good.Set(context.Background(), k, p, raw)
	return ctx.Err()
}

func TestResetterChecksPassOnGoodStore(t *testing.T) {
	Run(t, func(*testing.T) sequence.Store { return newGood() })
}

func TestResetterChecksCatchBrokenStores(t *testing.T) {
	cases := []struct {
		name  string
		store sequence.Store
		check string
	}{
		{"set ignores scope", setIgnoresScope{newGood()}, "SetIsolation"},
		{"set is separate from incr", setIsSeparate{newGood()}, "SetThenIncrContinuesFromValue"},
		{"current always ok", currentAlwaysOK{newGood()}, "CurrentOfUnknownPartition"},
		{"set allows negative", setAllowsNegative{newGood()}, "SetRejectsNegative"},
		{"set advances on cancelled ctx", setAdvancesOnCancel{newGood()}, "SetContext"},
	}
	for _, tc := range cases {
		var run func(context.Context, sequence.Resetter, sequence.Store) error
		for _, ck := range resetterChecks {
			if ck.name == tc.check {
				run = ck.run
			}
		}
		if run == nil {
			t.Fatalf("%s: no check named %s", tc.name, tc.check)
		}
		if err := run(context.Background(), tc.store.(sequence.Resetter), tc.store); err == nil {
			t.Errorf("%s: check %s did not catch the broken store", tc.name, tc.check)
		}
	}
}

func TestStoreWithoutResetterSkipsResetterChecks(t *testing.T) {
	Run(t, func(*testing.T) sequence.Store { return incrOnly{newGood()} })
}

// incrOnly hides the Resetter methods, like a third-party store that only implements Incr.
type incrOnly struct{ g *good }

func (s incrOnly) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	return s.g.Incr(ctx, k, p, by)
}

// caseInsensitive folds case and trims trailing spaces, like a default MySQL collation.
type caseInsensitive struct{ *good }

func fold(k sequence.Key, p string) (sequence.Key, string) {
	k.Name, k.Scope = strings.ToLower(strings.TrimRight(k.Name, " ")), strings.ToLower(strings.TrimRight(k.Scope, " "))
	return k, strings.ToLower(strings.TrimRight(p, " "))
}

func (s caseInsensitive) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	k, p = fold(k, p)
	return s.good.Incr(ctx, k, p, by)
}

func TestCaseCheckCatchesCollationBugs(t *testing.T) {
	var run check
	for _, ck := range coreChecks {
		if ck.name == "CaseAndWhitespaceSensitive" {
			run = ck.run
		}
	}
	if run == nil {
		t.Fatal("check missing")
	}
	if err := run(context.Background(), newGood(), newConfig(nil)); err != nil {
		t.Fatalf("a correct store must pass: %v", err)
	}
	if err := run(context.Background(), caseInsensitive{newGood()}, newConfig(nil)); err == nil {
		t.Fatal("a case-insensitive store must fail the check")
	}
}
