package sqlstore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeDriver is an in-memory database/sql driver that understands the one upsert statement
// sqlstore sends: it keeps counters by (name, scope, period) and records every query. It lets
// the adapter (argument order, scanning, error mapping, tx detection) be tested without a
// real database; the SQL itself is tested against Postgres in internal/integration.
type fakeDriver struct {
	mu       sync.Mutex
	counters map[[3]string]int64
	queries  []string
	args     [][]driver.Value
	fail     error                           // returned by every query when set
	hook     func(ctx context.Context) error // runs inside a query, may return an error
}

var fakeSeq atomic.Int64

func newFakeDB(t *testing.T) (*sql.DB, *fakeDriver) {
	t.Helper()
	d := &fakeDriver{counters: map[[3]string]int64{}}
	name := fmt.Sprintf("fake-%d", fakeSeq.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, d
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d}, nil }

type fakeConn struct{ d *fakeDriver }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unsupported") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

func (c *fakeConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	c.d.queries, c.d.args = append(c.d.queries, q), append(c.d.args, vals)
	if c.d.hook != nil {
		if err := c.d.hook(ctx); err != nil {
			return nil, err
		}
	}
	if c.d.fail != nil {
		return nil, c.d.fail
	}
	key := [3]string{vals[0].(string), vals[1].(string), vals[2].(string)}
	c.d.counters[key] += vals[3].(int64)
	return &oneRow{v: c.d.counters[key]}, nil
}

type oneRow struct {
	v    int64
	done bool
}

func (r *oneRow) Columns() []string { return []string{"counter"} }
func (r *oneRow) Close() error      { return nil }
func (r *oneRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.v
	return nil
}

// sqlStateErr mimics pgx's *pgconn.PgError and lib/pq's *pq.Error.
type sqlStateErr struct{ code string }

func (e sqlStateErr) Error() string    { return "fake error " + e.code }
func (e sqlStateErr) SQLState() string { return e.code }
