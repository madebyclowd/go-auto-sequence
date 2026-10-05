package memstore_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
	"github.com/madebyclowd/go-auto-sequence/storetest"
)

var _ sequence.Store = (*memstore.Store)(nil)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(*testing.T) sequence.Store { return memstore.New() })
}

func TestHeavyConcurrency(t *testing.T) {
	s := memstore.New()
	k := sequence.Key{Name: "heavy"}
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if _, err := s.Incr(context.Background(), k, "", 1); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := s.Peek(k, ""); got != 20000 {
		t.Fatalf("Peek = %d, want 20000", got)
	}
}

func TestCancelledContextDoesNotAdvance(t *testing.T) {
	s := memstore.New()
	k := sequence.Key{Name: "c"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Incr(ctx, k, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got := s.Peek(k, ""); got != 0 {
		t.Fatalf("Peek = %d, want 0", got)
	}
}

func TestOverflowDoesNotWrap(t *testing.T) {
	s := memstore.New()
	k := sequence.Key{Name: "o"}
	if _, err := s.Incr(context.Background(), k, "", math.MaxInt64-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Incr(context.Background(), k, "", 2); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatalf("err = %v, want ErrExhausted", err)
	}
	if got := s.Peek(k, ""); got != math.MaxInt64-1 {
		t.Fatalf("counter moved to %d", got)
	}
}

func TestResetterRoundTrip(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	k := sequence.Key{Name: "r"}
	if _, ok, _ := s.Current(ctx, k, ""); ok {
		t.Fatal("unknown partition must report ok=false")
	}
	if err := s.Set(ctx, k, "", 7); err != nil {
		t.Fatal(err)
	}
	if raw, ok, _ := s.Current(ctx, k, ""); !ok || raw != 7 || s.Peek(k, "") != 7 {
		t.Fatalf("got (%d, %v)", raw, ok)
	}
	if err := s.Set(ctx, k, "", -1); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}

func BenchmarkIncr(b *testing.B) {
	s := memstore.New()
	ctx := context.Background()
	k := sequence.Key{Name: "invoice"}
	b.ReportAllocs()
	for range b.N {
		_, _ = s.Incr(ctx, k, "", 1)
	}
}
