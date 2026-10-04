package sequence

import (
	"context"
	"fmt"
	"testing"
)

type localStore struct{ m map[prefetchKey]int64 }

func (l *localStore) Incr(_ context.Context, k Key, p string, by int64) (int64, error) {
	l.m[prefetchKey{k, p}] += by
	return l.m[prefetchKey{k, p}], nil
}

// A counter's in-memory state must be dropped when its block is used up, so memory follows the
// number of partly used blocks, not the number of tenants and periods ever seen.
func TestPrefetchDropsStateWhenBlockIsUsedUp(t *testing.T) {
	st, err := Prefetch(&localStore{m: map[prefetchKey]int64{}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	p := st.(*prefetchStore)
	ctx := context.Background()
	size := func() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.keys) }

	for i := range 1000 {
		k := Key{Name: "n", Scope: fmt.Sprint(i)}
		for range 2 { // exactly one block
			if _, err := p.Incr(ctx, k, "2026", 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	if size() != 0 {
		t.Fatalf("%d states left after every block was used up, want 0", size())
	}
	if _, err := p.Incr(ctx, Key{Name: "partial"}, "", 1); err != nil { // half a block
		t.Fatal(err)
	}
	if size() != 1 {
		t.Fatalf("a partly used block must stay: %d states", size())
	}
	if _, err := p.Incr(ctx, Key{Name: "partial"}, "", 1); err != nil {
		t.Fatal(err)
	}
	if size() != 0 {
		t.Fatalf("%d states left, want 0", size())
	}
}
