package sequence

import (
	"context"
	"fmt"
	"math"
	"math/big"
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
	maxSet       bool
	max          int64
	threshold    int64 // visible value of the warning threshold; valid when maxSet
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
	ser := &Series{
		seq: s, store: s.store, name: name, format: cfg.format, period: cfg.period,
		start: cfg.start, requireScope: cfg.requireScope,
	}
	if cfg.maxSet {
		if cfg.max < cfg.start {
			return nil, fmt.Errorf("%w: max (%d) must be >= start (%d)", ErrInvalidConfig, cfg.max, cfg.start)
		}
		if cfg.thresholdPct < 1 || cfg.thresholdPct > 100 {
			return nil, fmt.Errorf("%w: threshold percent must be 1..100, got %d", ErrInvalidConfig, cfg.thresholdPct)
		}
		ser.maxSet, ser.max, ser.threshold = true, cfg.max, thresholdSeq(cfg.start, cfg.max, cfg.thresholdPct)
	}
	return ser, nil
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
	if err := s.checkMax(key, period, seq); err != nil {
		return Number{}, err
	}

	value, err := s.format.Format(Parts{Key: key, Period: period, Seq: seq, At: at, Vars: cfg.vars})
	if err != nil {
		return Number{}, err
	}
	s.notify(ctx, key, period, seq, seq)
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

func (s *Series) checkMax(key Key, period string, last int64) error {
	if s.maxSet && last > s.max {
		return &ExhaustedError{Key: key, Period: period, Max: s.max, Seq: last}
	}
	return nil
}

// notify reports a threshold crossing for the values first..last issued by one call. The
// crossing happened in this call when the value before first was below the threshold and last
// reached it; the increments are unique, so exactly one call sees it.
func (s *Series) notify(ctx context.Context, key Key, period string, first, last int64) {
	if !s.maxSet || first > s.threshold || last < s.threshold {
		return
	}
	e := Exhaustion{Key: key, Period: period, Seq: max(first, s.threshold), Max: s.max, Threshold: s.threshold}
	if h := s.seq.onExhaust; h != nil {
		h(ctx, e)
		return
	}
	s.seq.logger.WarnContext(ctx, "sequence nearing exhaustion",
		"name", key.Name, "scope", key.Scope, "period", period, "seq", e.Seq, "threshold", e.Threshold, "max", e.Max)
}

// thresholdSeq is the visible value of the warning threshold: the pct-th percent of the
// numbers from start to max, rounded up, counted from start.
func thresholdSeq(start, maxVal int64, pct int) int64 {
	count := new(big.Int).Sub(big.NewInt(maxVal), big.NewInt(start))
	count.Add(count, big.NewInt(1))
	pos := new(big.Int).Mul(count, big.NewInt(int64(pct)))
	pos.Add(pos, big.NewInt(99))
	pos.Div(pos, big.NewInt(100)) // ceil(count*pct/100), at least 1
	pos.Add(pos, big.NewInt(start-1))
	return pos.Int64()
}
