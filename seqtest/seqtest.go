// Package seqtest gives tests of code that issues numbers a ready fixture: a Sequencer on an
// in-memory store and a fake clock you move yourself.
//
//	f := seqtest.New(t)
//	inv := f.Series("invoice", sequence.WithPeriod(sequence.Monthly))
//	n, _ := inv.Next(ctx)
//	f.Clock.Advance(32 * 24 * time.Hour) // the month rolls over
//
// It deliberately has no assertion helpers: compare with a plain if and t.Errorf so the failure
// message says what you meant. Tests that need a real database use package storetest and their
// own wiring. For checking a Store implementation, see package storetest.
package seqtest

import (
	"sync"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

// defaultTime is the fake clock's start: a Wednesday in the middle of a month, a quarter and an
// ISO week (and of the year), so a test never sits on a period boundary by accident.
var defaultTime = time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)

// Clock is a fake time source. It never moves on its own; the test moves it. It is safe for
// concurrent use, because the sequencer may call Now from many goroutines.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the current fake time. It matches the signature sequence.WithClock expects.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// Advance moves the clock forward by d (backward if d is negative).
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Fixture is a Sequencer wired to an in-memory store and a fake clock.
type Fixture struct {
	Sequencer *sequence.Sequencer
	Store     *memstore.Store // use Peek to read a raw counter
	Clock     *Clock

	t testing.TB
}

// Option configures New.
type Option func(*config)

type config struct {
	start   time.Time
	seqOpts []sequence.Option
}

// At sets the fake clock's start time. Default: 2026-06-17 12:00:00 UTC.
func At(t time.Time) Option { return func(c *config) { c.start = t } }

// WithSequencerOptions forwards options to sequence.New, for example WithLocation, WithLogger or
// WithExhaustionHandler. They are applied after the fixture's own clock, so passing WithClock
// replaces the fake clock: Fixture.Clock then no longer drives the sequencer.
func WithSequencerOptions(opts ...sequence.Option) Option {
	return func(c *config) { c.seqOpts = append(c.seqOpts, opts...) }
}

// New returns a Fixture. It marks itself a test helper and fails the test with t.Fatalf if an
// option is invalid, so there is no error to check.
func New(t testing.TB, opts ...Option) *Fixture {
	t.Helper()
	cfg := config{start: defaultTime}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.start.IsZero() {
		t.Fatalf("seqtest: At time must not be the zero time")
	}
	store := memstore.New()
	clock := &Clock{t: cfg.start}
	seq, err := sequence.New(store, append([]sequence.Option{sequence.WithClock(clock.Now)}, cfg.seqOpts...)...)
	if err != nil {
		t.Fatalf("seqtest: %v", err)
	}
	return &Fixture{Sequencer: seq, Store: store, Clock: clock, t: t}
}

// Series defines a series on the fixture's Sequencer and fails the test with t.Fatalf if the
// definition is invalid.
func (f *Fixture) Series(name string, opts ...sequence.SeriesOption) *sequence.Series {
	f.t.Helper()
	s, err := f.Sequencer.Series(name, opts...)
	if err != nil {
		f.t.Fatalf("seqtest: series %q: %v", name, err)
	}
	return s
}
