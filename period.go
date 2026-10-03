package sequence

import (
	"fmt"
	"time"
)

// Period maps an issue time to the key of the counter partition it belongs to, for example
// "2026-10" for a monthly reset. The returned key is stored with the counter, so a Period
// must be deterministic and its output should sort lexicographically in time order.
// An empty key means the counter never resets.
//
// The built-ins format the time as given; the Sequencer converts it to its configured
// location first. A fiscal-year rule is just another func(time.Time) string.
type Period func(t time.Time) string

var (
	_ Period = Never
	_ Period = Yearly
	_ Period = Quarterly
	_ Period = Monthly
	_ Period = Weekly
	_ Period = Daily
)

// Never never resets the counter. Its key is "".
func Never(time.Time) string { return "" }

// Yearly resets every calendar year: "2026".
func Yearly(t time.Time) string { return t.Format("2006") }

// Quarterly resets every calendar quarter: "2026-Q4".
func Quarterly(t time.Time) string {
	return fmt.Sprintf("%04d-Q%d", t.Year(), (int(t.Month())+2)/3)
}

// Monthly resets every calendar month: "2026-10".
func Monthly(t time.Time) string { return t.Format("2006-01") }

// Weekly resets every ISO 8601 week: "2026-W40". The year is the ISO week-numbering year,
// which differs from the calendar year around New Year (2027-01-01 is "2026-W53").
func Weekly(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// Daily resets every calendar day: "2026-10-03".
func Daily(t time.Time) string { return t.Format("2006-01-02") }

// maxPeriodLen is the longest period key a Next call accepts; see ErrPeriodTooLong.
const maxPeriodLen = 32
