package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// names hands out random key names so a shared database needs no cleanup between checks.
type names struct{}

func (names) next() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return "st-" + hex.EncodeToString(b)
}

// check is one conformance check. It returns nil when the store behaves.
type check func(ctx context.Context, s sequence.Store, c config) error

type namedCheck struct {
	name string
	run  check
}

var coreChecks = []namedCheck{
	{"FirstCallReturnsBy", checkFirstCall},
	{"StrictlyConsecutive", checkConsecutive},
	{"RangeSemantics", checkRange},
	{"RejectsNonPositiveBy", checkRejectsNonPositive},
	{"Isolation", checkIsolation},
	{"EmptyScopeAndPeriod", checkEmptyScopeAndPeriod},
	{"CaseAndWhitespaceSensitive", checkCaseAndWhitespace},
	{"Concurrency", checkConcurrency},
	{"ConcurrentRanges", checkConcurrentRanges},
	{"Context", checkContext},
}

func expect(what string, got, want int64) error {
	if got != want {
		return fmt.Errorf("%s: got %d, want %d", what, got, want)
	}
	return nil
}

func checkFirstCall(ctx context.Context, s sequence.Store, _ config) error {
	k1 := sequence.Key{Name: names{}.next()}
	got, err := s.Incr(ctx, k1, "p", 1)
	if err != nil {
		return err
	}
	if err := expect("first Incr(by=1)", got, 1); err != nil {
		return err
	}
	k2 := sequence.Key{Name: names{}.next()}
	got, err = s.Incr(ctx, k2, "p", 5)
	if err != nil {
		return err
	}
	return expect("first Incr(by=5)", got, 5)
}

func checkConsecutive(ctx context.Context, s sequence.Store, _ config) error {
	k := sequence.Key{Name: names{}.next()}
	for i := int64(1); i <= 100; i++ {
		got, err := s.Incr(ctx, k, "", 1)
		if err != nil {
			return err
		}
		if err := expect(fmt.Sprintf("call %d", i), got, i); err != nil {
			return err
		}
	}
	return nil
}

func checkRange(ctx context.Context, s sequence.Store, _ config) error {
	k := sequence.Key{Name: names{}.next()}
	for _, step := range []struct{ by, want int64 }{{1, 1}, {3, 4}, {10, 14}, {1, 15}} {
		got, err := s.Incr(ctx, k, "", step.by)
		if err != nil {
			return err
		}
		if err := expect(fmt.Sprintf("Incr(by=%d) range end", step.by), got, step.want); err != nil {
			return err
		}
	}
	return nil
}

func checkRejectsNonPositive(ctx context.Context, s sequence.Store, _ config) error {
	k := sequence.Key{Name: names{}.next()}
	if _, err := s.Incr(ctx, k, "", 1); err != nil {
		return err
	}
	for _, by := range []int64{0, -1, -100} {
		if _, err := s.Incr(ctx, k, "", by); err == nil {
			return fmt.Errorf("Incr(by=%d) must return an error", by)
		}
	}
	got, err := s.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	return expect("Incr after rejected calls (counter must not move)", got, 2)
}

func checkIsolation(ctx context.Context, s sequence.Store, _ config) error {
	n := names{}.next()
	base := sequence.Key{Name: n, Scope: "s"}
	variants := []struct {
		label  string
		k      sequence.Key
		period string
	}{
		{"base", base, "p"},
		{"other name", sequence.Key{Name: n + "x", Scope: "s"}, "p"},
		{"other scope", sequence.Key{Name: n, Scope: "t"}, "p"},
		{"other period", base, "q"},
	}
	// Interleave: each variant must see its own 1, 2, 3.
	for round := int64(1); round <= 3; round++ {
		for _, v := range variants {
			got, err := s.Incr(ctx, v.k, v.period, 1)
			if err != nil {
				return err
			}
			if err := expect(v.label+" round "+fmt.Sprint(round), got, round); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkEmptyScopeAndPeriod(ctx context.Context, s sequence.Store, _ config) error {
	n := names{}.next()
	cases := []struct {
		label  string
		k      sequence.Key
		period string
	}{
		{"empty scope, empty period", sequence.Key{Name: n}, ""},
		{"empty scope, period", sequence.Key{Name: n}, "p"},
		{"scope, empty period", sequence.Key{Name: n, Scope: "s"}, ""},
	}
	for round := int64(1); round <= 2; round++ {
		for _, c := range cases {
			got, err := s.Incr(ctx, c.k, c.period, 1)
			if err != nil {
				return err
			}
			if err := expect(c.label, got, round); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCaseAndWhitespace guards against databases whose default collation is case-insensitive or
// ignores trailing spaces (MySQL's): keys that differ only in case or trailing whitespace are
// different counters.
func checkCaseAndWhitespace(ctx context.Context, s sequence.Store, _ config) error {
	n := names{}.next()
	type part struct{ name, scope, period string }
	variants := []part{
		{n + "A", "s", "p"}, {n + "a", "s", "p"}, // name case
		{n + "A ", "s", "p"}, {n + "a ", "s", "p"}, // name trailing space
		{n + "A", "T", "p"}, {n + "A", "t", "p"}, {n + "A", "t ", "p"}, // scope case and space
		{n + "A", "s", "Q"}, {n + "A", "s", "q"}, {n + "A", "s", "q "}, // period case and space
	}
	for i, v := range variants {
		got, err := s.Incr(ctx, sequence.Key{Name: v.name, Scope: v.scope}, v.period, 1)
		if err != nil {
			return err
		}
		if err := expect(fmt.Sprintf("first Incr of variant %d %q: keys that differ only in case or trailing space must not share a counter", i, v), got, 1); err != nil {
			return err
		}
	}
	return nil
}

// fanOut runs workers goroutines of fn and collects the first error.
func fanOut(workers int, fn func(worker int) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(w); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func checkConcurrency(ctx context.Context, s sequence.Store, c config) error {
	k := sequence.Key{Name: names{}.next()}
	var mu sync.Mutex
	var all []int64
	err := fanOut(c.goroutines, func(int) error {
		local := make([]int64, 0, c.perGoroutine)
		for range c.perGoroutine {
			got, err := s.Incr(ctx, k, "p", 1)
			if err != nil {
				return err
			}
			local = append(local, got)
		}
		mu.Lock()
		all = append(all, local...)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	return wantExactly1ToN(all, int64(c.goroutines*c.perGoroutine))
}

func wantExactly1ToN(vals []int64, n int64) error {
	if int64(len(vals)) != n {
		return fmt.Errorf("got %d results, want %d", len(vals), n)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	for i, v := range vals {
		if v != int64(i)+1 {
			return fmt.Errorf("results are not exactly 1..%d: position %d holds %d (duplicate or gap)", n, i, v)
		}
	}
	return nil
}

type rng struct{ lo, hi int64 } // inclusive

func checkConcurrentRanges(ctx context.Context, s sequence.Store, c config) error {
	k := sequence.Key{Name: names{}.next()}
	var mu sync.Mutex
	var ranges []rng
	err := fanOut(c.goroutines, func(int) error {
		for range c.perGoroutine {
			r, err := rand.Int(rand.Reader, big.NewInt(5))
			if err != nil {
				return err
			}
			by := r.Int64() + 1
			end, err := s.Incr(ctx, k, "p", by)
			if err != nil {
				return err
			}
			mu.Lock()
			ranges = append(ranges, rng{end - by + 1, end})
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].lo < ranges[j].lo })
	next := int64(1)
	for _, r := range ranges {
		if r.lo != next {
			return fmt.Errorf("ranges overlap or leave a gap: expected next range to start at %d, got [%d,%d]", next, r.lo, r.hi)
		}
		next = r.hi + 1
	}
	return nil
}

func checkContext(ctx context.Context, s sequence.Store, _ config) error {
	k := sequence.Key{Name: names{}.next()}
	if _, err := s.Incr(ctx, k, "", 1); err != nil {
		return err
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := s.Incr(cancelled, k, "", 1)
	if err == nil {
		return errors.New("Incr with a cancelled ctx returned no error, want errors.Is(err, context.Canceled)")
	}
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("Incr with a cancelled ctx: want errors.Is(err, context.Canceled), got: %w", err)
	}
	got, err := s.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	return expect("Incr after a cancelled call (counter must not move)", got, 2)
}
