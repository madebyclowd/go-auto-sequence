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
