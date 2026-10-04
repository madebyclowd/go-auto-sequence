package sequence

import "context"

// Store persists counters. Implementations: sqlstore, memstore, or your own; the
// storetest package is the executable form of this contract.
//
// A Store must be safe for concurrent use. When it is bound to a caller's transaction
// the library never commits or rolls back that transaction.
type Store interface {
	// Incr atomically adds by (> 0) to the counter for (k, period) and returns the new
	// raw count; the first call for a partition returns by. It returns the end of the
	// reserved range [new-by+1, new]. Distinct keys, scopes and periods never share a
	// counter. A cancelled ctx returns ctx.Err() (matchable with errors.Is) without
	// advancing the counter. by <= 0 is an error.
	Incr(ctx context.Context, k Key, period string, by int64) (int64, error)
}

// Resetter is an optional Store capability, found by type assertion (like io.WriterTo). A store
// that implements it supports Series.Current and Series.Reset; one that does not makes those
// methods return an error matching errors.ErrUnsupported. memstore and sqlstore implement it.
//
// Values are raw counts: the number of increments applied, not the visible sequence number.
type Resetter interface {
	// Current returns the raw count for (k, period). ok is false when the partition has no
	// counter yet.
	Current(ctx context.Context, k Key, period string) (raw int64, ok bool, err error)

	// Set makes the raw count of (k, period) exactly raw (>= 0), creating the counter if needed.
	// The next Incr(by) then returns raw+by. A cancelled ctx returns ctx.Err() without changing
	// the counter; raw < 0 is an error.
	Set(ctx context.Context, k Key, period string, raw int64) error
}
