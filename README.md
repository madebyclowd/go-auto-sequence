# go-auto-sequence

[![ci](https://img.shields.io/github/actions/workflow/status/madebyclowd/go-auto-sequence/ci.yml?branch=main&label=ci&style=flat-square)](https://github.com/madebyclowd/go-auto-sequence/actions/workflows/ci.yml)
[![security](https://img.shields.io/github/actions/workflow/status/madebyclowd/go-auto-sequence/security.yml?branch=main&label=security&style=flat-square)](https://github.com/madebyclowd/go-auto-sequence/actions/workflows/security.yml)
[![codecov](https://img.shields.io/codecov/c/github/madebyclowd/go-auto-sequence?style=flat-square)](https://codecov.io/gh/madebyclowd/go-auto-sequence)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

Formatted, ordered, unique document numbers for Go, like `INV-2026-00042` or `ORD-999`. Each new
number is larger than the last, and two callers never get the same one, even when thousands of
requests arrive in the same second.

It works with any Go project: the standard library's `database/sql`, GORM, sqlx, bun, sqlc. It does
not depend on a web framework or an ORM.

> **Status: v1, stable.** The exported API follows semantic versioning: no breaking changes within v1.
> It is tested against PostgreSQL 17, MySQL 8.4 and SQLite in CI, but it has not yet been used by a real
> application of its own, so field reports are welcome.

Install:

```bash
go get github.com/madebyclowd/go-auto-sequence@latest
```

```go
import "github.com/madebyclowd/go-auto-sequence" // package name: sequence
```

The repository is `go-auto-sequence`; the package is `sequence`, so you write `sequence.New(...)`.

## Why not just use the database `id`?

1. **It leaks data.** Customers can guess how many orders or invoices you have.
2. **It is not under your control.** It may start at 1 on your laptop and at 14,000 in production.
3. **You cannot shape it.** You cannot add a year, a branch code or your own prefix, or restart it
   every January, or give every tenant its own sequence starting at 1.

This library gives you a separate number, such as `invoice_number`, and makes sure two requests
never get the same one.

**When you do not need it:** if you only need one global counter and gaps are fine, a native database
`SEQUENCE` or `AUTO_INCREMENT` is simpler and faster. Use this library when you need formatted
numbers, per-tenant or per-period counters, or numbers without gaps (tax and audit rules).

## Quick start (PostgreSQL)

Create the table once with your migration tool (`sqlstore.Migrations(sqlstore.Postgres)` returns the
SQL as an `fs.FS`), then:

```go
db, _ := sql.Open("pgx", dsn) // any database/sql driver
store, _ := sqlstore.New(db, sqlstore.Postgres)
seq, _ := sequence.New(store)
format, _ := sequence.ParseFormat("INV-{YYYY}-{seq:5}")
invoice, _ := seq.Series("invoice", sequence.WithFormat(format), sequence.WithPeriod(sequence.Yearly))

n, err := invoice.Next(ctx) // INV-2026-00001
```

Create one `Series` per sequence and share it. The snippet above runs as a test against a real
PostgreSQL (`internal/integration`). For tests and single-process use, `memstore.New()` replaces the
database store.

### Gapless numbers (tax and audit rules)

Bind the series to **your own** transaction. The library never begins, commits or rolls back that
transaction, so a rollback gives the number back:

```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()
n, err := invoice.WithStore(store.WithTx(tx)).Next(ctx)
// ... insert the invoice row using n.Value in the same transaction ...
tx.Commit()
```

The price: the next caller for the same counter waits until you commit, so keep these transactions
short, and use a scope per tenant or branch so unrelated customers never wait on each other.

## Where teams use this

- **Invoicing:** sequential invoice numbers that meet tax or audit rules.
- **Online stores:** order numbers during a flash sale, with no duplicates.
- **SaaS billing:** each customer sees their own numbers start at `1` (`WithScope(tenantID)`).
- **Multi-branch retail:** each store gets its own daily receipt numbers.
- **Support desks:** short, permanent ticket numbers.
- **Manufacturing:** batch or serial numbers that restart each fiscal year.

## What you can do

| Need | How |
|---|---|
| A prefix, year, padding | `ParseFormat("INV-{YYYY}-{seq:5}")`, see [the guide](docs/guide.md#format-templates) |
| Restart every year, quarter, month, week or day | `WithPeriod(sequence.Yearly)` (or your own function, such as a fiscal year) |
| One sequence per tenant | `Next(ctx, sequence.WithScope(tenantID))`, and `RequireScope()` so you cannot forget it |
| A branch code or any value in the number | `{var:branch}` with `sequence.Var("branch", "JKT")` |
| Start at 1000 | `WithStart(1000)` |
| Stop at a maximum and get warned early | `WithMax(999999, 90)` and `WithExhaustionHandler` |
| A check digit | `{check:luhn}` and `sequence.ValidLuhn` |
| Many numbers at once (an import) | `Reserve(ctx, n)`, one round trip for a contiguous range |
| Back-date a number | `sequence.At(createdAt)` |
| Read or repair the counter | `Current(ctx)` and `Reset(ctx, next)` |
| Fewer database round trips, gaps allowed | `sequence.Prefetch(store, 50)` |
| Test your own code | `seqtest.New(t)`: an in-memory store and a clock you move yourself |

## Databases

| Database | Status |
|---|---|
| PostgreSQL | Supported, tested against a real server in CI |
| MySQL 8+ | Supported, tested against a real server (8.4) in CI |
| SQLite 3.35+ | Supported, tested in CI with a pure-Go driver |
| MariaDB, CockroachDB, Aurora, Cloud SQL, Vitess, PlanetScale | Best effort: wire-compatible, **not tested**; see the [guide](docs/guide.md#databases) |
| SQL Server, Oracle, DynamoDB, Mongo | Not supported; write a `Store` (about 30 lines, [recipe](docs/recipes.md#write-your-own-store)) |

The library never runs migrations. Use `sqlstore.Migrations(dialect)` or `sqlstore.Schema(dialect,
table)` with goose, golang-migrate, atlas, or copy the SQL. On MySQL use that SQL: it sets a binary
collation, so `Invoice` and `invoice` stay different counters. On SQLite open write transactions with
`BEGIN IMMEDIATE` (DSN `_txlock=immediate`).

## Recipes and guide

- [Guide](docs/guide.md): concepts, every option, errors, databases, testing.
- [Recipes](docs/recipes.md): GORM (including a `BeforeCreate` hook), sqlx, bun, pgx, importing data,
  multi-tenant numbering, events after commit, retries, writing your own store.
- [Coming from the Laravel package](docs/coming-from-laravel.md): what maps one to one and what was
  deliberately left out.

## How it is tested

Concurrency is the product, so the tests run against real databases under the race detector: 200
goroutines on PostgreSQL, 100 on MySQL and 40 on SQLite each issue numbers and the test checks they
are unique and have no gaps. A conformance suite (`storetest`) defines what a `Store` must do; you can
run it against your own. No performance numbers are published, because none have been measured.

## License

MIT. See [LICENSE](LICENSE).
