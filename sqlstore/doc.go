// Package sqlstore is a sequence.Store backed by database/sql.
//
// It imports no driver: pass any *sql.DB, *sql.Tx or *sql.Conn (or anything with the same two
// methods, such as a GORM ConnPool). The database's atomic upsert is the lock, so counters are
// correct across processes and instances. Bind a store to your own transaction with WithTx to
// make numbers gapless: the library never begins, commits or rolls back that transaction.
//
// The library does not run migrations. Migrations returns the SQL as an fs.FS for goose,
// golang-migrate, atlas or copy-paste, and Schema renders it for a custom table name.
package sqlstore
