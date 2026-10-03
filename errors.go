package sequence

import (
	"errors"
	"fmt"
)

// Sentinel errors. Match them with errors.Is; they are part of the public API.
var (
	ErrExhausted     = errors.New("sequence: max value exceeded")
	ErrLockTimeout   = errors.New("sequence: lock wait timed out")
	ErrNoTransaction = errors.New("sequence: store not bound to a transaction")
	ErrScopeRequired = errors.New("sequence: scope required for this series")
	ErrPeriodTooLong = errors.New("sequence: period key too long")
	ErrInvalidConfig = errors.New("sequence: invalid configuration")
	ErrInvalidFormat = errors.New("sequence: invalid format template")
	ErrMissingVar    = errors.New("sequence: template variable not supplied")
)

// ExhaustedError carries the details of an exhausted counter. It matches ErrExhausted
// with errors.Is and can be extracted with errors.As.
type ExhaustedError struct {
	Key    Key
	Period string
	Max    int64
	Seq    int64
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("%s: %s period %q reached %d, max %d", ErrExhausted.Error(), e.Key, e.Period, e.Seq, e.Max)
}

// Is reports whether target is ErrExhausted.
func (e *ExhaustedError) Is(target error) bool { return target == ErrExhausted }
