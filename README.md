# go-auto-sequence

Framework-agnostic sequential document numbers for Go, like `INV-2026-00042` or
`ORD-999`. Each new number is larger than the last, and two callers never get the
same one, even under heavy concurrency.

> **Status: v0, pre-release.** Nothing is published yet and the API may change in
> minor releases until v1.0.0.

```go
import "github.com/madebyclowd/go-auto-sequence" // package name: sequence
```

The repository is `go-auto-sequence`; the package is `sequence`, so you write
`sequence.New(...)`.

## Quick start (PostgreSQL)

Create the table once with your migration tool (`sqlstore.Migrations(sqlstore.Postgres)` returns
the SQL as an `fs.FS`), then:

```go
db, _ := sql.Open("pgx", dsn) // any database/sql driver
store, _ := sqlstore.New(db, sqlstore.Postgres)
seq, _ := sequence.New(store)
format, _ := sequence.ParseFormat("INV-{YYYY}-{seq:5}")
invoice, _ := seq.Series("invoice", sequence.WithFormat(format), sequence.WithPeriod(sequence.Yearly))

n, err := invoice.Next(ctx) // INV-2026-00001
```

Create one `Series` per sequence and share it. For gapless numbers, bind the series to your own
transaction: `invoice.WithStore(store.WithTx(tx)).Next(ctx)`; a rollback gives the number back.
The snippet above runs as a test against a real PostgreSQL (`internal/integration`).

For tests and single-process use, `memstore.New()` replaces the database store.

**Databases:** PostgreSQL, MySQL 8+ and SQLite 3.35+ (`sqlstore.Postgres`, `sqlstore.MySQL`,
`sqlstore.SQLite`), tested against real servers in CI. For SQLite open write transactions with
`BEGIN IMMEDIATE` (DSN `_txlock=immediate`). Create the table with `sqlstore.Migrations` or
`sqlstore.Schema`; on MySQL use that SQL, it sets a binary collation so `Invoice` and `invoice`
stay different counters.

**Also included:** `Reserve`, `Current`/`Reset`, `WithMax` with an exhaustion handler, `Prefetch`,
and `seqtest` for your own tests. Number recycling and step increments are deliberately not part of v1.

## License

MIT. See [LICENSE](LICENSE).
