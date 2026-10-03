package sequence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type nopStore struct{}

func (nopStore) Incr(context.Context, Key, string, int64) (int64, error) { return 1, nil }

func TestKeyValidate(t *testing.T) {
	cases := []struct {
		name string
		key  Key
		ok   bool
	}{
		{"ok", Key{"invoice", "tenant-1"}, true},
		{"global", Key{Name: "invoice"}, true},
		{"empty name", Key{}, false},
		{"name 128", Key{Name: strings.Repeat("a", 128)}, true},
		{"name 129", Key{Name: strings.Repeat("a", 129)}, false},
		{"name multibyte over", Key{Name: strings.Repeat("é", 65)}, false},
		{"name bad utf8", Key{Name: "a\xffb"}, false},
		{"scope 128", Key{"a", strings.Repeat("s", 128)}, true},
		{"scope 129", Key{"a", strings.Repeat("s", 129)}, false},
		{"scope bad utf8", Key{"a", "\xff"}, false},
	}
	for _, c := range cases {
		err := c.key.validate()
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v does not wrap ErrInvalidConfig", c.name, err)
		}
	}
}

func TestKeyStringInjective(t *testing.T) {
	parts := []string{"", "a", "b", "a/b", "a%2Fb", "%", "/", "%25", "a/", "/b", "%2F", "é"}
	seen := map[string]Key{}
	for _, n := range parts {
		for _, s := range parts {
			k := Key{n, s}
			str := k.String()
			if prev, dup := seen[str]; dup && prev != k {
				t.Fatalf("collision: %#v and %#v both give %q", prev, k, str)
			}
			seen[str] = k
		}
	}
}

func TestErrors(t *testing.T) {
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

func TestNumber(t *testing.T) {
	_ = map[Number]struct{}{} // Number must stay comparable
	if got := (Number{Value: "INV-1"}).String(); got != "INV-1" {
		t.Fatalf("String() = %q", got)
	}
}

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
