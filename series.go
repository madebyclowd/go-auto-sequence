package sequence

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Series is a named counter with a format, a period and a start value. It is immutable and
// safe for concurrent use: create one per sequence and share it for the life of the process.
type Series struct {
	seq          *Sequencer
	store        Store
	name         string
	format       Formatter
	period       Period
	start        int64
	requireScope bool
}

// Series defines a sequence. Everything is validated here, never at Next time: every error
// wraps ErrInvalidConfig.
func (s *Sequencer) Series(name string, opts ...SeriesOption) (*Series, error) {
	cfg := seriesConfig{period: Never, start: 1}
	for _, o := range opts {
		o(&cfg)
	}
	if err := (Key{Name: name}).validate(); err != nil {
		return nil, err
	}
	if cfg.start < 0 {
		return nil, fmt.Errorf("%w: start must be >= 0, got %d", ErrInvalidConfig, cfg.start)
	}
	if cfg.period == nil {
		return nil, fmt.Errorf("%w: period must not be nil", ErrInvalidConfig)
	}
	if cfg.formatSet && cfg.format == nil {
		return nil, fmt.Errorf("%w: format must not be nil", ErrInvalidConfig)
	}
	if cfg.format == nil {
		f, err := ParseFormat("{seq}")
		if err != nil {
			return nil, err
		}
		cfg.format = f
	}
	return &Series{
		seq: s, store: s.store, name: name, format: cfg.format, period: cfg.period,
		start: cfg.start, requireScope: cfg.requireScope,
	}, nil
}

// WithStore returns a copy of the series that uses st, typically a store bound to a
// transaction: invoice.WithStore(store.WithTx(tx)). The original is unchanged. A nil st
// makes the next call return ErrInvalidConfig.
func (s *Series) WithStore(st Store) *Series {
	c := *s
	c.store = st
	return &c
}

// Next issues the next number.
//
// The counter advances before the template is rendered. Missing {var:} values of a *Format
// are detected first, so the common mistake never burns a number; a custom Formatter that
// fails does, and on a pool-bound store that leaves a gap (a transaction-bound store gives
// the number back when the caller rolls back).
func (s *Series) Next(ctx context.Context, opts ...CallOption) (Number, error) {
	if s.store == nil {
		return Number{}, fmt.Errorf("%w: store must not be nil", ErrInvalidConfig)
	}
	cfg := newCallConfig(opts)
	key, period, at, err := s.resolve(cfg)
	if err != nil {
		return Number{}, err
	}

	if f, ok := s.format.(*Format); ok {
		if err := f.checkVars(cfg.vars); err != nil {
			return Number{}, err
		}
	}

	raw, err := s.store.Incr(ctx, key, period, 1)
	if err != nil {
		return Number{}, err
	}
	if raw < 1 || raw-1 > math.MaxInt64-s.start {
		return Number{}, &ExhaustedError{Key: key, Period: period, Max: math.MaxInt64, Seq: raw}
	}
	seq := s.start + (raw - 1)

	value, err := s.format.Format(Parts{Key: key, Period: period, Seq: seq, At: at, Vars: cfg.vars})
	if err != nil {
		return Number{}, err
	}
	return Number{Value: value, Seq: seq, Period: period, Key: key}, nil
}

// resolve validates the per-call input and works out the counter partition and the issue time.
func (s *Series) resolve(cfg callConfig) (key Key, period string, at time.Time, err error) {
	if cfg.atSet && cfg.at.IsZero() {
		return Key{}, "", time.Time{}, fmt.Errorf("%w: At time must not be the zero time", ErrInvalidConfig)
	}
	if s.requireScope && cfg.scope == "" {
		return Key{}, "", time.Time{}, fmt.Errorf("%w: series %q", ErrScopeRequired, s.name)
	}
	key = Key{Name: s.name, Scope: cfg.scope}
	if err := key.validate(); err != nil {
		return Key{}, "", time.Time{}, err
	}
	at = cfg.at
	if !cfg.atSet {
		at = s.seq.clock()
	}
	at = at.In(s.seq.loc)
	period = s.period(at)
	if len(period) > maxPeriodLen {
		return Key{}, "", time.Time{}, fmt.Errorf("%w: %d bytes, limit %d", ErrPeriodTooLong, len(period), maxPeriodLen)
	}
	return key, period, at, nil
}
