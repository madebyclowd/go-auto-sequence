package sqlstore

import "fmt"

// Dialect selects the SQL flavour. It is chosen explicitly and never sniffed from the
// connection.
type Dialect int

// Supported dialects: PostgreSQL, MySQL 8+ and SQLite 3.35+.
const (
	Postgres Dialect = iota + 1
	MySQL
	SQLite
)

func (d Dialect) String() string {
	switch d {
	case Postgres:
		return "postgres"
	case MySQL:
		return "mysql"
	case SQLite:
		return "sqlite"
	}
	return fmt.Sprintf("Dialect(%d)", int(d))
}

func (d Dialect) valid() bool { return d == Postgres || d == MySQL || d == SQLite }

// returning reports whether the increment statement returns the new value as a row (Postgres and
// SQLite RETURNING). MySQL has no RETURNING for an upsert, so its statement sets LAST_INSERT_ID and
// the value is read from the result instead.
func (d Dialect) returning() bool { return d != MySQL }

// incrSQL returns the single atomic statement that adds by to a counter and yields the new value.
// table is a validated identifier. Every dialect relies on the same property: one upsert statement
// takes the row lock, so concurrent callers queue on the row and each sees its own value.
func (d Dialect) incrSQL(table string) string {
	switch d {
	case MySQL:
		// LAST_INSERT_ID(expr) in both branches makes LastInsertId() the new counter whether the row
		// was inserted or updated, so RowsAffected (and the clientFoundRows driver flag) never
		// matters. VALUES() is avoided on purpose: it is deprecated in MySQL 8.
		return "INSERT INTO " + table + " (name, scope, period, counter)\n" +
			"VALUES (?, ?, ?, LAST_INSERT_ID(?))\n" +
			"ON DUPLICATE KEY UPDATE counter = LAST_INSERT_ID(counter + ?), updated_at = CURRENT_TIMESTAMP"
	case SQLite:
		return "INSERT INTO " + table + " AS s (name, scope, period, counter)\n" +
			"VALUES (?, ?, ?, ?)\n" +
			"ON CONFLICT (name, scope, period)\n" +
			"DO UPDATE SET counter = s.counter + excluded.counter, updated_at = CURRENT_TIMESTAMP\n" +
			"RETURNING counter"
	}
	// Postgres. The alias keeps schema-qualified tables valid.
	return "INSERT INTO " + table + " AS s (name, scope, period, counter, updated_at)\n" +
		"VALUES ($1, $2, $3, $4, now())\n" +
		"ON CONFLICT (name, scope, period)\n" +
		"DO UPDATE SET counter = s.counter + EXCLUDED.counter, updated_at = now()\n" +
		"RETURNING counter"
}

// incrArgs returns the bound arguments of incrSQL. MySQL binds the increment twice (insert branch
// and update branch).
func (d Dialect) incrArgs(name, scope, period string, by int64) []any {
	if d == MySQL {
		return []any{name, scope, period, by, by}
	}
	return []any{name, scope, period, by}
}

func (d Dialect) currentSQL(table string) string {
	if d == Postgres {
		return "SELECT counter FROM " + table + " WHERE name = $1 AND scope = $2 AND period = $3"
	}
	return "SELECT counter FROM " + table + " WHERE name = ? AND scope = ? AND period = ?"
}

// setSQL writes an absolute counter value; like incrSQL it is one atomic statement, so a
// concurrent increment and a reset serialize on the row.
func (d Dialect) setSQL(table string) string {
	switch d {
	case MySQL:
		return "INSERT INTO " + table + " (name, scope, period, counter)\n" +
			"VALUES (?, ?, ?, ?)\n" +
			"ON DUPLICATE KEY UPDATE counter = ?, updated_at = CURRENT_TIMESTAMP"
	case SQLite:
		return "INSERT INTO " + table + " (name, scope, period, counter)\n" +
			"VALUES (?, ?, ?, ?)\n" +
			"ON CONFLICT (name, scope, period)\n" +
			"DO UPDATE SET counter = excluded.counter, updated_at = CURRENT_TIMESTAMP"
	}
	return "INSERT INTO " + table + " (name, scope, period, counter, updated_at)\n" +
		"VALUES ($1, $2, $3, $4, now())\n" +
		"ON CONFLICT (name, scope, period)\n" +
		"DO UPDATE SET counter = EXCLUDED.counter, updated_at = now()"
}

func (d Dialect) setArgs(name, scope, period string, raw int64) []any {
	if d == MySQL {
		return []any{name, scope, period, raw, raw}
	}
	return []any{name, scope, period, raw}
}
