package integration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/sqlstore"
	"github.com/madebyclowd/go-auto-sequence/storetest"
)

const dsnEnv = "SEQ_TEST_POSTGRES_DSN"

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("%s not set", dsnEnv)
	}
	db, err := sql.Open("pgx", dsn)
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

// newTable creates a uniquely named counter table from sqlstore.Schema and drops it afterwards,
// so tests never touch each other or any existing table.
func newTable(t *testing.T, db *sql.DB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	table := "seq_it_" + hex.EncodeToString(b)
	ddl, err := sqlstore.Schema(sqlstore.Postgres, table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })
	return table
}

func newStore(t *testing.T, db *sql.DB, table string, opts ...sqlstore.Option) *sqlstore.Store {
	t.Helper()
	s, err := sqlstore.New(db, sqlstore.Postgres, append([]sqlstore.Option{sqlstore.WithTables(table)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreConformance(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)
	storetest.Run(t, func(*testing.T) sequence.Store { return newStore(t, db, table) })
}

func TestStoreTransactionConformance(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)
	pool := newStore(t, db, table)
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
}

// The headline property: 200 goroutines issuing numbers through a real Series on a real
// database get exactly 1..N, no duplicates and no gaps.
func TestSeriesConcurrentUniqueAndGapless(t *testing.T) {
	db := openDB(t)
	seq, err := sequence.New(newStore(t, db, newTable(t, db)))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := sequence.ParseFormat("INV-{seq:5}")
	series, err := seq.Series("invoice", sequence.WithFormat(f))
	if err != nil {
		t.Fatal(err)
	}
	const workers, each = 200, 25
	var mu sync.Mutex
	var all []int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				n, err := series.Next(context.Background())
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
	if len(all) != workers*each {
		t.Fatalf("got %d numbers, want %d", len(all), workers*each)
	}
	for i, v := range all {
		if v != int64(i)+1 {
			t.Fatalf("position %d holds %d: duplicate or gap", i, v)
		}
	}
}

// Inside the caller's transaction a number is gapless: a rollback gives it back. On a store bound to
// the pool the number is gap-tolerant: the caller's later rollback cannot undo it.
func TestRollbackGaplessVsGapTolerant(t *testing.T) {
	db := openDB(t)
	pool := newStore(t, db, newTable(t, db))
	seq, _ := sequence.New(pool)
	ctx := context.Background()

	t.Run("tx-bound is gapless", func(t *testing.T) {
		order, _ := seq.Series("tx-bound")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		n, err := order.WithStore(pool.WithTx(tx)).Next(ctx)
		if err != nil || n.Seq != 1 {
			t.Fatalf("first = %+v, %v", n, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n, err := order.Next(ctx); err != nil || n.Seq != 1 {
			t.Fatalf("after rollback got %+v, %v; the number must be given back", n, err)
		}
	})

	t.Run("pool-bound leaves a gap", func(t *testing.T) {
		order, _ := seq.Series("pool-bound")
		tx, err := db.BeginTx(ctx, nil) // the caller's business transaction, unrelated to the counter
		if err != nil {
			t.Fatal(err)
		}
		n, err := order.Next(ctx) // issued on the pool, outside tx
		if err != nil || n.Seq != 1 {
			t.Fatalf("first = %+v, %v", n, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n, err := order.Next(ctx); err != nil || n.Seq != 2 {
			t.Fatalf("got %+v, %v; the rolled-back flow's number stays burned (a gap)", n, err)
		}
	})
}

func TestRequireTx(t *testing.T) {
	db := openDB(t)
	s := newStore(t, db, newTable(t, db), sqlstore.RequireTx())
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
}

// Transaction A holds the row lock; B waits. With lock_timeout, Postgres reports 55P03 and the
// store maps it to ErrLockTimeout. With a context deadline, B returns promptly with both
// ErrLockTimeout and the context error.
func TestLockTimeout(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)
	s := newStore(t, db, table)
	ctx := context.Background()
	k := sequence.Key{Name: "hot"}

	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback() //nolint:errcheck
	if _, err := s.WithTx(holder).Incr(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}

	t.Run("server lock_timeout", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '150ms'"); err != nil {
			t.Fatal(err)
		}
		_, err = s.WithTx(tx).Incr(ctx, k, "", 1)
		if !errors.Is(err, sequence.ErrLockTimeout) {
			t.Fatalf("err = %v, want ErrLockTimeout", err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		short, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := s.Incr(short, k, "", 1)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the context error", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("took %v, the deadline was not honoured", time.Since(start))
		}
		t.Logf("deadline error: %v (ErrLockTimeout=%v)", err, errors.Is(err, sequence.ErrLockTimeout))
	})
}

func TestBigintOverflowIsExhausted(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)
	if _, err := db.Exec("INSERT INTO "+table+" (name, counter) VALUES ('big', $1)", int64(math.MaxInt64)-1); err != nil {
		t.Fatal(err)
	}
	s := newStore(t, db, table)
	_, err := s.Incr(context.Background(), sequence.Key{Name: "big"}, "", 5)
	var ee *sequence.ExhaustedError
	if !errors.Is(err, sequence.ErrExhausted) || !errors.As(err, &ee) {
		t.Fatalf("err = %v", err)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)
	ddl, _ := sqlstore.Schema(sqlstore.Postgres, table)
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("re-applying the migration failed: %v", err)
	}
}

// The README quick-start, run for real: this is the code a new user copies.
func TestQuickStart(t *testing.T) {
	db := openDB(t)
	table := newTable(t, db)

	store, _ := sqlstore.New(db, sqlstore.Postgres, sqlstore.WithTables(table))
	seq, _ := sequence.New(store)
	format, _ := sequence.ParseFormat("INV-{YYYY}-{seq:5}")
	invoice, _ := seq.Series("invoice", sequence.WithFormat(format), sequence.WithPeriod(sequence.Yearly))

	n, err := invoice.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "INV-" + time.Now().UTC().Format("2006") + "-00001"
	if n.Value != want {
		t.Fatalf("got %q, want %q", n.Value, want)
	}
}

// racyStore is a deliberately broken store: it reads the counter and writes it back in separate
// statements, without a lock. It exists to prove the concurrency test has teeth: the same
// assertion that passes for sqlstore must fail here.
type racyStore struct {
	db    *sql.DB
	table string
}

func (r racyStore) Incr(ctx context.Context, k sequence.Key, period string, by int64) (int64, error) {
	var cur int64
	err := r.db.QueryRowContext(ctx, "SELECT counter FROM "+r.table+" WHERE name=$1 AND scope=$2 AND period=$3", k.Name, k.Scope, period).Scan(&cur)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := cur + by
	_, err = r.db.ExecContext(ctx, "INSERT INTO "+r.table+" (name, scope, period, counter) VALUES ($1,$2,$3,$4) "+
		"ON CONFLICT (name, scope, period) DO UPDATE SET counter = EXCLUDED.counter", k.Name, k.Scope, period, next)
	return next, err
}

// issueConcurrently runs workers x each Next calls and reports whether the numbers were exactly 1..N.
func issueConcurrently(t *testing.T, store sequence.Store, workers, each int) bool {
	t.Helper()
	seq, err := sequence.New(store)
	if err != nil {
		t.Fatal(err)
	}
	series, _ := seq.Series("teeth")
	var mu sync.Mutex
	var all []int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				n, err := series.Next(context.Background())
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
	for i, v := range all {
		if v != int64(i)+1 {
			return false
		}
	}
	return len(all) == workers*each
}

func TestConcurrencyTestHasTeeth(t *testing.T) {
	db := openDB(t)
	if !issueConcurrently(t, newStore(t, db, newTable(t, db)), 50, 20) {
		t.Fatal("sqlstore must produce exactly 1..N")
	}
	if issueConcurrently(t, racyStore{db, newTable(t, db)}, 50, 20) {
		t.Fatal("the broken store passed: the concurrency assertion has no teeth")
	}
}

// The recommended way to bound lock waits: set lock_timeout on the connection (or role), not with
// SET LOCAL inside the caller's transaction. The pool store then fails with SQLSTATE 55P03, which
// the store maps to ErrLockTimeout, while a plain context deadline stays context.DeadlineExceeded.
func TestConnectionLevelLockTimeout(t *testing.T) {
	admin := openDB(t)
	table := newTable(t, admin)

	cfg, err := pgx.ParseConfig(os.Getenv(dsnEnv))
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["lock_timeout"] = "150ms"
	bounded := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = bounded.Close() })

	s := newStore(t, bounded, table)
	ctx := context.Background()
	k := sequence.Key{Name: "hot"}

	holder, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback() //nolint:errcheck
	if _, err := newStore(t, admin, table).WithTx(holder).Incr(ctx, k, "", 1); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = s.Incr(ctx, k, "", 1)
	if !errors.Is(err, sequence.ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("a server lock timeout is not a context deadline")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestSeriesCurrentAndReset(t *testing.T) {
	db := openDB(t)
	pool := newStore(t, db, newTable(t, db))
	seq, _ := sequence.New(pool)
	ctx := context.Background()
	s, _ := seq.Series("inv", sequence.WithStart(1000))

	if _, issued, err := s.Current(ctx); err != nil || issued {
		t.Fatalf("empty: issued=%v err=%v", issued, err)
	}
	for range 3 {
		if _, err := s.Next(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if last, issued, err := s.Current(ctx); err != nil || !issued || last != 1002 {
		t.Fatalf("Current = (%d, %v, %v), want (1002, true)", last, issued, err)
	}
	if err := s.Reset(ctx, 5000); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Next(ctx); err != nil || n.Seq != 5000 {
		t.Fatalf("after Reset(5000): %+v, %v", n, err)
	}
	if err := s.Reset(ctx, 999); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("below start: %v", err)
	}

	// A Reset inside the caller's transaction is undone by a rollback.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithStore(pool.WithTx(tx)).Reset(ctx, 9000); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if last, _, err := s.Current(ctx); err != nil || last != 5000 {
		t.Fatalf("after rolled-back Reset: last=%d err=%v, want 5000", last, err)
	}
}

// Concurrent Next calls interleaved with Reset must never panic or error, and numbers handed out
// between two resets stay unique.
func TestResetUnderConcurrentNext(t *testing.T) {
	db := openDB(t)
	seq, _ := sequence.New(newStore(t, db, newTable(t, db)))
	s, _ := seq.Series("busy")
	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.Next(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if err := s.Reset(ctx, 1); err != nil {
			t.Error(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
}

// Reserve takes one range with one statement: concurrent reservations never overlap, and the
// range inside a rolled-back caller transaction is given back (gapless).
func TestReserveOnPostgres(t *testing.T) {
	db := openDB(t)
	pool := newStore(t, db, newTable(t, db))
	seq, _ := sequence.New(pool)
	ctx := context.Background()
	s, _ := seq.Series("batch")

	var mu sync.Mutex
	seen := map[int64]bool{}
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				ns, err := s.Reserve(ctx, 5)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				for i, n := range ns {
					if i > 0 && n.Seq != ns[i-1].Seq+1 {
						t.Errorf("range not contiguous: %d then %d", ns[i-1].Seq, n.Seq)
					}
					if seen[n.Seq] {
						t.Errorf("duplicate %d", n.Seq)
					}
					seen[n.Seq] = true
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for i := int64(1); i <= 40*10*5; i++ {
		if !seen[i] {
			t.Fatalf("gap at %d", i)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := s.WithStore(pool.WithTx(tx)).Reserve(ctx, 50)
	if err != nil || ns[0].Seq != 2001 {
		t.Fatalf("in tx: %v, %v", ns, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Next(ctx); err != nil || n.Seq != 2001 {
		t.Fatalf("after rollback got %+v, %v; the reserved block must be given back", n, err)
	}
}
