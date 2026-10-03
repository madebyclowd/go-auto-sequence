package sequence

import (
	"context"
	"errors"
	"testing"
	"time"
)

type nopStore struct{}

func (nopStore) Incr(context.Context, Key, string, int64) (int64, error) { return 1, nil }

func TestNew(t *testing.T) {
	if _, err := New(nopStore{}); err != nil {
		t.Fatal(err)
	}
	bad := map[string][]Option{
		"nil clock":    {WithClock(nil)},
		"nil location": {WithLocation(nil)},
		"nil logger":   {WithLogger(nil)},
	}
	for name, opts := range bad {
		if _, err := New(nopStore{}, opts...); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := New(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("nil store: err = %v", err)
	}
	s, err := New(nopStore{}, WithClock(func() time.Time { return time.Unix(0, 0) }))
	if err != nil || s.clock().Unix() != 0 || s.loc != time.UTC {
		t.Fatalf("options not applied: %v", err)
	}
	s.logger.Info("discarded") // default logger must not panic
}
