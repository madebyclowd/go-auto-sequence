package sequence_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

func TestReserve(t *testing.T) {
	seq, st := newSeq(t, &fakeClock{t: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()
	s := mustSeries(t, seq, "inv", sequence.WithStart(100), sequence.WithPeriod(sequence.Monthly),
		sequence.WithFormat(mustFormat(t, "{period}/{var:branch}/{seq:4}")))

	next(t, s, sequence.Var("branch", "A")) // seq 100
	ns, err := s.Reserve(ctx, 3, sequence.Var("branch", "JKT"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-03/JKT/0101", "2026-03/JKT/0102", "2026-03/JKT/0103"}
	for i, n := range ns {
		if n.Value != want[i] || n.Seq != int64(101+i) || n.Period != "2026-03" || n.Key.Name != "inv" {
			t.Fatalf("number %d = %+v, want value %s", i, n, want[i])
		}
	}
	if got := next(t, s, sequence.Var("branch", "A")).Seq; got != 104 {
		t.Fatalf("after Reserve(3) the next number is %d, want 104", got)
	}
	if st.Peek(sequence.Key{Name: "inv"}, "2026-03") != 5 {
		t.Fatal("Reserve(3) must advance the counter by exactly 3")
	}
}

func TestReserveScopeAtAndRequireScope(t *testing.T) {
	seq, _ := newSeq(t, &fakeClock{t: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()
	s := mustSeries(t, seq, "x", sequence.RequireScope(), sequence.WithPeriod(sequence.Yearly))
	if _, err := s.Reserve(ctx, 2); !errors.Is(err, sequence.ErrScopeRequired) {
		t.Fatalf("err = %v", err)
	}
	ns, err := s.Reserve(ctx, 2, sequence.WithScope("t"), sequence.At(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil || ns[0].Period != "2025" || ns[0].Key.Scope != "t" || ns[1].Seq != 2 {
		t.Fatalf("got %+v, %v", ns, err)
	}
}

func TestReserveCountLimits(t *testing.T) {
	seq, st := newSeq(t, nil)
	s := mustSeries(t, seq, "x")
	for _, n := range []int{0, -1, 1<<20 + 1} {
		if _, err := s.Reserve(context.Background(), n); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("n=%d: err = %v", n, err)
		}
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != 0 {
		t.Fatal("a rejected count must not advance the counter")
	}
	if ns, err := s.Reserve(context.Background(), 1<<20); err != nil || len(ns) != 1<<20 {
		t.Fatalf("the maximum count must work: %d, %v", len(ns), err)
	}
}

func TestReserveMissingVarDoesNotBurnRange(t *testing.T) {
	seq, st := newSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithFormat(mustFormat(t, "{var:b}-{seq}")))
	if _, err := s.Reserve(context.Background(), 10); !errors.Is(err, sequence.ErrMissingVar) {
		t.Fatalf("err = %v", err)
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != 0 {
		t.Fatal("counter must not move")
	}
}

func TestReserveCustomFormatterErrorConsumesRange(t *testing.T) {
	seq, st := newSeq(t, nil)
	boom := errors.New("boom")
	s := mustSeries(t, seq, "x", sequence.WithFormat(sequence.FormatFunc(func(p sequence.Parts) (string, error) {
		if p.Seq == 3 {
			return "", boom
		}
		return "ok", nil
	})))
	ns, err := s.Reserve(context.Background(), 5)
	if !errors.Is(err, boom) || ns != nil {
		t.Fatalf("got %v, %v", ns, err)
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != 5 {
		t.Fatal("a late formatter error leaves the whole range consumed, as documented")
	}
}

func TestReserveOverflowAndContext(t *testing.T) {
	seq, _ := sequence.New(fixedStore{math.MaxInt64})
	s := mustSeries(t, seq, "x", sequence.WithStart(2))
	if _, err := s.Reserve(context.Background(), 3); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatalf("err = %v", err)
	}
	seq2, _ := newSeq(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mustSeries(t, seq2, "y").Reserve(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := mustSeries(t, seq2, "z").WithStore(nil).Reserve(context.Background(), 2); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("nil store: %v", err)
	}
}

// Concurrent Reserve and Next calls must hand out disjoint ranges that together cover 1..N.
func TestReserveConcurrentRangesAreDisjoint(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x")
	var mu sync.Mutex
	seen := map[int64]bool{}
	var total int64
	var wg sync.WaitGroup
	for g := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				var ns []sequence.Number
				var err error
				if g%2 == 0 {
					ns, err = s.Reserve(context.Background(), 7)
				} else {
					var n sequence.Number
					n, err = s.Next(context.Background())
					ns = []sequence.Number{n}
				}
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				for i, n := range ns {
					if i > 0 && n.Seq != ns[i-1].Seq+1 {
						t.Errorf("range not contiguous: %d then %d", ns[i-1].Seq, n.Seq)
					}
					if seen[n.Seq] {
						t.Errorf("duplicate %d", n.Seq)
					}
					seen[n.Seq] = true
					total++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for i := int64(1); i <= total; i++ {
		if !seen[i] {
			t.Fatalf("gap at %d of %d", i, total)
		}
	}
}
