package sequence_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func newSeq(t *testing.T, clock *fakeClock, opts ...sequence.Option) (*sequence.Sequencer, *memstore.Store) {
	t.Helper()
	st := memstore.New()
	if clock != nil {
		opts = append(opts, sequence.WithClock(clock.Now))
	}
	s, err := sequence.New(st, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}

func mustFormat(t *testing.T, tmpl string) *sequence.Format {
	t.Helper()
	f, err := sequence.ParseFormat(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func mustSeries(t *testing.T, s *sequence.Sequencer, name string, opts ...sequence.SeriesOption) *sequence.Series {
	t.Helper()
	ser, err := s.Series(name, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return ser
}

func next(t *testing.T, s *sequence.Series, opts ...sequence.CallOption) sequence.Number {
	t.Helper()
	n, err := s.Next(context.Background(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNextSequential(t *testing.T) {
	seq, _ := newSeq(t, &fakeClock{t: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)})
	inv := mustSeries(t, seq, "invoice", sequence.WithFormat(mustFormat(t, "{name}-{seq:5}")))
	for i, want := range []string{"invoice-00001", "invoice-00002", "invoice-00003"} {
		n := next(t, inv)
		if n.Value != want || n.Seq != int64(i+1) || n.Period != "" || n.Key != (sequence.Key{Name: "invoice"}) || n.String() != want {
			t.Fatalf("got %+v, want value %s", n, want)
		}
	}
}

func TestDefaults(t *testing.T) {
	seq, _ := newSeq(t, nil)
	if got := next(t, mustSeries(t, seq, "x")).Value; got != "1" {
		t.Fatalf("default format is {seq}, got %q", got)
	}
}

func TestWithStart(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithStart(1000))
	for _, want := range []int64{1000, 1001, 1002} {
		if got := next(t, s).Seq; got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	}
	s0 := mustSeries(t, seq, "zero", sequence.WithStart(0))
	if got := next(t, s0).Seq; got != 0 {
		t.Fatalf("start 0: got %d", got)
	}
}

func TestScopeIsolationAndRequire(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithFormat(mustFormat(t, "{scope}:{seq}")))
	got := []string{
		next(t, s, sequence.WithScope("a")).Value, next(t, s, sequence.WithScope("b")).Value,
		next(t, s).Value, next(t, s, sequence.WithScope("a")).Value,
	}
	if want := []string{"a:1", "b:1", ":1", "a:2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}

	req := mustSeries(t, seq, "r", sequence.RequireScope())
	for _, opts := range [][]sequence.CallOption{nil, {sequence.WithScope("")}} {
		if _, err := req.Next(context.Background(), opts...); !errors.Is(err, sequence.ErrScopeRequired) {
			t.Fatalf("err = %v, want ErrScopeRequired", err)
		}
	}
	if n := next(t, req, sequence.WithScope("t")); n.Seq != 1 || n.Key.Scope != "t" {
		t.Fatalf("got %+v", n)
	}
}

func TestInvalidCallInput(t *testing.T) {
	seq, st := newSeq(t, nil)
	s := mustSeries(t, seq, "x")
	for name, opt := range map[string]sequence.CallOption{
		"scope too long": sequence.WithScope(strings.Repeat("a", 129)),
		"scope bad utf8": sequence.WithScope("\xff"),
		"zero At":        sequence.At(time.Time{}),
	} {
		if _, err := s.Next(context.Background(), opt); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != 0 {
		t.Fatal("invalid input must not advance the counter")
	}
}

func TestPeriodRollover(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)}
	seq, _ := newSeq(t, clock)
	f := mustFormat(t, "{period}-{seq}")
	monthly := mustSeries(t, seq, "m", sequence.WithPeriod(sequence.Monthly), sequence.WithFormat(f))
	never := mustSeries(t, seq, "n", sequence.WithFormat(f))

	if a, b := next(t, monthly).Value, next(t, monthly).Value; a != "2026-01-1" || b != "2026-01-2" {
		t.Fatalf("got %s, %s", a, b)
	}
	next(t, never)
	clock.Set(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if got := next(t, monthly).Value; got != "2026-02-1" {
		t.Fatalf("monthly must reset, got %s", got)
	}
	if got := next(t, never).Value; got != "-2" {
		t.Fatalf("Never must not reset, got %s", got)
	}
}

func TestAtBackdating(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	seq, st := newSeq(t, clock)
	s := mustSeries(t, seq, "x", sequence.WithPeriod(sequence.Yearly), sequence.WithFormat(mustFormat(t, "{YYYY}-{MM}-{seq}")))
	if got := next(t, s, sequence.At(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))).Value; got != "2025-06-1" {
		t.Fatalf("got %s", got)
	}
	if got := next(t, s).Value; got != "2026-10-1" {
		t.Fatalf("current period must be untouched, got %s", got)
	}
	if st.Peek(sequence.Key{Name: "x"}, "2025") != 1 || st.Peek(sequence.Key{Name: "x"}, "2026") != 1 {
		t.Fatal("each period keeps its own counter")
	}
}

func TestLocation(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC)}
	for loc, want := range map[*time.Location]string{time.UTC: "2026", jakarta: "2027"} {
		seq, _ := newSeq(t, clock, sequence.WithLocation(loc))
		s := mustSeries(t, seq, "x", sequence.WithPeriod(sequence.Yearly))
		if got := next(t, s).Period; got != want {
			t.Errorf("%v: got %s, want %s", loc, got, want)
		}
	}
}

func TestVars(t *testing.T) {
	seq, st := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithFormat(mustFormat(t, "{var:branch}-{var:kind}-{seq}")))
	if got := next(t, s, sequence.Var("branch", "JKT"), sequence.Vars(map[string]string{"kind": "A"})).Value; got != "JKT-A-1" {
		t.Fatalf("got %s", got)
	}
	_, err := s.Next(context.Background(), sequence.Var("branch", "JKT"))
	if !errors.Is(err, sequence.ErrMissingVar) {
		t.Fatalf("err = %v", err)
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != 1 {
		t.Fatal("a missing variable must not burn a number")
	}
	// The map passed to Vars is copied.
	m := map[string]string{"branch": "A", "kind": "B"}
	opt := sequence.Vars(m)
	m["branch"] = "changed"
	if got := next(t, s, opt).Value; got != "A-B-2" {
		t.Fatalf("got %s", got)
	}
}

func TestCustomFormatterError(t *testing.T) {
	seq, _ := newSeq(t, nil)
	boom := errors.New("boom")
	s := mustSeries(t, seq, "x", sequence.WithFormat(sequence.FormatFunc(func(sequence.Parts) (string, error) { return "", boom })))
	if _, err := s.Next(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSeriesConfigErrors(t *testing.T) {
	seq, _ := newSeq(t, nil)
	bad := map[string][]sequence.SeriesOption{
		"negative start": {sequence.WithStart(-1)},
		"nil format":     {sequence.WithFormat(nil)},
		"nil period":     {sequence.WithPeriod(nil)},
	}
	for name, opts := range bad {
		if _, err := seq.Series("x", opts...); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	for name, n := range map[string]string{"empty": "", "129 bytes": strings.Repeat("a", 129), "bad utf8": "\xff"} {
		if _, err := seq.Series(n); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("name %s: err = %v", name, err)
		}
	}
}

func TestPeriodTooLong(t *testing.T) {
	seq, st := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithPeriod(func(time.Time) string { return strings.Repeat("p", 33) }))
	if _, err := s.Next(context.Background()); !errors.Is(err, sequence.ErrPeriodTooLong) {
		t.Fatalf("err = %v", err)
	}
	if st.Peek(sequence.Key{Name: "x"}, strings.Repeat("p", 33)) != 0 {
		t.Fatal("counter must not advance")
	}
	ok := mustSeries(t, seq, "y", sequence.WithPeriod(func(time.Time) string { return strings.Repeat("p", 32) }))
	next(t, ok)
}

type fixedStore struct{ raw int64 }

func (f fixedStore) Incr(context.Context, sequence.Key, string, int64) (int64, error) {
	return f.raw, nil
}

func TestOverflow(t *testing.T) {
	for name, c := range map[string]struct {
		raw   int64
		start int64
	}{"raw max": {math.MaxInt64, 2}, "start pushes over": {10, math.MaxInt64 - 5}} {
		seq, err := sequence.New(fixedStore{c.raw})
		if err != nil {
			t.Fatal(err)
		}
		s := mustSeries(t, seq, "x", sequence.WithStart(c.start))
		_, err = s.Next(context.Background())
		var ee *sequence.ExhaustedError
		if !errors.Is(err, sequence.ErrExhausted) || !errors.As(err, &ee) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// The last representable value still works.
	seq, _ := sequence.New(fixedStore{1})
	if n := next(t, mustSeries(t, seq, "x", sequence.WithStart(math.MaxInt64))); n.Seq != math.MaxInt64 {
		t.Fatalf("got %d", n.Seq)
	}
}

func TestContextPassesThrough(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	for _, sentinel := range []error{sequence.ErrInvalidConfig, sequence.ErrExhausted, sequence.ErrLockTimeout} {
		if errors.Is(err, sentinel) {
			t.Fatalf("a cancelled ctx must not look like %v", sentinel)
		}
	}
}

func TestWithStoreIsImmutable(t *testing.T) {
	seq, orig := newSeq(t, nil)
	s := mustSeries(t, seq, "x")
	other := memstore.New()
	derived := s.WithStore(other)
	next(t, derived)
	next(t, derived)
	next(t, s)
	if orig.Peek(sequence.Key{Name: "x"}, "") != 1 || other.Peek(sequence.Key{Name: "x"}, "") != 2 {
		t.Fatal("WithStore must not change the original or share counters")
	}
	if _, err := s.WithStore(nil).Next(context.Background()); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("nil store: err = %v", err)
	}
	next(t, s) // the original still works
}

func TestConcurrentNext(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithFormat(mustFormat(t, "N{seq}")))
	var mu sync.Mutex
	seen := make(map[int64]bool)
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				n, err := s.Next(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if seen[n.Seq] {
					t.Errorf("duplicate %d", n.Seq)
				}
				seen[n.Seq] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for i := int64(1); i <= 5000; i++ {
		if !seen[i] {
			t.Fatalf("gap at %d", i)
		}
	}
}
