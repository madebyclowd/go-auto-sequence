package sequence

import (
	"errors"
	"fmt"
	"testing"
)

func TestExhaustedError(t *testing.T) {
	var err error = &ExhaustedError{Key: Key{Name: "x"}, Max: 9, Seq: 10}
	wrapped := fmt.Errorf("next: %w", err)
	if !errors.Is(wrapped, ErrExhausted) {
		t.Fatal("ExhaustedError must match ErrExhausted")
	}
	var ee *ExhaustedError
	if !errors.As(wrapped, &ee) || ee.Max != 9 {
		t.Fatal("errors.As must extract ExhaustedError")
	}
	if errors.Is(ErrLockTimeout, ErrExhausted) {
		t.Fatal("unrelated sentinels must not match")
	}
}
