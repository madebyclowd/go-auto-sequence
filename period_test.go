package sequence

import (
	"fmt"
	"testing"
	"time"
	_ "time/tzdata" // zones below must resolve on every CI platform
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func TestPeriodBuiltins(t *testing.T) {
	cases := []struct {
		name string
		p    Period
		at   time.Time
		want string
	}{
		{"yearly", Yearly, date(2026, 10, 3), "2026"},
		{"monthly", Monthly, date(2026, 10, 3), "2026-10"},
		{"monthly leap day", Monthly, date(2028, 2, 29), "2028-02"},
		{"monthly jan", Monthly, date(2026, 1, 1), "2026-01"},
		{"daily", Daily, date(2026, 10, 3), "2026-10-03"},
		{"quarter end q1", Quarterly, date(2026, 3, 31), "2026-Q1"},
		{"quarter start q2", Quarterly, date(2026, 4, 1), "2026-Q2"},
		{"quarter end q4", Quarterly, date(2026, 12, 31), "2026-Q4"},
		{"week 2026-12-31", Weekly, date(2026, 12, 31), "2026-W53"},
		{"week 2027-01-01", Weekly, date(2027, 1, 1), "2026-W53"},
		{"week 2027-01-03", Weekly, date(2027, 1, 3), "2026-W53"},
		{"week 2027-01-04", Weekly, date(2027, 1, 4), "2027-W01"},
		{"week 2025-12-29", Weekly, date(2025, 12, 29), "2026-W01"},
		{"week 2024-12-30", Weekly, date(2024, 12, 30), "2025-W01"},
		{"never", Never, date(2026, 10, 3), ""},
		{"never zero time", Never, time.Time{}, ""},
	}
	for _, c := range cases {
		if got := c.p(c.at); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPeriodLocationMatters(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC)
	if got := Yearly(instant); got != "2026" {
		t.Errorf("UTC: %q", got)
	}
	if got := Yearly(instant.In(jakarta)); got != "2027" {
		t.Errorf("Jakarta: %q", got)
	}
}

// Sao Paulo skipped midnight on DST start days (2018-11-04: 00:00 became 01:00). Walking by
// absolute hours must visit every calendar day exactly once, in order.
func TestDailyAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for at := time.Date(2018, 11, 2, 0, 0, 0, 0, loc); at.Before(time.Date(2018, 11, 7, 0, 0, 0, 0, loc)); at = at.Add(time.Hour) {
		k := Daily(at)
		if len(keys) == 0 || keys[len(keys)-1] != k {
			keys = append(keys, k)
		}
	}
	want := []string{"2018-11-02", "2018-11-03", "2018-11-04", "2018-11-05", "2018-11-06"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
}

func TestPeriodsSortChronologically(t *testing.T) {
	for name, p := range map[string]Period{"yearly": Yearly, "quarterly": Quarterly, "monthly": Monthly, "weekly": Weekly, "daily": Daily} {
		prev := ""
		for at := date(2024, 1, 1); at.Year() < 2028; at = at.AddDate(0, 0, 1) {
			k := p(at)
			if k < prev {
				t.Fatalf("%s: %q sorts before earlier key %q at %s", name, k, prev, at.Format(time.DateOnly))
			}
			prev = k
		}
	}
}

func TestPeriodKeysFitLengthGuard(t *testing.T) {
	for _, p := range []Period{Yearly, Quarterly, Monthly, Weekly, Daily} {
		if got := p(date(9999, 12, 31)); len(got) > maxPeriodLen {
			t.Errorf("%q longer than %d", got, maxPeriodLen)
		}
	}
}

// ExamplePeriod shows a custom fiscal-year Period (year starts 1 April) next to a built-in.
func ExamplePeriod() {
	fiscalYear := func(t time.Time) string {
		y := t.Year()
		if t.Month() < time.April {
			y--
		}
		return fmt.Sprintf("FY%d", y)
	}
	var p Period = fiscalYear
	fmt.Println(p(date(2026, 3, 31)), p(date(2026, 4, 1)), Monthly(date(2026, 4, 1)))
	// Output: FY2025 FY2026 2026-04
}
