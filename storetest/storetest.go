// Package storetest is the executable form of the sequence.Store contract. Third-party
// stores run it in their own tests:
//
//	func TestMyStore(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) sequence.Store { return newMyStore(t) })
//	}
//
// New checks are added only in minor releases and are listed in the CHANGELOG.
package storetest

import (
	"context"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

type config struct {
	goroutines   int
	perGoroutine int
	timeout      time.Duration
}

// Option configures Run and RunTx.
type Option func(*config)

// WithConcurrency sets the size of the concurrency checks. Default: 50 goroutines of 20 calls.
func WithConcurrency(goroutines, perGoroutine int) Option {
	return func(c *config) { c.goroutines, c.perGoroutine = goroutines, perGoroutine }
}

// WithTimeout sets the context deadline of each subtest. Default: 30 seconds.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

func newConfig(opts []Option) config {
	c := config{goroutines: 50, perGoroutine: 20, timeout: 30 * time.Second}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Run executes the Incr conformance checks, plus the Resetter checks when the store implements
// sequence.Resetter. newStore is called once per subtest.
func Run(t *testing.T, newStore func(t *testing.T) sequence.Store, opts ...Option) {
	t.Helper()
	c := newConfig(opts)
	for _, ck := range coreChecks {
		t.Run(ck.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()
			if err := ck.run(ctx, newStore(t), c); err != nil {
				t.Fatal(err)
			}
		})
	}
	runResetter(t, newStore, c)
}

// runResetter executes the Resetter checks for stores that implement sequence.Resetter; other
// stores skip them.
func runResetter(t *testing.T, newStore func(t *testing.T) sequence.Store, c config) {
	t.Helper()
	t.Run("Resetter", func(t *testing.T) {
		if _, ok := newStore(t).(sequence.Resetter); !ok {
			t.Skip("store does not implement sequence.Resetter")
		}
		for _, ck := range resetterChecks {
			t.Run(ck.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
				defer cancel()
				s := newStore(t)
				if err := ck.run(ctx, s.(sequence.Resetter), s); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}
