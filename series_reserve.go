package sequence

import (
	"context"
	"fmt"
	"math"
)

// maxReserve bounds one Reserve call so a typo cannot burn a huge range or exhaust memory.
const maxReserve = 1 << 20

// Reserve issues n contiguous numbers with a single store round trip (one Incr of n) and returns
// them in ascending order. It is the efficient way to import or pre-number a batch. n must be in
// 1..1,048,576, otherwise it returns ErrInvalidConfig.
//
// All numbers share one issue time, so they fall in one period; WithScope, At, Var and Vars
// apply as for Next. The counter advances before the numbers are rendered, so if rendering fails
// (a custom Formatter error) the whole range is consumed; on a transaction-bound store a rollback
// gives it back, on a pool-bound store it is a gap. Missing {var:} values of a *Format are caught
// before the counter moves. With WithMax, a range that would pass the maximum returns an
// *ExhaustedError and issues nothing, but the range is consumed like any failed call.
func (s *Series) Reserve(ctx context.Context, n int, opts ...CallOption) ([]Number, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: store must not be nil", ErrInvalidConfig)
	}
	if n < 1 || n > maxReserve {
		return nil, fmt.Errorf("%w: reserve count must be 1..%d, got %d", ErrInvalidConfig, maxReserve, n)
	}
	cfg := newCallConfig(opts)
	key, period, at, err := s.resolve(cfg)
	if err != nil {
		return nil, err
	}
	if f, ok := s.format.(*Format); ok {
		if err := f.checkVars(cfg.vars); err != nil {
			return nil, err
		}
	}

	end, err := s.store.Incr(ctx, key, period, int64(n))
	if err != nil {
		return nil, err
	}
	first := end - int64(n) + 1 // raw count of the first reserved number
	if first < 1 || end-1 > math.MaxInt64-s.start {
		return nil, &ExhaustedError{Key: key, Period: period, Max: math.MaxInt64, Seq: end}
	}

	firstSeq := s.start + (first - 1)
	lastSeq := firstSeq + int64(n) - 1
	if err := s.checkMax(key, period, lastSeq); err != nil {
		return nil, err
	}

	out := make([]Number, 0, n)
	for i := range n {
		seq := firstSeq + int64(i)
		value, err := s.format.Format(Parts{Key: key, Period: period, Seq: seq, At: at, Vars: cfg.vars})
		if err != nil {
			return nil, err
		}
		out = append(out, Number{Value: value, Seq: seq, Period: period, Key: key})
	}
	s.notify(ctx, key, period, firstSeq, lastSeq)
	return out, nil
}
