package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/sqlstore"
	"github.com/madebyclowd/go-auto-sequence/storetest"
)

const mysqlDSNEnv = "SEQ_TEST_MYSQL_DSN"

// env is one database the shared dialect suite runs against. PostgreSQL has its own, older tests
// in postgres_test.go.
type env struct {
	name    string
	dialect sqlstore.Dialect
	open    func(t *testing.T) *sql.DB // skips the test when the database is not available
	bigRow  string                     // INSERT near the int64 limit, "%s" is the table name
	workers int                        // goroutines for the concurrency test
}

func sqliteDSN(t *testing.T, immediate bool) string {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "seq.db")) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	if immediate {
		dsn += "&_txlock=immediate" // the documented recipe: take the write lock at BEGIN
	}
	return dsn
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(t, true))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openMySQL(t *testing.T, extra string) *sql.DB {
	t.Helper()
	dsn := os.Getenv(mysqlDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", mysqlDSNEnv)
	}
	if extra != "" {
		dsn += "&" + extra
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(64)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

var envs = []env{
	{name: "mysql", dialect: sqlstore.MySQL, open: func(t *testing.T) *sql.DB { return openMySQL(t, "") },
		bigRow: "INSERT INTO %s (name, counter) VALUES ('big', 9223372036854775806)", workers: 200},
	{name: "sqlite", dialect: sqlstore.SQLite, open: openSQLite,
		bigRow: "INSERT INTO %s (name, counter) VALUES ('big', 9223372036854775806)", workers: 40},
}

func tableFor(t *testing.T, db *sql.DB, d sqlstore.Dialect) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	table := "seq_it_" + hex.EncodeToString(b)
	ddl, err := sqlstore.Schema(d, table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })
	return table
}

func storeFor(t *testing.T, db *sql.DB, d sqlstore.Dialect, table string, opts ...sqlstore.Option) *sqlstore.Store {
	t.Helper()
	s, err := sqlstore.New(db, d, append([]sqlstore.Option{sqlstore.WithTables(table)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func eachEnv(t *testing.T, fn func(t *testing.T, e env, db *sql.DB, table string)) {
	t.Helper()
	for _, e := range envs {
		t.Run(e.name, func(t *testing.T) {
			db := e.open(t)
			fn(t, e, db, tableFor(t, db, e.dialect))
		})
	}
}

func TestDialectConformance(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		storetest.Run(t, func(*testing.T) sequence.Store { return storeFor(t, db, e.dialect, table) })
	})
}

func TestDialectTransactionConformance(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		pool := storeFor(t, db, e.dialect, table)
		storetest.RunTx(t, func(*testing.T) storetest.TxHarness {
			return storetest.TxHarness{
				Outside: pool,
				Begin: func(t *testing.T) (sequence.Store, func(bool) error) {
					tx, err := db.BeginTx(context.Background(), nil)
					if err != nil {
						t.Fatal(err)
					}
					return pool.WithTx(tx), func(commit bool) error {
						if commit {
							return tx.Commit()
						}
						return tx.Rollback()
					}
				},
			}
		})
	})
}

func TestDialectConcurrentUniqueAndGapless(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		seq, err := sequence.New(storeFor(t, db, e.dialect, table))
		if err != nil {
			t.Fatal(err)
		}
		s, _ := seq.Series("invoice")
		const each = 25
		var mu sync.Mutex
		var all []int64
		var wg sync.WaitGroup
		for range e.workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range each {
					n, err := s.Next(context.Background())
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					all = append(all, n.Seq)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
		if len(all) != e.workers*each {
			t.Fatalf("got %d numbers, want %d", len(all), e.workers*each)
		}
		for i, v := range all {
			if v != int64(i)+1 {
				t.Fatalf("position %d holds %d: duplicate or gap", i, v)
			}
		}
	})
}

func TestDialectRollbackGaplessVsGapTolerant(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		pool := storeFor(t, db, e.dialect, table)
		seq, _ := sequence.New(pool)
		ctx := context.Background()

		txBound, _ := seq.Series("tx-bound")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := txBound.WithStore(pool.WithTx(tx)).Next(ctx); err != nil || n.Seq != 1 {
			t.Fatalf("first = %+v, %v", n, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n, err := txBound.Next(ctx); err != nil || n.Seq != 1 {
			t.Fatalf("after rollback got %+v, %v; the number must be given back", n, err)
		}

		// A number issued on the pool store is committed on its own, so a rollback of the caller's
		// business transaction afterwards cannot give it back: a gap. (The transactions are not
		// overlapped: on SQLite an open BEGIN IMMEDIATE transaction holds the write lock.)
		poolBound, _ := seq.Series("pool-bound")
		if n, err := poolBound.Next(ctx); err != nil || n.Seq != 1 {
			t.Fatalf("first = %+v, %v", n, err)
		}
		biz, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := biz.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n, err := poolBound.Next(ctx); err != nil || n.Seq != 2 {
			t.Fatalf("got %+v, %v; a pool-bound number stays burned (a gap)", n, err)
		}
	})
}

func TestDialectRequireTx(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		s := storeFor(t, db, e.dialect, table, sqlstore.RequireTx())
		ctx := context.Background()
		k := sequence.Key{Name: "x"}
		if _, err := s.Incr(ctx, k, "", 1); !errors.Is(err, sequence.ErrNoTransaction) {
			t.Fatalf("pool: err = %v", err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		if got, err := s.WithTx(tx).Incr(ctx, k, "", 1); err != nil || got != 1 {
			t.Fatalf("tx: got %d, %v", got, err)
		}
	})
}

// Current, Reset, Reserve, WithMax with its handler, and Prefetch, on each real database.
func TestDialectFeatureTour(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		pool := storeFor(t, db, e.dialect, table)
		var fired atomic.Int64
		seq, _ := sequence.New(pool, sequence.WithExhaustionHandler(func(context.Context, sequence.Exhaustion) { fired.Add(1) }))
		ctx := context.Background()

		s, _ := seq.Series("inv", sequence.WithStart(1000), sequence.WithMax(1009, 50))
		if _, issued, err := s.Current(ctx); err != nil || issued {
			t.Fatalf("empty: issued=%v err=%v", issued, err)
		}
		ns, err := s.Reserve(ctx, 3)
		if err != nil || ns[0].Seq != 1000 || ns[2].Seq != 1002 {
			t.Fatalf("Reserve: %+v, %v", ns, err)
		}
		if last, issued, err := s.Current(ctx); err != nil || !issued || last != 1002 {
			t.Fatalf("Current = (%d, %v, %v)", last, issued, err)
		}
		for range 4 { // 1003..1006 crosses the 50% threshold (1004)
			if _, err := s.Next(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if fired.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", fired.Load())
		}
		if err := s.Reset(ctx, 1008); err != nil {
			t.Fatal(err)
		}
		if n, err := s.Next(ctx); err != nil || n.Seq != 1008 {
			t.Fatalf("after Reset(1008): %+v, %v", n, err)
		}
		if _, err := s.Reserve(ctx, 5); !errors.Is(err, sequence.ErrExhausted) {
			t.Fatalf("a reserve past max: %v", err)
		}

		// Prefetch: a pool store is fine, a real transaction-bound store is rejected.
		if _, err := sequence.Prefetch(pool, 10); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := sequence.Prefetch(pool.WithTx(tx), 10); !errors.Is(err, sequence.ErrPrefetchInTx) {
			t.Fatalf("err = %v, want ErrPrefetchInTx", err)
		}
	})
}

func TestDialectBigintOverflowIsExhausted(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		if _, err := db.Exec(strings.Replace(e.bigRow, "%s", table, 1)); err != nil {
			t.Fatal(err)
		}
		s := storeFor(t, db, e.dialect, table)
		_, err := s.Incr(context.Background(), sequence.Key{Name: "big"}, "", 5)
		var ee *sequence.ExhaustedError
		if !errors.Is(err, sequence.ErrExhausted) || !errors.As(err, &ee) {
			t.Fatalf("err = %v", err)
		}
		// The counter did not move.
		var counter int64
		if err := db.QueryRow("SELECT counter FROM " + table + " WHERE name = 'big'").Scan(&counter); err != nil || counter != math.MaxInt64-1 {
			t.Fatalf("counter = %d, %v", counter, err)
		}
	})
}

func TestDialectQuickStart(t *testing.T) {
	eachEnv(t, func(t *testing.T, e env, db *sql.DB, table string) {
		store, _ := sqlstore.New(db, e.dialect, sqlstore.WithTables(table))
		seq, _ := sequence.New(store)
		format, _ := sequence.ParseFormat("INV-{seq:5}")
		invoice, _ := seq.Series("invoice", sequence.WithFormat(format))
		n, err := invoice.Next(context.Background())
		if err != nil || n.Value != "INV-00001" {
			t.Fatalf("got %+v, %v", n, err)
		}
	})
}

// MySQL: the server gives up waiting for the row lock (innodb_lock_wait_timeout=1 on the
// connection), which is ErrLockTimeout through the driver-free text classifier.
func TestMySQLLockWaitTimeout(t *testing.T) {
	admin := openMySQL(t, "")
	table := tableFor(t, admin, sqlstore.MySQL)
	bounded := openMySQL(t, "innodb_lock_wait_timeout=1")
	ctx := context.Background()
	k := sequence.Key{Name: "hot"}

	holder, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback() //nolint:errcheck
	if _, err := storeFor(t, admin, sqlstore.MySQL, table).WithTx(holder).Incr(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}
	_, err = storeFor(t, bounded, sqlstore.MySQL, table).Incr(ctx, k, "", 1)
	if !errors.Is(err, sequence.ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
	t.Logf("driver error preserved in the chain: %v", err)
}

// SQLite: a writer that cannot get the lock within busy_timeout reports ErrLockTimeout.
func TestSQLiteBusyIsLockTimeout(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "busy.db"))
	open := func(busyMS string) *sql.DB {
		db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout("+busyMS+")&_pragma=journal_mode(WAL)&_txlock=immediate")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	holderDB, waiterDB := open("10000"), open("200")
	table := tableFor(t, holderDB, sqlstore.SQLite)
	ctx := context.Background()

	holder, err := holderDB.BeginTx(ctx, nil) // BEGIN IMMEDIATE: holds the write lock
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback() //nolint:errcheck
	if _, err := storeFor(t, holderDB, sqlstore.SQLite, table).WithTx(holder).Incr(ctx, sequence.Key{Name: "x"}, "", 1); err != nil {
		t.Fatal(err)
	}
	_, err = storeFor(t, waiterDB, sqlstore.SQLite, table).Incr(ctx, sequence.Key{Name: "x"}, "", 1)
	if !errors.Is(err, sequence.ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

// The recipe in ADR-008: with the default deferred transactions a transaction that read first and
// then writes can fail with SQLITE_BUSY when another connection committed in between; opening write
// transactions with BEGIN IMMEDIATE (DSN _txlock=immediate) avoids that. The library cannot change a
// transaction it does not own, so this is documented, and this test proves why.
func TestSQLiteDeferredTransactionFailureMode(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "deferred.db"))
	open := func(immediate bool) *sql.DB {
		dsn := "file:" + path + "?_pragma=busy_timeout(300)&_pragma=journal_mode(WAL)"
		if immediate {
			dsn += "&_txlock=immediate"
		}
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	deferred, immediate := open(false), open(true)
	table := tableFor(t, deferred, sqlstore.SQLite)
	ctx := context.Background()
	k := sequence.Key{Name: "x"}

	// tx1 (deferred) reads, so it holds a read snapshot; another connection then commits a write;
	// tx1 now tries to write on its stale snapshot.
	tx1, err := deferred.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback() //nolint:errcheck
	var n int
	if err := tx1.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err := storeFor(t, immediate, sqlstore.SQLite, table).Incr(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}
	_, err = storeFor(t, deferred, sqlstore.SQLite, table).WithTx(tx1).Incr(ctx, k, "", 1)
	if err == nil {
		t.Fatal("a deferred transaction that read first must fail to upgrade after a concurrent commit")
	}
	if !errors.Is(err, sequence.ErrLockTimeout) {
		t.Fatalf("the busy error should be classified as ErrLockTimeout, got %v", err)
	}

	// The same flow with BEGIN IMMEDIATE: the transaction takes the write lock first and simply works.
	tx2, err := immediate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback() //nolint:errcheck
	if err := tx2.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if got, err := storeFor(t, immediate, sqlstore.SQLite, table).WithTx(tx2).Incr(ctx, k, "", 1); err != nil || got != 2 {
		t.Fatalf("with BEGIN IMMEDIATE: got %d, %v", got, err)
	}
}
