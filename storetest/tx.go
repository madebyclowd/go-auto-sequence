package storetest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// TxHarness lets RunTx drive a store that can bind to a caller transaction.
type TxHarness struct {
	// Begin starts a transaction and returns a store bound to it. finish(true) commits and
	// finish(false) rolls back.
	Begin func(t *testing.T) (tx sequence.Store, finish func(commit bool) error)
	// Outside reads and advances the same counters outside any transaction.
	Outside sequence.Store
}

type txCheck func(ctx context.Context, h TxHarness, t *testing.T) error

var txChecks = []struct {
	name string
	run  txCheck
}{
	{"RollbackGivesNumberBack", checkRollback},
	{"CommitKeepsNumber", checkCommit},
	{"ConcurrentTransactionsSerialize", checkTxSerialize},
}

// RunTx executes the transaction checks. newHarness is called once per subtest.
func RunTx(t *testing.T, newHarness func(t *testing.T) TxHarness, opts ...Option) {
	t.Helper()
	c := newConfig(opts)
	for _, ck := range txChecks {
		t.Run(ck.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()
			if err := ck.run(ctx, newHarness(t), t); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func checkRollback(ctx context.Context, h TxHarness, t *testing.T) error {
	k := sequence.Key{Name: names{}.next()}
	tx, finish := h.Begin(t)
	got, err := tx.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	if err := expect("Incr inside the transaction", got, 1); err != nil {
		return err
	}
	if err := finish(false); err != nil {
		return err
	}
	got, err = h.Outside.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	return expect("Incr outside after rollback (the number must be given back)", got, 1)
}

func checkCommit(ctx context.Context, h TxHarness, t *testing.T) error {
	k := sequence.Key{Name: names{}.next()}
	tx, finish := h.Begin(t)
	got, err := tx.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	if err := expect("Incr inside the transaction", got, 1); err != nil {
		return err
	}
	if err := finish(true); err != nil {
		return err
	}
	got, err = h.Outside.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	return expect("Incr outside after commit", got, 2)
}

func checkTxSerialize(ctx context.Context, h TxHarness, t *testing.T) error {
	k := sequence.Key{Name: names{}.next()}
	var mu sync.Mutex
	var all []int64
	err := fanOut(2, func(int) error {
		tx, finish := h.Begin(t)
		got, err := tx.Incr(ctx, k, "", 1)
		if err != nil {
			_ = finish(false)
			return err
		}
		if err := finish(true); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		mu.Lock()
		all = append(all, got)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	return wantExactly1ToN(all, 2)
}
