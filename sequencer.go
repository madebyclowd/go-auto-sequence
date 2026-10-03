package sequence

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Option configures a Sequencer.
type Option func(*config)

type config struct {
	clock  func() time.Time
	loc    *time.Location
	logger *slog.Logger
}

// WithClock sets the time source used for periods and date tokens. Default: time.Now.
func WithClock(now func() time.Time) Option { return func(c *config) { c.clock = now } }

// WithLocation sets the location periods and date tokens are computed in. Default: UTC.
func WithLocation(loc *time.Location) Option { return func(c *config) { c.loc = loc } }

// WithLogger sets the logger. Default: a logger that discards everything.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// Sequencer binds a Store to a clock, a location and a logger, and creates series.
// It has no exported mutable state and is safe for concurrent use.
type Sequencer struct {
	store  Store
	clock  func() time.Time
	loc    *time.Location
	logger *slog.Logger
}

// New returns a Sequencer. A nil store, clock, location or logger is ErrInvalidConfig.
func New(store Store, opts ...Option) (*Sequencer, error) {
	c := config{clock: time.Now, loc: time.UTC, logger: slog.New(discardHandler{})}
	for _, o := range opts {
		o(&c)
	}
	switch {
	case store == nil:
		return nil, fmt.Errorf("%w: store must not be nil", ErrInvalidConfig)
	case c.clock == nil:
		return nil, fmt.Errorf("%w: clock must not be nil", ErrInvalidConfig)
	case c.loc == nil:
		return nil, fmt.Errorf("%w: location must not be nil", ErrInvalidConfig)
	case c.logger == nil:
		return nil, fmt.Errorf("%w: logger must not be nil", ErrInvalidConfig)
	}
	return &Sequencer{store: store, clock: c.clock, loc: c.loc, logger: c.logger}, nil
}

// discardHandler drops every record (slog.DiscardHandler needs Go 1.24; the floor is 1.22).
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }
