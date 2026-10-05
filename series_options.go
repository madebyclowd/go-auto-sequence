package sequence

// SeriesOption configures a Series.
type SeriesOption func(*seriesConfig)

type seriesConfig struct {
	format       Formatter
	formatSet    bool
	period       Period
	start        int64
	requireScope bool
	max          int64
	thresholdPct int
	maxSet       bool
}

// WithFormat sets how numbers are rendered. Default: the template "{seq}".
func WithFormat(f Formatter) SeriesOption {
	return func(c *seriesConfig) { c.format, c.formatSet = f, true }
}

// WithPeriod sets when the counter resets. Default: Never.
func WithPeriod(p Period) SeriesOption { return func(c *seriesConfig) { c.period = p } }

// WithStart sets the first visible sequence value. Default: 1. It must be >= 0.
func WithStart(n int64) SeriesOption { return func(c *seriesConfig) { c.start = n } }

// RequireScope makes a call without a non-empty WithScope return ErrScopeRequired, so a
// forgotten scope can never silently use the global counter.
func RequireScope() SeriesOption { return func(c *seriesConfig) { c.requireScope = true } }

// WithMax caps the series at maxVal (a visible sequence value) and sets the exhaustion warning
// threshold as a percentage (1..100) of the numbers between the start value and maxVal. A call
// that would issue a value above maxVal returns an *ExhaustedError (matching ErrExhausted). The
// exhaustion handler, if any, is notified when an issued number crosses the threshold. The
// threshold is rounded up, so the warning fires at or after the stated percentage, never before:
// with a start of 1, a maximum of 10 and 85 percent it fires at 9, not 8.
// maxVal must be >= the start value.
func WithMax(maxVal int64, thresholdPercent int) SeriesOption {
	return func(c *seriesConfig) { c.max, c.thresholdPct, c.maxSet = maxVal, thresholdPercent, true }
}
