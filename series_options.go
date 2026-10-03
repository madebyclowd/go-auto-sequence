package sequence

// SeriesOption configures a Series.
type SeriesOption func(*seriesConfig)

type seriesConfig struct {
	format       Formatter
	formatSet    bool
	period       Period
	start        int64
	requireScope bool
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
