package sequence

import (
	"maps"
	"time"
)

// CallOption adjusts a single call such as Next. An option that does not apply to the
// method it is passed to is ignored, as with grpc.CallOption.
type CallOption func(*callConfig)

type callConfig struct {
	scope string
	vars  map[string]string
	at    time.Time
	atSet bool
}

// WithScope selects the counter partition for this call, typically a tenant ID. An empty
// scope is the global counter. An invalid scope (over 128 bytes or not valid UTF-8) makes
// the call return ErrInvalidConfig.
func WithScope(scope string) CallOption { return func(c *callConfig) { c.scope = scope } }

// Var supplies the value of one {var:key} template token for this call.
func Var(key, value string) CallOption {
	return func(c *callConfig) {
		if c.vars == nil {
			c.vars = make(map[string]string)
		}
		c.vars[key] = value
	}
}

// Vars supplies several {var:key} values at once. The map is copied, so the caller may
// reuse or modify it afterwards.
func Vars(m map[string]string) CallOption {
	m = maps.Clone(m)
	return func(c *callConfig) {
		if c.vars == nil {
			c.vars = make(map[string]string, len(m))
		}
		for k, v := range m {
			c.vars[k] = v
		}
	}
}

// At overrides the issue time, for imports and back-dating. It selects the period and
// feeds the date tokens. A back-dated call increments the old period's counter, so numbers
// can be issued out of chronological order. The zero time makes the call return
// ErrInvalidConfig.
func At(t time.Time) CallOption {
	return func(c *callConfig) { c.at, c.atSet = t, true }
}

func newCallConfig(opts []CallOption) callConfig {
	var c callConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}
