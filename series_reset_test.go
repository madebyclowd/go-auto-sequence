package sequence_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

func TestCurrent(t *testing.T) {
	seq, _ := newSeq(t, nil)
	ctx := context.Background()
	s := mustSeries(t, seq, "x", sequence.WithStart(1000))

	if last, issued, err := s.Current(ctx); err != nil || issued || last != 0 {
		t.Fatalf("before any number: (%d, %v, %v)", last, issued, err)
	}
	next(t, s)
	next(t, s)
	if last, issued, err := s.Current(ctx); err != nil || !issued || last != 1001 {
		t.Fatalf("after two numbers: (%d, %v, %v), want (1001, true)", last, issued, err)
	}
	// A real value of 0 is distinguishable from "none".
	zero := mustSeries(t, seq, "zero", sequence.WithStart(0))
	next(t, zero)
	if last, issued, err := zero.Current(ctx); err != nil || !issued || last != 0 {
		t.Fatalf("start 0, one number: (%d, %v, %v), want (0, true)", last, issued, err)
	}
	// Scope and period select the partition; Current never issues a number.
	if _, issued, _ := s.Current(ctx, sequence.WithScope("other")); issued {
		t.Fatal("another scope has no numbers")
	}
	if last, _, _ := s.Current(ctx); last != 1001 {
		t.Fatalf("Current must not issue a number, got %d", last)
	}
}

func TestCurrentUsesPeriod(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)}
	seq, _ := newSeq(t, clock)
	s := mustSeries(t, seq, "x", sequence.WithPeriod(sequence.Monthly))
	next(t, s)
	next(t, s)
	clock.Set(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if _, issued, _ := s.Current(context.Background()); issued {
		t.Fatal("a new month has no numbers yet")
	}
	if last, issued, _ := s.Current(context.Background(), sequence.At(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))); !issued || last != 2 {
		t.Fatalf("At selects the January partition, got (%d, %v)", last, issued)
	}
}

func TestReset(t *testing.T) {
	seq, st := newSeq(t, nil)
	ctx := context.Background()
	s := mustSeries(t, seq, "x", sequence.WithStart(100), sequence.WithFormat(mustFormat(t, "N{seq}")))
	for range 3 {
		next(t, s)
	}
	if err := s.Reset(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if got := next(t, s).Value; got != "N500" {
		t.Fatalf("after Reset(500) the next number is %s", got)
	}
	// Reset back to the start.
	if err := s.Reset(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if last, issued, _ := s.Current(ctx); issued {
		t.Fatalf("Reset(start) leaves nothing issued, got (%d, %v)", last, issued)
	}
	if got := next(t, s).Seq; got != 100 {
		t.Fatalf("got %d, want 100", got)
	}
	// Drift repair idiom: Reset(highest+1).
	if err := s.Reset(ctx, 901); err != nil {
		t.Fatal(err)
	}
	if got := next(t, s).Seq; got != 901 {
		t.Fatalf("got %d", got)
	}
	// next below start is a config error and changes nothing.
	before := st.Peek(sequence.Key{Name: "x"}, "")
	if err := s.Reset(ctx, 99); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
	if st.Peek(sequence.Key{Name: "x"}, "") != before {
		t.Fatal("a rejected Reset must not change the counter")
	}
	// Scope applies, and RequireScope is enforced.
	if err := s.Reset(ctx, 700, sequence.WithScope("t")); err != nil {
		t.Fatal(err)
	}
	if got := next(t, s, sequence.WithScope("t")).Seq; got != 700 {
		t.Fatalf("got %d", got)
	}
	req := mustSeries(t, seq, "r", sequence.RequireScope())
	if err := req.Reset(ctx, 5); !errors.Is(err, sequence.ErrScopeRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := req.Current(ctx); !errors.Is(err, sequence.ErrScopeRequired) {
		t.Fatalf("err = %v", err)
	}
}

type incrOnlyStore struct{ s sequence.Store }

func (o incrOnlyStore) Incr(ctx context.Context, k sequence.Key, p string, by int64) (int64, error) {
	return o.s.Incr(ctx, k, p, by)
}

func TestCurrentAndResetNeedResetter(t *testing.T) {
	seq, err := sequence.New(incrOnlyStore{memstore.New()})
	if err != nil {
		t.Fatal(err)
	}
	s := mustSeries(t, seq, "x")
	ctx := context.Background()
	if _, _, err := s.Current(ctx); !errors.Is(err, errors.ErrUnsupported) || !strings.Contains(err.Error(), "Resetter") {
		t.Fatalf("Current err = %v", err)
	}
	if err := s.Reset(ctx, 5); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Reset err = %v", err)
	}
	next(t, s) // Next still works without the capability
}

func TestResetAndCurrentWithNilStore(t *testing.T) {
	seq, _ := newSeq(t, nil)
	s := mustSeries(t, seq, "x").WithStore(nil)
	if err := s.Reset(context.Background(), 1); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := s.Current(context.Background()); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}
