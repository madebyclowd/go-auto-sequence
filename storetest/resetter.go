package storetest

import (
	"context"
	"errors"
	"fmt"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

// resetterChecks run only for stores that implement sequence.Resetter.
var resetterChecks = []struct {
	name string
	run  func(ctx context.Context, r sequence.Resetter, s sequence.Store) error
}{
	{"CurrentOfUnknownPartition", checkCurrentUnknown},
	{"SetThenCurrentRoundTrips", checkSetCurrent},
	{"SetThenIncrContinuesFromValue", checkSetThenIncr},
	{"SetZero", checkSetZero},
	{"SetRejectsNegative", checkSetNegative},
	{"SetIsolation", checkSetIsolation},
	{"SetContext", checkSetContext},
}

func checkCurrentUnknown(ctx context.Context, r sequence.Resetter, _ sequence.Store) error {
	raw, ok, err := r.Current(ctx, sequence.Key{Name: names{}.next()}, "p")
	if err != nil {
		return err
	}
	if ok || raw != 0 {
		return fmt.Errorf("Current of an unknown partition = (%d, %v), want (0, false)", raw, ok)
	}
	return nil
}

func checkSetCurrent(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	k := sequence.Key{Name: names{}.next(), Scope: "s"}
	if _, err := s.Incr(ctx, k, "p", 3); err != nil {
		return err
	}
	for _, want := range []int64{10, 7} { // overwrite upwards and downwards
		if err := r.Set(ctx, k, "p", want); err != nil {
			return err
		}
		raw, ok, err := r.Current(ctx, k, "p")
		if err != nil {
			return err
		}
		if !ok || raw != want {
			return fmt.Errorf("after Set(%d) Current = (%d, %v)", want, raw, ok)
		}
	}
	return nil
}

func checkSetThenIncr(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	k := sequence.Key{Name: names{}.next()}
	if err := r.Set(ctx, k, "", 41); err != nil { // Set creates the counter when it is missing
		return err
	}
	got, err := s.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	if err := expect("Incr after Set(41)", got, 42); err != nil {
		return err
	}
	raw, _, err := r.Current(ctx, k, "")
	if err != nil {
		return err
	}
	return expect("Current after Incr", raw, 42)
}

func checkSetZero(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	k := sequence.Key{Name: names{}.next()}
	if _, err := s.Incr(ctx, k, "", 5); err != nil {
		return err
	}
	if err := r.Set(ctx, k, "", 0); err != nil {
		return err
	}
	got, err := s.Incr(ctx, k, "", 1)
	if err != nil {
		return err
	}
	return expect("Incr after Set(0)", got, 1)
}

func checkSetNegative(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	k := sequence.Key{Name: names{}.next()}
	if _, err := s.Incr(ctx, k, "", 4); err != nil {
		return err
	}
	if err := r.Set(ctx, k, "", -1); err == nil {
		return errors.New("Set(-1) must return an error")
	}
	raw, _, err := r.Current(ctx, k, "")
	if err != nil {
		return err
	}
	return expect("Current after a rejected Set (counter must not move)", raw, 4)
}

func checkSetIsolation(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	n := names{}.next()
	base := sequence.Key{Name: n, Scope: "a"}
	other := []struct {
		label  string
		k      sequence.Key
		period string
	}{
		{"other scope", sequence.Key{Name: n, Scope: "b"}, "p"},
		{"other period", base, "q"},
		{"other name", sequence.Key{Name: n + "x", Scope: "a"}, "p"},
	}
	for _, o := range other {
		if _, err := s.Incr(ctx, o.k, o.period, 2); err != nil {
			return err
		}
	}
	if err := r.Set(ctx, base, "p", 99); err != nil {
		return err
	}
	if raw, ok, err := r.Current(ctx, base, "p"); err != nil || !ok || raw != 99 {
		return fmt.Errorf("Set must be readable from the same partition: Current = (%d, %v, %v), want (99, true, nil)", raw, ok, err)
	}
	for _, o := range other {
		raw, _, err := r.Current(ctx, o.k, o.period)
		if err != nil {
			return err
		}
		if err := expect("Set must not touch "+o.label, raw, 2); err != nil {
			return err
		}
	}
	return nil
}

func checkSetContext(ctx context.Context, r sequence.Resetter, s sequence.Store) error {
	k := sequence.Key{Name: names{}.next()}
	if _, err := s.Incr(ctx, k, "", 6); err != nil {
		return err
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := r.Set(cancelled, k, "", 1); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("Set with a cancelled ctx: err = %v, want errors.Is(err, context.Canceled)", err)
	}
	if _, _, err := r.Current(cancelled, k, ""); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("Current with a cancelled ctx: err = %v, want errors.Is(err, context.Canceled)", err)
	}
	raw, _, err := r.Current(ctx, k, "")
	if err != nil {
		return err
	}
	return expect("Current after a cancelled Set (counter must not move)", raw, 6)
}
