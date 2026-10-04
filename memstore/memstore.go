// Package memstore is an in-process sequence.Store for tests and single-process use.
//
// It is not durable and not safe across processes: counters vanish on restart and are not
// shared between instances. Use sqlstore for anything that must survive or scale out.
package memstore

import (
	"context"
	"fmt"
	"math"
	"sync"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

type entry struct {
	key    sequence.Key
	period string
}

// Store is a mutex-guarded in-memory counter set. The zero value is not usable; call New.
type Store struct {
	mu sync.Mutex
	m  map[entry]int64
}

var (
	_ sequence.Store    = (*Store)(nil)
	_ sequence.Resetter = (*Store)(nil)
)

// New returns an empty Store.
func New() *Store { return &Store{m: make(map[entry]int64)} }

// Incr implements sequence.Store.
func (s *Store) Incr(ctx context.Context, k sequence.Key, period string, by int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if by <= 0 {
		return 0, fmt.Errorf("%w: increment must be > 0, got %d", sequence.ErrInvalidConfig, by)
	}
	e := entry{k, period}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.m[e]
	if cur > math.MaxInt64-by {
		return 0, &sequence.ExhaustedError{Key: k, Period: period, Max: math.MaxInt64, Seq: cur}
	}
	cur += by
	s.m[e] = cur
	return cur, nil
}

// Current implements sequence.Resetter.
func (s *Store) Current(ctx context.Context, k sequence.Key, period string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[entry{k, period}]
	return v, ok, nil
}

// Set implements sequence.Resetter.
func (s *Store) Set(ctx context.Context, k sequence.Key, period string, raw int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if raw < 0 {
		return fmt.Errorf("%w: raw count must be >= 0, got %d", sequence.ErrInvalidConfig, raw)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[entry{k, period}] = raw
	return nil
}

// Peek returns the current raw count for (k, period), or 0 if it was never incremented.
// It is a testing aid and not part of sequence.Store.
func (s *Store) Peek(k sequence.Key, period string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[entry{k, period}]
}
