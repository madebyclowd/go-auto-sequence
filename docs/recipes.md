# Recipes

Short, copy-paste answers. What was actually checked:

- **Run against a real PostgreSQL:** the GORM transaction form and the GORM `BeforeCreate` hook, including that a
  rolled-back transaction gives the number back.
- **Compiled:** that `gorm.ConnPool`, `*sqlx.DB`/`*sqlx.Tx` and `bun.IDB`/`bun.Tx` satisfy the store's `DBTX`
  interface, and that pgx's `stdlib.OpenDBFromPool` yields a usable `*sql.DB`.
- **Compiled and run in a scratch program:** the custom store, the import, the events-after-commit flow and the
  retry helper (the retry helper only with a synthetic error).
- **Not run:** the sqlc, sqlx `BeginTxx` and bun `RunInTx` call shapes (standard APIs of those libraries), and anything
  on CockroachDB, MariaDB or the other best-effort databases.

All examples assume:

```go
store, _ := sqlstore.New(db, sqlstore.Postgres) // db is a *sql.DB
seq, _ := sequence.New(store)
invoice, _ := seq.Series("invoice")
```

## Plain `database/sql` and sqlc

`*sql.Tx` is accepted directly. With sqlc, pass the same transaction to `queries.WithTx(tx)` and to the store:

```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

n, err := invoice.WithStore(store.WithTx(tx)).Next(ctx)
if err != nil { return err }
q := queries.WithTx(tx)
if err := q.CreateInvoice(ctx, CreateInvoiceParams{Number: n.Value /* ... */}); err != nil { return err }
return tx.Commit()
```

## GORM

`gorm.ConnPool` has the two methods the store needs, so GORM's own transaction connection works.

Inside `db.Transaction`:

```go
err := db.Transaction(func(tx *gorm.DB) error {
    n, err := invoice.WithStore(store.WithTx(tx.Statement.ConnPool)).Next(ctx)
    if err != nil { return err }
    return tx.Create(&Invoice{Number: n.Value}).Error // a rollback gives the number back
})
```

As a `BeforeCreate` hook (the closest thing to Laravel's creating event):

```go
func (o *Order) BeforeCreate(tx *gorm.DB) error {
    if o.Number != "" { return nil }
    n, err := orderSeries.WithStore(orderStore.WithTx(tx.Statement.ConnPool)).Next(tx.Statement.Context)
    if err != nil { return err }
    o.Number = n.Value
    return nil
}
```

GORM wraps each `Create` in a transaction by default, so the hook runs inside it. Build `orderStore` with
`sqlstore.RequireTx()` to make sure it can never run outside one (it would return `ErrNoTransaction`). If you
disable GORM's default transaction (`SkipDefaultTransaction`), the hook will receive a pool and fail that check, which is
what you want for gapless numbers. When the `Create` runs inside your own `db.Transaction` that rolls back, the
number is given back too.

## sqlx

`*sqlx.DB` and `*sqlx.Tx` embed the standard types, so they work as they are:

```go
tx, _ := sqlxDB.BeginTxx(ctx, nil)
defer tx.Rollback()
n, err := invoice.WithStore(store.WithTx(tx)).Next(ctx)
```

## bun

```go
err := bunDB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
    n, err := invoice.WithStore(store.WithTx(tx)).Next(ctx)
    if err != nil { return err }
    _, err = tx.NewInsert().Model(&Invoice{Number: n.Value}).Exec(ctx)
    return err
})
```

Ent and other libraries: anything that hands you the underlying `*sql.Tx` works the same way. If it hides it, use
`database/sql` for the counter and your library for the rest, in the same transaction only if you can share the
`*sql.Tx`.

## pgx

pgx works through `database/sql`:

```go
pool, _ := pgxpool.New(ctx, dsn)
db := stdlib.OpenDBFromPool(pool) // *sql.DB on top of the pool
store, _ := sqlstore.New(db, sqlstore.Postgres)
```

pgx's own native transaction type (`pgx.Tx`) cannot be joined by this store: it is not a `database/sql` handle. Use
`db.BeginTx` for gapless numbers. A native pgx store is planned for after v1.

## Multi-tenant numbering

Each tenant gets its own numbers starting at 1. `RequireScope()` turns a forgotten scope into an error instead of a
silent shared counter:

```go
invoice, _ := seq.Series("invoice", sequence.RequireScope())
n, err := invoice.Next(ctx, sequence.WithScope(tenantID)) // tenantID is at most 128 bytes
```

Scopes also keep gapless transactions short-lived per tenant: one tenant's open transaction never blocks another's.

## Import old data

`Reserve` takes a contiguous range in one round trip and `At` puts it in the right period:

```go
ns, err := invoice.Reserve(ctx, len(rows), sequence.At(rows[0].CreatedAt)) // all share that period
for i := range rows { rows[i].Number = ns[i].Value }
```

For data spread over several periods, group the rows by period and reserve once per group. After an import that set
numbers some other way, repair the counter so the next number follows the highest one:
`invoice.Reset(ctx, highest+1, sequence.At(thatPeriod))`.

## Emit events after commit

A number is only real once the transaction commits, so publish "invoice issued" events afterwards:

```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()
n, err := invoice.WithStore(store.WithTx(tx)).Next(ctx)
// ... write the invoice using n.Value ...
if err := tx.Commit(); err != nil { return err }
publish(InvoiceIssued{Number: n.Value}) // never publish before Commit
```

The library has no issue or reset events on purpose (an event fired inside the transaction would count rolled-back
numbers). Only the threshold warning exists: `sequence.WithExhaustionHandler`.

## Retries

For a database that reports retryable serialisation failures (CockroachDB returns SQLSTATE `40001`), retry the **whole**
transaction, not just the counter call. The store does not retry for you, because it does not own your transaction:

```go
func withRetry(ctx context.Context, attempts int, fn func(context.Context) error) error {
    var err error
    for i := 0; i < attempts; i++ {
        if err = fn(ctx); err == nil { return nil }
        var st interface{ SQLState() string }
        if !errors.As(err, &st) || st.SQLState() != "40001" { return err }
    }
    return err
}
```

(`fn` should begin a fresh transaction each time.) This pattern was exercised only with a synthetic error, not against
CockroachDB.

## Tell contention from slowness

A context deadline that expires while waiting for a row lock is a `context.DeadlineExceeded`. A lock wait that the
**server** gives up on is `sequence.ErrLockTimeout`. To get the second, set the server's lock timeout on the connection
or the role (not with `SET LOCAL` in your transaction) and keep your context deadline as the outer limit:

```text
postgres: ALTER ROLE app SET lock_timeout = '2s'      (or the lock_timeout connection parameter)
mysql:    innodb_lock_wait_timeout=2                   (DSN parameter or server setting)
sqlite:   _pragma=busy_timeout(2000)                   (DSN)
```

## Write your own store

Any `Store` is one method. `Incr` adds `by` to the counter for `(key, period)` and returns the **new** value, atomically,
so the first call returns `by`. This one is in-process; a Redis `INCRBY` or a table with an atomic update would follow the
same shape:

```go
type mapStore struct {
    mu sync.Mutex
    m  map[string]int64
}

func (s *mapStore) Incr(ctx context.Context, k sequence.Key, period string, by int64) (int64, error) {
    if err := ctx.Err(); err != nil { return 0, err } // a cancelled call must not advance the counter
    if by <= 0 { return 0, fmt.Errorf("%w: increment must be > 0", sequence.ErrInvalidConfig) }
    id := k.String() + "|" + period // Key.String is injective: distinct keys never collide
    s.mu.Lock()
    defer s.mu.Unlock()
    s.m[id] += by
    return s.m[id], nil
}
```

Then prove it with the conformance suite (it checks consecutive numbers, ranges, isolation, case sensitivity, concurrency and
cancelled contexts):

```go
func TestMyStore(t *testing.T) {
    storetest.Run(t, func(t *testing.T) sequence.Store { return &mapStore{m: map[string]int64{}} })
}
```

If your store can bind to a transaction, also run `storetest.RunTx`; if it supports `Current` and `Reset`, implement the
`sequence.Resetter` interface and `Run` checks that too.

## Tests

```go
f := seqtest.New(t)
inv := f.Series("invoice", sequence.WithPeriod(sequence.Monthly))
n, _ := inv.Next(ctx)
f.Clock.Advance(32 * 24 * time.Hour) // next month
```
