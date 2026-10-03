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
