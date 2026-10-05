package sequence

import (
	"context"
	"fmt"
	"sync"
)

const (
	minPrefetchBlock = 2
	maxPrefetchBlock = 1 << 20
)

// Prefetch wraps inner so that numbers are served from an in-memory block: one Incr of block on
// the inner store hands out block single increments. It cuts round trips by the block size and
// composes like bufio.NewReader. block must be 2..1,048,576.
//
// What you give up, by design:
//   - Numbers have gaps. A restart (or a failed formatting step) loses the unused rest of a block,
//     and a transaction cannot give a cached number back. Prefetch rejects a transaction-bound
//     store with ErrPrefetchInTx (detected through an InTx() bool method, which sqlstore.Store has).
//   - Numbers are unique but not monotonic across processes: each process holds its own block.
//   - Increments larger than 1 (Series.Reserve) bypass the block and go straight to the inner
//     store, so they keep Store's contiguous-range guarantee.
//   - The result implements Store only: Current and Reset return errors.ErrUnsupported on a
//     prefetched series, because the database counter is ahead of what was issued and a reset
//     cannot reach other processes' blocks. Run them through the underlying store with
//     Series.WithStore.
//
// A refill happens synchronously in the caller that finds the block empty, under that caller's
// ctx; there are no goroutines and nothing to close. Other callers for the same counter wait for
// the refill but stop waiting when their own ctx ends. A failed or cancelled refill stores
// nothing, and the next caller refills. A counter's in-memory state is dropped as soon as its
// block is used up and nobody is waiting.
func Prefetch(inner Store, block int) (Store, error) {
	if inner == nil {
		return nil, fmt.Errorf("%w: inner store must not be nil", ErrInvalidConfig)
	}
	if block < minPrefetchBlock || block > maxPrefetchBlock {
		return nil, fmt.Errorf("%w: prefetch block must be %d..%d, got %d", ErrInvalidConfig, minPrefetchBlock, maxPrefetchBlock, block)
	}
	if t, ok := inner.(interface{ InTx() bool }); ok && t.InTx() {
		return nil, ErrPrefetchInTx
	}
	return &prefetchStore{inner: inner, block: int64(block), keys: make(map[prefetchKey]*prefetchState)}, nil
}

type prefetchKey struct {
	key    Key
	period string
}

// prefetchState is one counter's cached block, the raw values next..end inclusive. lock is a
// one-slot channel used as a mutex a waiter can abandon when its ctx ends. refs counts the
// goroutines that hold or wait for the state so it can be deleted safely.
type prefetchState struct {
	lock      chan struct{}
	next, end int64
	refs      int
}

type prefetchStore struct {
	inner Store
	block int64

	mu   sync.Mutex
	keys map[prefetchKey]*prefetchState
}

func (p *prefetchStore) Incr(ctx context.Context, k Key, period string, by int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if by != 1 {
		return p.inner.Incr(ctx, k, period, by) // ranges and invalid input are the inner store's business
	}

	pk := prefetchKey{k, period}
	st := p.acquire(pk)
	defer p.release(pk, st)

	select {
	case st.lock <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	defer func() { <-st.lock }()

	if st.next > st.end {
		end, err := p.inner.Incr(ctx, k, period, p.block)
		if err != nil {
			return 0, err
		}
		st.next, st.end = end-p.block+1, end
	}
	v := st.next
	st.next++
	return v, nil
}

func (p *prefetchStore) acquire(pk prefetchKey) *prefetchState {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.keys[pk]
	if !ok {
		st = &prefetchState{lock: make(chan struct{}, 1), next: 1, end: 0}
		p.keys[pk] = st
	}
	st.refs++
	return st
}

// release drops the caller's reference. With no references left, the state is deleted once its
// block is used up; a partly used block stays so the next caller can finish it.
func (p *prefetchStore) release(pk prefetchKey, st *prefetchState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st.refs--
	if st.refs == 0 && st.next > st.end {
		delete(p.keys, pk)
	}
}
