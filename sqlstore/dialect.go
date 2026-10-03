package sqlstore

import "fmt"

// Dialect selects the SQL flavour. It is chosen explicitly and never sniffed from the
// connection. Only Postgres exists so far.
type Dialect int

// Supported dialects.
const (
	Postgres Dialect = iota + 1
)

func (d Dialect) String() string {
	if d == Postgres {
		return "postgres"
	}
	return fmt.Sprintf("Dialect(%d)", int(d))
}

func (d Dialect) valid() bool { return d == Postgres }

// incrSQL returns the single atomic statement that adds $4 to a counter and returns the new
// value. table is a validated identifier.
func (d Dialect) incrSQL(table string) string {
	// Postgres: the upsert takes the row lock, so concurrent callers queue on the row and
	// each sees its own RETURNING value. The alias keeps schema-qualified tables valid.
	return "INSERT INTO " + table + " AS s (name, scope, period, counter, updated_at)\n" +
		"VALUES ($1, $2, $3, $4, now())\n" +
		"ON CONFLICT (name, scope, period)\n" +
		"DO UPDATE SET counter = s.counter + EXCLUDED.counter, updated_at = now()\n" +
		"RETURNING counter"
}

func (d Dialect) currentSQL(table string) string {
	return "SELECT counter FROM " + table + " WHERE name = $1 AND scope = $2 AND period = $3"
}

// setSQL writes an absolute counter value; like incrSQL it is one atomic statement, so a
// concurrent increment and a reset serialize on the row.
func (d Dialect) setSQL(table string) string {
	return "INSERT INTO " + table + " (name, scope, period, counter, updated_at)\n" +
		"VALUES ($1, $2, $3, $4, now())\n" +
		"ON CONFLICT (name, scope, period)\n" +
		"DO UPDATE SET counter = EXCLUDED.counter, updated_at = now()"
}
