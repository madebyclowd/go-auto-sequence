package sqlstore_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/sqlstore"
	"github.com/madebyclowd/go-auto-sequence/storetest"
)

// The fake driver proves the adapter plumbing, not SQL semantics (see fakedriver_test.go).
func TestConformanceAgainstFakeDriver(t *testing.T) {
	for _, d := range []sqlstore.Dialect{sqlstore.Postgres, sqlstore.MySQL, sqlstore.SQLite} {
		t.Run(d.String(), func(t *testing.T) {
			storetest.Run(t, func(t *testing.T) sequence.Store {
				db, _ := newFakeDB(t)
				s, err := sqlstore.New(db, d)
				if err != nil {
					t.Fatal(err)
				}
				return s
			})
		})
	}
}

func TestStatementAndArguments(t *testing.T) {
	db, d := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres, sqlstore.WithTables("app.seqs"))
	got, err := s.Incr(context.Background(), sequence.Key{Name: "inv", Scope: "t1"}, "2026", 3)
	if err != nil || got != 3 {
		t.Fatalf("got %d, %v", got, err)
	}
	q := d.queries[0]
	for _, want := range []string{"INSERT INTO app.seqs AS s", "ON CONFLICT (name, scope, period)", "s.counter + EXCLUDED.counter", "RETURNING counter"} {
		if !strings.Contains(q, want) {
			t.Errorf("statement lacks %q:\n%s", want, q)
		}
	}
	a := d.args[0]
	if a[0] != "inv" || a[1] != "t1" || a[2] != "2026" || a[3] != int64(3) {
		t.Fatalf("args = %v", a)
	}
}

func TestInvalidIncrementNeverReachesDatabase(t *testing.T) {
	db, d := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres)
	for _, by := range []int64{0, -1} {
		if _, err := s.Incr(context.Background(), sequence.Key{Name: "x"}, "", by); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("by=%d: err = %v", by, err)
		}
	}
	if len(d.queries) != 0 {
		t.Fatal("no query expected")
	}
}

func TestNewValidation(t *testing.T) {
	db, _ := newFakeDB(t)
	for name, tbl := range map[string]string{
		"empty": "", "space": "a b", "semicolon": "x;DROP TABLE y", "quote": `a"b`,
		"leading digit": "1a", "three parts": "a.b.c", "trailing dot": "a.",
	} {
		if _, err := sqlstore.New(db, sqlstore.Postgres, sqlstore.WithTables(tbl)); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("%s (%q): err = %v", name, tbl, err)
		}
	}
	for _, tbl := range []string{"sequences", "_s1", "app.sequences", "App_1.Seq"} {
		if _, err := sqlstore.New(db, sqlstore.Postgres, sqlstore.WithTables(tbl)); err != nil {
			t.Errorf("%q: %v", tbl, err)
		}
	}
	if _, err := sqlstore.New(nil, sqlstore.Postgres); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Errorf("nil db: %v", err)
	}
	if _, err := sqlstore.New(db, sqlstore.Dialect(0)); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Errorf("zero dialect: %v", err)
	}
}

func TestRequireTx(t *testing.T) {
	db, _ := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres, sqlstore.RequireTx())
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	if _, err := s.Incr(ctx, k, "", 1); !errors.Is(err, sequence.ErrNoTransaction) {
		t.Fatalf("pool: err = %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := s.WithTx(conn).Incr(ctx, k, "", 1); !errors.Is(err, sequence.ErrNoTransaction) {
		t.Fatalf("*sql.Conn is not a transaction: err = %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	if got, err := s.WithTx(tx).Incr(ctx, k, "", 1); err != nil || got != 1 {
		t.Fatalf("tx: got %d, %v", got, err)
	}
	// Without RequireTx a pool store works.
	plain, _ := sqlstore.New(db, sqlstore.Postgres)
	if _, err := plain.Incr(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}
}

func TestWithTxIsImmutable(t *testing.T) {
	db, d := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres)
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback() //nolint:errcheck
	bound := s.WithTx(tx)
	if bound == s {
		t.Fatal("WithTx must return a copy")
	}
	if _, err := s.Incr(context.Background(), sequence.Key{Name: "x"}, "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Incr(context.Background(), sequence.Key{Name: "x"}, "", 1); err != nil {
		t.Fatal(err)
	}
	if len(d.queries) != 2 {
		t.Fatalf("queries = %d", len(d.queries))
	}
	if _, err := s.WithTx(nil).Incr(context.Background(), sequence.Key{Name: "x"}, "", 1); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("nil tx: err = %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	run := func(ctx context.Context, fail error) error {
		db, d := newFakeDB(t)
		d.fail = fail
		s, _ := sqlstore.New(db, sqlstore.Postgres)
		_, err := s.Incr(ctx, k, "p", 1)
		return err
	}

	if err := run(ctx, sqlStateErr{"55P03"}); !errors.Is(err, sequence.ErrLockTimeout) {
		t.Errorf("55P03: %v", err)
	}
	var ee *sequence.ExhaustedError
	if err := run(ctx, sqlStateErr{"22003"}); !errors.Is(err, sequence.ErrExhausted) || !errors.As(err, &ee) || ee.Key != k {
		t.Errorf("22003: %v", err)
	}
	other := sqlStateErr{"23505"}
	if err := run(ctx, other); errors.Is(err, sequence.ErrLockTimeout) || !errors.Is(err, other) {
		t.Errorf("other SQLSTATE must be wrapped, not swallowed: %v", err)
	}
	plain := errors.New("connection refused")
	if err := run(ctx, plain); !errors.Is(err, plain) {
		t.Errorf("plain error must be wrapped: %v", err)
	}
	// 57014 reported while the deadline expires mid-query: ErrLockTimeout and the context error.
	db, d := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres)
	d.fail = sqlStateErr{"57014"}
	if _, err := s.Incr(ctx, k, "p", 1); errors.Is(err, sequence.ErrLockTimeout) {
		t.Errorf("57014 with a live ctx is not a lock timeout: %v", err)
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	d.hook = func(c context.Context) error { <-c.Done(); return nil }
	_, err := s.Incr(short, k, "p", 1)
	if !errors.Is(err, sequence.ErrLockTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("57014 after a deadline: %v", err)
	}
	// A plain context error passes through unchanged.
	d.fail, d.hook = nil, nil
	cancelled, cancel2 := context.WithCancel(ctx)
	cancel2()
	if _, err := s.Incr(cancelled, k, "p", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestMigrationsAndSchema(t *testing.T) {
	files, err := fs.Glob(sqlstore.Migrations(sqlstore.Postgres), "*.sql")
	if err != nil || len(files) != 1 || files[0] != "0001_create_sequences.sql" {
		t.Fatalf("files = %v, %v", files, err)
	}
	b, _ := fs.ReadFile(sqlstore.Migrations(sqlstore.Postgres), files[0])
	for _, want := range []string{"CREATE TABLE IF NOT EXISTS sequences", "PRIMARY KEY (name, scope, period)", "counter    bigint"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("migration lacks %q", want)
		}
	}
	custom, err := sqlstore.Schema(sqlstore.Postgres, "app.my_seq")
	if err != nil || !strings.Contains(custom, "CREATE TABLE IF NOT EXISTS app.my_seq (") || strings.Contains(custom, "EXISTS sequences") {
		t.Fatalf("custom schema:\n%s\n%v", custom, err)
	}
	if def, _ := sqlstore.Schema(sqlstore.Postgres, "sequences"); def != string(b) {
		t.Error("Schema with the default name must equal the embedded migration")
	}
	if _, err := sqlstore.Schema(sqlstore.Postgres, "bad name;"); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Errorf("bad table: %v", err)
	}
	if sqlstore.Migrations(sqlstore.Dialect(0)) != nil {
		t.Error("unknown dialect must return nil")
	}
}

func TestCurrentAndSet(t *testing.T) {
	db, d := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres, sqlstore.WithTables("app.seqs"))
	ctx := context.Background()
	k := sequence.Key{Name: "inv", Scope: "t1"}

	if raw, ok, err := s.Current(ctx, k, "2026"); err != nil || ok || raw != 0 {
		t.Fatalf("empty: (%d, %v, %v)", raw, ok, err)
	}
	if err := s.Set(ctx, k, "2026", 41); err != nil {
		t.Fatal(err)
	}
	if raw, ok, err := s.Current(ctx, k, "2026"); err != nil || !ok || raw != 41 {
		t.Fatalf("after Set: (%d, %v, %v)", raw, ok, err)
	}
	if got, err := s.Incr(ctx, k, "2026", 1); err != nil || got != 42 {
		t.Fatalf("Incr after Set: %d, %v", got, err)
	}
	var sel, set string
	for _, q := range d.queries {
		switch {
		case strings.HasPrefix(q, "SELECT"):
			sel = q
		case strings.Contains(q, "counter = EXCLUDED.counter"):
			set = q
		}
	}
	if !strings.Contains(sel, "FROM app.seqs WHERE name = $1 AND scope = $2 AND period = $3") {
		t.Errorf("select statement: %s", sel)
	}
	if !strings.Contains(set, "INSERT INTO app.seqs") || !strings.Contains(set, "ON CONFLICT (name, scope, period)") {
		t.Errorf("set statement: %s", set)
	}
	if err := s.Set(ctx, k, "2026", -1); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Errorf("negative raw: %v", err)
	}
}

func TestSetHonoursRequireTxButCurrentDoesNot(t *testing.T) {
	db, _ := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres, sqlstore.RequireTx())
	ctx := context.Background()
	k := sequence.Key{Name: "x"}
	if err := s.Set(ctx, k, "", 1); !errors.Is(err, sequence.ErrNoTransaction) {
		t.Fatalf("Set on a pool: %v", err)
	}
	if _, _, err := s.Current(ctx, k, ""); err != nil {
		t.Fatalf("Current is a read and must work on a pool: %v", err)
	}
	tx, _ := db.BeginTx(ctx, nil)
	defer tx.Rollback() //nolint:errcheck
	if err := s.WithTx(tx).Set(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}
}

func TestSetAndCurrentContext(t *testing.T) {
	db, _ := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Set(cancelled, sequence.Key{Name: "x"}, "", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("Set: %v", err)
	}
	if _, _, err := s.Current(cancelled, sequence.Key{Name: "x"}, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("Current: %v", err)
	}
}

func TestInTx(t *testing.T) {
	db, _ := newFakeDB(t)
	s, _ := sqlstore.New(db, sqlstore.Postgres)
	if s.InTx() {
		t.Fatal("a pool-bound store is not in a transaction")
	}
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback() //nolint:errcheck
	if !s.WithTx(tx).InTx() || s.InTx() {
		t.Fatal("WithTx must return a transaction-bound copy and leave the receiver alone")
	}
	if _, err := sequence.Prefetch(s.WithTx(tx), 10); !errors.Is(err, sequence.ErrPrefetchInTx) {
		t.Fatalf("Prefetch of a tx-bound sqlstore: %v", err)
	}
	if _, err := sequence.Prefetch(s, 10); err != nil {
		t.Fatalf("Prefetch of a pool-bound sqlstore: %v", err)
	}
}

func TestDialectStatements(t *testing.T) {
	cases := []struct {
		d        sqlstore.Dialect
		incr     []string
		wantArgs []any
	}{
		{sqlstore.Postgres, []string{"INSERT INTO app.seqs AS s", "$4", "RETURNING counter"}, []any{"inv", "t1", "2026", int64(3)}},
		{sqlstore.SQLite, []string{"INSERT INTO app.seqs AS s", "VALUES (?, ?, ?, ?)", "excluded.counter", "RETURNING counter"}, []any{"inv", "t1", "2026", int64(3)}},
		{sqlstore.MySQL, []string{"INSERT INTO app.seqs (name, scope, period, counter)", "LAST_INSERT_ID(?)", "ON DUPLICATE KEY UPDATE counter = LAST_INSERT_ID(counter + ?)"}, []any{"inv", "t1", "2026", int64(3), int64(3)}},
	}
	for _, c := range cases {
		db, d := newFakeDB(t)
		s, _ := sqlstore.New(db, c.d, sqlstore.WithTables("app.seqs"))
		got, err := s.Incr(context.Background(), sequence.Key{Name: "inv", Scope: "t1"}, "2026", 3)
		if err != nil || got != 3 {
			t.Fatalf("%v: got %d, %v", c.d, got, err)
		}
		for _, want := range c.incr {
			if !strings.Contains(d.queries[0], want) {
				t.Errorf("%v statement lacks %q:\n%s", c.d, want, d.queries[0])
			}
		}
		if strings.Contains(d.queries[0], "VALUES(counter)") || strings.Contains(d.queries[0], "values(counter)") {
			t.Errorf("%v: the deprecated VALUES() function must not be used", c.d)
		}
		if fmt.Sprint(d.args[0]) != fmt.Sprint(c.wantArgs) {
			t.Errorf("%v args = %v, want %v", c.d, d.args[0], c.wantArgs)
		}
	}
}

// Error classes the way real drivers present them: the text of go-sql-driver/mysql, a Code() int
// method (modernc.org/sqlite), and message-only SQLite drivers.
type textErr string

func (e textErr) Error() string { return string(e) }

type codeErr struct {
	code int
	msg  string
}

func (e codeErr) Error() string { return e.msg }
func (e codeErr) Code() int     { return e.code }

func TestErrorClassifierPerDialect(t *testing.T) {
	cases := []struct {
		name string
		d    sqlstore.Dialect
		err  error
		want string // "lock", "exhausted" or "plain"
	}{
		{"mysql lock wait text", sqlstore.MySQL, textErr("Error 1205 (HY000): Lock wait timeout exceeded; try restarting transaction"), "lock"},
		{"mysql out of range text", sqlstore.MySQL, textErr("Error 1690 (22003): BIGINT value is out of range in '(`counter` + 5)'"), "exhausted"},
		{"mysql warn out of range", sqlstore.MySQL, textErr("Error 1264 (22003): Out of range value for column 'counter' at row 1"), "exhausted"},
		{"mysql deadlock is only wrapped", sqlstore.MySQL, textErr("Error 1213 (40001): Deadlock found when trying to get lock"), "plain"},
		{"mysql other", sqlstore.MySQL, textErr("Error 1062 (23000): Duplicate entry"), "plain"},
		{"mysql plain text", sqlstore.MySQL, errors.New("connection refused"), "plain"},
		{"sqlite busy code", sqlstore.SQLite, codeErr{5, "database is locked (5) (SQLITE_BUSY)"}, "lock"},
		{"sqlite locked code", sqlstore.SQLite, codeErr{6, "database table is locked"}, "lock"},
		{"sqlite extended busy code", sqlstore.SQLite, codeErr{261, "database is locked"}, "lock"},
		{"sqlite check code", sqlstore.SQLite, codeErr{275, "constraint failed: CHECK constraint failed: counter (275)"}, "exhausted"},
		{"sqlite unique is not exhaustion", sqlstore.SQLite, codeErr{2067, "constraint failed: UNIQUE constraint failed: sequences.name (2067)"}, "plain"},
		{"sqlite busy text only", sqlstore.SQLite, textErr("database is locked"), "lock"},
		{"sqlite check text only", sqlstore.SQLite, textErr("CHECK constraint failed: counter"), "exhausted"},
		{"postgres lock", sqlstore.Postgres, sqlStateErr{"55P03"}, "lock"},
		{"postgres overflow", sqlstore.Postgres, sqlStateErr{"22003"}, "exhausted"},
		{"postgres mysql-looking text is ignored", sqlstore.Postgres, textErr("Error 1205 (HY000): x"), "plain"},
	}
	for _, c := range cases {
		db, d := newFakeDB(t)
		d.fail = c.err
		s, _ := sqlstore.New(db, c.d)
		_, err := s.Incr(context.Background(), sequence.Key{Name: "x"}, "p", 1)
		gotLock, gotEx := errors.Is(err, sequence.ErrLockTimeout), errors.Is(err, sequence.ErrExhausted)
		switch c.want {
		case "lock":
			if !gotLock || gotEx {
				t.Errorf("%s: %v", c.name, err)
			}
		case "exhausted":
			if !gotEx || gotLock {
				t.Errorf("%s: %v", c.name, err)
			}
		default:
			if gotLock || gotEx || !errors.Is(err, c.err) {
				t.Errorf("%s: must be wrapped and unmapped, got %v", c.name, err)
			}
		}
	}
}

func TestMigrationsAndSchemaForEveryDialect(t *testing.T) {
	for _, c := range []struct {
		d     sqlstore.Dialect
		wants []string
	}{
		{sqlstore.Postgres, []string{"timestamptz", "PRIMARY KEY (name, scope, period)"}},
		{sqlstore.MySQL, []string{"utf8mb4_0900_bin", "PRIMARY KEY (name, scope, period)", "ENGINE=InnoDB"}},
		{sqlstore.SQLite, []string{"WITHOUT ROWID", "typeof(counter) = 'integer'", "PRIMARY KEY (name, scope, period)"}},
	} {
		b, err := fs.ReadFile(sqlstore.Migrations(c.d), "0001_create_sequences.sql")
		if err != nil {
			t.Fatalf("%v: %v", c.d, err)
		}
		for _, want := range c.wants {
			if !strings.Contains(string(b), want) {
				t.Errorf("%v migration lacks %q", c.d, want)
			}
		}
		custom, err := sqlstore.Schema(c.d, "app.my_seq")
		if err != nil || !strings.Contains(custom, "CREATE TABLE IF NOT EXISTS app.my_seq (") || strings.Contains(custom, "EXISTS sequences") {
			t.Errorf("%v custom schema:\n%s\n%v", c.d, custom, err)
		}
		if def, _ := sqlstore.Schema(c.d, "sequences"); def != string(b) {
			t.Errorf("%v: Schema with the default name must equal the migration", c.d)
		}
	}
}
