package sequence

import (
	"errors"
	"strings"
	"testing"
)

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
