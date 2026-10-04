package sqlstore_test

import (
	"context"
	"errors"
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
	storetest.Run(t, func(t *testing.T) sequence.Store {
		db, _ := newFakeDB(t)
		s, err := sqlstore.New(db, sqlstore.Postgres)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
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
