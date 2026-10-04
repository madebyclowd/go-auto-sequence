package sequence_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

// counting records how many times the inner store is called.
type counting struct {
	sequence.Store
	calls atomic.Int64
}

func (c *counting) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	c.calls.Add(1)
	return c.Store.Incr(ctx, k, p, by)
}

func newPrefetch(t *testing.T, block int) (sequence.Store, *counting) {
	t.Helper()
	inner := &counting{Store: memstore.New()}
	p, err := sequence.Prefetch(inner, block)
	if err != nil {
		t.Fatal(err)
	}
	return p, inner
}

func TestPrefetchServesFromBlock(t *testing.T) {
	p, inner := newPrefetch(t, 10)
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	for i := int64(1); i <= 25; i++ {
		got, err := p.Incr(ctx, k, "", 1)
		if err != nil || got != i {
			t.Fatalf("call %d: got %d, %v", i, got, err)
		}
	}
	if inner.calls.Load() != 3 {
		t.Fatalf("inner calls = %d, want 3 (25 numbers in blocks of 10)", inner.calls.Load())
	}
}

func TestPrefetchConcurrentUniqueAndFewRoundTrips(t *testing.T) {
	p, inner := newPrefetch(t, 50)
	const workers, each = 200, 100
	var mu sync.Mutex
	seen := make(map[int64]bool, workers*each)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				v, err := p.Incr(context.Background(), sequence.Key{Name: "x"}, "p", 1)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if seen[v] {
					t.Errorf("duplicate %d", v)
				}
				seen[v] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for i := int64(1); i <= workers*each; i++ {
		if !seen[i] {
			t.Fatalf("gap at %d (a single process must hand out exactly 1..N)", i)
		}
	}
	if got, want := inner.calls.Load(), int64(workers*each/50); got != want {
		t.Fatalf("inner calls = %d, want exactly %d: concurrent callers must share each refill", got, want)
	}
}

func TestPrefetchPassesRangesThrough(t *testing.T) {
	p, inner := newPrefetch(t, 10)
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	if got, _ := p.Incr(ctx, k, "", 1); got != 1 { // takes block 1..10
		t.Fatal(got)
	}
	got, err := p.Incr(ctx, k, "", 5) // goes straight to the inner store
	if err != nil || got != 15 {
		t.Fatalf("range end = %d, %v; want 15 (a contiguous 11..15 after the cached block)", got, err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("inner calls = %d, want 2", inner.calls.Load())
	}
	if _, err := p.Incr(ctx, k, "", 0); err == nil {
		t.Fatal("by=0 must be rejected by the inner store")
	}
}

func TestPrefetchHidesResetter(t *testing.T) {
	p, _ := newPrefetch(t, 5)
	if _, ok := p.(sequence.Resetter); ok {
		t.Fatal("Prefetch must not implement Resetter")
	}
	seq, _ := sequence.New(p)
	s, _ := seq.Series("x")
	if _, _, err := s.Current(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Current: %v", err)
	}
	if err := s.Reset(context.Background(), 5); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Reset: %v", err)
	}
	if n, err := s.Next(context.Background()); err != nil || n.Seq != 1 {
		t.Fatalf("Next: %+v, %v", n, err)
	}
	// Reserve bypasses the block and stays contiguous.
	ns, err := s.Reserve(context.Background(), 4)
	if err != nil || ns[0].Seq != 6 || ns[3].Seq != 9 {
		t.Fatalf("Reserve through Prefetch: %+v, %v (block 1..5 is cached, the range follows it)", ns, err)
	}
}

type txStore struct {
	sequence.Store
	inTx bool
}

func (t txStore) InTx() bool { return t.inTx }

func TestPrefetchConstruction(t *testing.T) {
	if _, err := sequence.Prefetch(txStore{memstore.New(), true}, 10); !errors.Is(err, sequence.ErrPrefetchInTx) {
		t.Fatalf("tx-bound: %v", err)
	}
	if _, err := sequence.Prefetch(txStore{memstore.New(), false}, 10); err != nil {
		t.Fatalf("pool-like store with InTx() == false: %v", err)
	}
	if _, err := sequence.Prefetch(memstore.New(), 10); err != nil {
		t.Fatalf("store without InTx: %v", err)
	}
	if _, err := sequence.Prefetch(nil, 10); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("nil inner: %v", err)
	}
	for _, b := range []int{-1, 0, 1, 1<<20 + 1} {
		if _, err := sequence.Prefetch(memstore.New(), b); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("block %d: %v", b, err)
		}
	}
	for _, b := range []int{2, 1 << 20} {
		if _, err := sequence.Prefetch(memstore.New(), b); err != nil {
			t.Errorf("block %d: %v", b, err)
		}
	}
}

// gate is a store whose Incr blocks until released, to hold a refill open.
type gate struct {
	sequence.Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gate) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return g.Store.Incr(ctx, k, p, by)
}

func TestPrefetchWaiterHonoursItsOwnContext(t *testing.T) {
	g := &gate{Store: memstore.New(), entered: make(chan struct{}), release: make(chan struct{})}
	p, _ := sequence.Prefetch(g, 5)
	k := sequence.Key{Name: "x"}

	first := make(chan error, 1)
	go func() {
		_, err := p.Incr(context.Background(), k, "", 1)
		first <- err
	}()
	<-g.entered // the first caller is inside the refill

	short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := p.Incr(short, k, "", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter err = %v, want its own deadline", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the waiter did not stop waiting at its own deadline")
	}
	close(g.release)
	if err := <-first; err != nil {
		t.Fatalf("the refilling caller must still succeed: %v", err)
	}
}

type flaky struct {
	sequence.Store
	fail atomic.Bool
}

func (f *flaky) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	if f.fail.Load() {
		return 0, errors.New("database down")
	}
	return f.Store.Incr(ctx, k, p, by)
}

func TestPrefetchFailedRefillStoresNothing(t *testing.T) {
	f := &flaky{Store: memstore.New()}
	p, _ := sequence.Prefetch(f, 5)
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	f.fail.Store(true)
	if _, err := p.Incr(ctx, k, "", 1); err == nil {
		t.Fatal("want the refill error")
	}
	f.fail.Store(false)
	if got, err := p.Incr(ctx, k, "", 1); err != nil || got != 1 {
		t.Fatalf("after recovery got %d, %v; a failed refill must not lose or skip numbers", got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Incr(cancelled, k, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if got, _ := p.Incr(ctx, k, "", 1); got != 2 {
		t.Fatalf("a cancelled call must not consume a number, got %d", got)
	}
}

func TestPrefetchIsolatesKeysScopesAndPeriods(t *testing.T) {
	p, _ := newPrefetch(t, 4)
	ctx := context.Background()
	for _, k := range []struct {
		key    sequence.Key
		period string
	}{{sequence.Key{Name: "a"}, ""}, {sequence.Key{Name: "a", Scope: "t"}, ""}, {sequence.Key{Name: "a"}, "2026"}, {sequence.Key{Name: "b"}, ""}} {
		for want := int64(1); want <= 6; want++ {
			if got, err := p.Incr(ctx, k.key, k.period, 1); err != nil || got != want {
				t.Fatalf("%+v: got %d, %v, want %d", k, got, err, want)
			}
		}
	}
}

func TestPrefetchWorksUnderASeries(t *testing.T) {
	p, inner := newPrefetch(t, 20)
	seq, _ := sequence.New(p)
	s, _ := seq.Series("inv", sequence.WithFormat(mustFormat(t, "INV-{seq:3}")), sequence.WithMax(30, 90))
	var last string
	for range 30 {
		n, err := s.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		last = n.Value
	}
	if last != "INV-030" {
		t.Fatalf("last = %s", last)
	}
	if _, err := s.Next(context.Background()); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatalf("err = %v", err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("inner calls = %d, want 2 for 31 numbers in blocks of 20", inner.calls.Load())
	}
}
