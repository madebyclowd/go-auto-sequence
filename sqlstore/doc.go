// Package sqlstore is a sequence.Store backed by database/sql.
//
// It imports no driver: pass any *sql.DB, *sql.Tx or *sql.Conn (or anything with the same two
// methods, such as a GORM ConnPool). The database's atomic upsert is the lock, so counters are
// correct across processes and instances. Bind a store to your own transaction with WithTx to
// make numbers gapless: the library never begins, commits or rolls back that transaction.
//
// Bound lock waits at the server, not in the caller's transaction: set lock_timeout on the
// connection or role (for example the lock_timeout runtime parameter, or ALTER ROLE ... SET
// lock_timeout), and keep a context deadline as the outer bound. A lock wait then fails with
// SQLSTATE 55P03, which Incr reports as sequence.ErrLockTimeout so callers can tell contention
// from slowness and retry. A context deadline that expires first is reported as the context
// error itself (context.DeadlineExceeded), never relabelled.
//
// The library does not run migrations. Migrations returns the SQL as an fs.FS for goose,
// golang-migrate, atlas or copy-paste, and Schema renders it for a custom table name.
package sqlstore
