package sequence

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 3, 7, 9, 5, 4, 0, time.UTC)

func render(t *testing.T, tmpl string, p Parts) string {
	t.Helper()
	f, err := ParseFormat(tmpl)
	if err != nil {
		t.Fatalf("ParseFormat(%q): %v", tmpl, err)
	}
	got, err := f.Format(p)
	if err != nil {
		t.Fatalf("Format(%q): %v", tmpl, err)
	}
	return got
}

func TestFormatTokens(t *testing.T) {
	p := Parts{Key: Key{"invoice", "tenant-1"}, Period: "2026-03", Seq: 42, At: at, Vars: map[string]string{"branch": "JKT"}}
	cases := []struct{ tmpl, want string }{
		{"{seq}", "42"},
		{"{seq:5}", "00042"},
		{"{name}/{scope}/{period}/{seq}", "invoice/tenant-1/2026-03/42"},
		{"{YYYY}|{YY}|{MM}|{M}|{DD}|{D}|{HH}|{mm}|{ss}-{seq}", "2026|26|03|3|07|7|09|05|04-42"},
		{"{var:branch}-{seq:3}", "JKT-042"},
		{"{{INV}}-{seq}", "{INV}-42"},
		{"INV-{YYYY}-{seq:5}", "INV-2026-00042"},
	}
	for _, c := range cases {
		if got := render(t, c.tmpl, p); got != c.want {
			t.Errorf("%q: got %q, want %q", c.tmpl, got, c.want)
		}
	}
	if got := render(t, "{seq:5}", Parts{Seq: 123456}); got != "123456" {
		t.Errorf("padding must never truncate, got %q", got)
	}
}

func TestFormatLuhn(t *testing.T) {
	// Digits from literals, dates, names and the sequence are all included.
	got := render(t, "{name}{YYYY}-{seq}{check:luhn}", Parts{Key: Key{Name: "A7"}, Seq: 5, At: at})
	if !ValidLuhn(got) {
		t.Fatalf("%q does not validate", got)
	}
	want := "A72026-5" + string(luhnCheckDigit("72026"+"5"))
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := render(t, "{seq}{check:luhn}", Parts{Seq: 7992739871}); got != "79927398713" {
		t.Fatalf("textbook vector: got %q", got)
	}
}

func TestParseFormatErrors(t *testing.T) {
	bad := map[string]string{
		"unknown token":       "{seq}{bogus}",
		"empty token":         "{}{seq}",
		"unterminated":        "{seq",
		"lone close":          "{seq}}x}",
		"no seq":              "INV-{YYYY}",
		"seq width zero":      "{seq:0}",
		"seq width too big":   "{seq:33}",
		"seq width not int":   "{seq:x}",
		"var without key":     "{var}{seq}",
		"var bad key":         "{var:1a}{seq}",
		"var empty key":       "{var:}{seq}",
		"check wrong arg":     "{seq}{check:mod10}",
		"check not last":      "{seq}{check:luhn}-X",
		"check then token":    "{seq}{check:luhn}{YYYY}",
		"date layout dropped": "{date:2006}{seq}",
		"name with arg":       "{name:x}{seq}",
		"date with arg":       "{YYYY:x}{seq}",
		"empty template":      "",
	}
	for name, tmpl := range bad {
		_, err := ParseFormat(tmpl)
		if !errors.Is(err, ErrInvalidFormat) {
			t.Errorf("%s (%q): err = %v, want ErrInvalidFormat", name, tmpl, err)
		}
	}
	_, err := ParseFormat("ab{bogus}")
	if err == nil || !strings.Contains(err.Error(), "offset 2") {
		t.Errorf("error must carry the byte offset, got %v", err)
	}
}

func TestFormatMissingVar(t *testing.T) {
	f, err := ParseFormat("{var:branch}-{seq}")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Format(Parts{Seq: 1})
	if !errors.Is(err, ErrMissingVar) || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("err = %v", err)
	}
	// Extra, unused variables are ignored.
	if _, err := f.Format(Parts{Seq: 1, Vars: map[string]string{"branch": "X", "other": "Y"}}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatStringAndAdapter(t *testing.T) {
	f, _ := ParseFormat("X-{seq}")
	if f.String() != "X-{seq}" {
		t.Fatal(f.String())
	}
	var fm Formatter = f
	_ = fm
	fm = FormatFunc(func(p Parts) (string, error) { return "custom", nil })
	if got, _ := fm.Format(Parts{}); got != "custom" {
		t.Fatal(got)
	}
}

func TestFormatConcurrentUse(t *testing.T) {
	f, err := ParseFormat("{name}-{YYYY}-{seq:5}{check:luhn}")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				seq := int64(g*1000 + i)
				got, err := f.Format(Parts{Key: Key{Name: "n"}, Seq: seq, At: at})
				if err != nil || !ValidLuhn(got) {
					t.Errorf("seq %d: %q, %v", seq, got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestFormatAllocations(t *testing.T) {
	f, _ := ParseFormat("INV-{YYYY}-{seq:5}")
	p := Parts{Seq: 42, At: at}
	if n := testing.AllocsPerRun(100, func() { _, _ = f.Format(p) }); n > 1 {
		t.Fatalf("%v allocations per Format, want at most 1 (the result string)", n)
	}
}

func FuzzParseFormat(f *testing.F) {
	for _, s := range []string{"{seq}", "INV-{YYYY}-{seq:5}{check:luhn}", "{{x}}{seq}", "{var:a}{seq}", "{", "}", "{seq:", "\xff{seq}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, tmpl string) {
		fm, err := ParseFormat(tmpl)
		if err != nil {
			if !errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("error does not wrap ErrInvalidFormat: %v", err)
			}
			return
		}
		vars := map[string]string{}
		for _, s := range fm.segs {
			if s.kind == segVar {
				vars[s.text] = "v"
			}
		}
		const seq = 9876543210
		got, err := fm.Format(Parts{Key: Key{"n", "s"}, Period: "p", Seq: seq, At: at, Vars: vars})
		if err != nil {
			t.Fatalf("Format failed after a successful parse: %v", err)
		}
		if !strings.Contains(got, "9876543210") {
			t.Fatalf("output %q lacks the sequence digits", got)
		}
	})
}

func BenchmarkFormat(b *testing.B) {
	f, _ := ParseFormat("INV-{YYYY}-{seq:5}{check:luhn}")
	p := Parts{Key: Key{Name: "invoice"}, Seq: 42, At: at}
	b.ReportAllocs()
	for range b.N {
		_, _ = f.Format(p)
	}
}
