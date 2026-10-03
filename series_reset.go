package sequence

import (
	"context"
	"errors"
	"fmt"
	"math"
)

func (s *Series) resetter() (Resetter, error) {
	if s.store == nil {
		return nil, fmt.Errorf("%w: store must not be nil", ErrInvalidConfig)
	}
	r, ok := s.store.(Resetter)
	if !ok {
		return nil, fmt.Errorf("sequence: store %T does not implement Resetter: %w", s.store, errors.ErrUnsupported)
	}
	return r, nil
}

// Current returns the last visible sequence value issued in the partition selected by the call
// options (WithScope, At), without issuing a number. issued is false when nothing has been
// issued there yet, so a real value of 0 is distinguishable from "none".
//
// The store must implement Resetter, otherwise the error matches errors.ErrUnsupported.
func (s *Series) Current(ctx context.Context, opts ...CallOption) (last int64, issued bool, err error) {
	r, err := s.resetter()
	if err != nil {
		return 0, false, err
	}
	key, period, _, err := s.resolve(newCallConfig(opts))
	if err != nil {
		return 0, false, err
	}
	raw, ok, err := r.Current(ctx, key, period)
	if err != nil {
		return 0, false, err
	}
	if !ok || raw < 1 {
		return 0, false, nil
	}
	if raw-1 > math.MaxInt64-s.start {
		return 0, false, &ExhaustedError{Key: key, Period: period, Max: math.MaxInt64, Seq: raw}
	}
	return s.start + (raw - 1), true, nil
}

// Reset makes the next number issued in the selected partition exactly next. It requires
// next >= the series start, otherwise it returns ErrInvalidConfig. Repairing drift after an
// import is Reset(ctx, highestIssued+1).
//
// Reset is a single atomic write on the store. Concurrent Next calls serialize against it at the
// row, so no two callers ever see the same number from one counter state, but a reset to a lower
// value deliberately makes already-issued numbers issuable again; quiesce writers first if that
// matters. With a transaction-bound store the reset is part of that transaction and a rollback
// undoes it. The store must implement Resetter, otherwise the error matches errors.ErrUnsupported.
func (s *Series) Reset(ctx context.Context, next int64, opts ...CallOption) error {
	r, err := s.resetter()
	if err != nil {
		return err
	}
	if next < s.start {
		return fmt.Errorf("%w: next (%d) must be >= start (%d)", ErrInvalidConfig, next, s.start)
	}
	key, period, _, err := s.resolve(newCallConfig(opts))
	if err != nil {
		return err
	}
	return r.Set(ctx, key, period, next-s.start)
}
