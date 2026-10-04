# Guide

How the pieces fit, and every option. For copy-paste examples see [recipes](recipes.md).

## The three things you create

```go
store, _ := sqlstore.New(db, sqlstore.Postgres)   // where counters live
seq, _   := sequence.New(store)                   // a Sequencer: store + clock + location + logger
inv, _   := seq.Series("invoice", /* options */)  // one Series per sequence, shared for the process lifetime
n, _     := inv.Next(ctx)                         // a Number
```

- **Store** holds counters. `sqlstore` (PostgreSQL, MySQL, SQLite), `memstore` (in-process, for tests), or your own.
- **Sequencer** is created once. Options: `WithClock`, `WithLocation` (default UTC), `WithLogger`
  (default: discard), `WithExhaustionHandler`.
- **Series** is an immutable handle for one named sequence. It is safe for concurrent use. Everything about
  it is validated when you create it, so a typo fails at start-up, not at 3 a.m.
- **Number** has `Value` (`"INV-2026-00042"`), `Seq` (`42`), `Period` (`"2026"`) and `Key`. It prints as its
  `Value`. It has no JSON or SQL methods on purpose: store `n.Value` (and `n.Seq` if you need it).

A counter is identified by `Key{Name, Scope}` plus the period. Names and scopes are at most 128 bytes of valid
UTF-8, a period key at most 32 bytes.

## Series options

| Option | Meaning | Default |
|---|---|---|
| `WithFormat(f)` | How the number is rendered | `{seq}` |
| `WithPeriod(p)` | When the counter restarts | `Never` |
| `WithStart(n)` | First visible value (must be `>= 0`) | `1` |
| `WithMax(max, pct)` | Cap, and a warning at `pct` percent (1 to 100) | none |
| `RequireScope()` | A call without `WithScope` returns `ErrScopeRequired` | off |

## Call options

Passed to `Next`, `Reserve`, `Current` and `Reset`. An option that does not apply to a method is ignored.

| Option | Meaning |
|---|---|
| `WithScope(s)` | Which partition, typically a tenant ID. Empty means the global counter. |
| `Var(k, v)` / `Vars(map)` | Values for `{var:k}` tokens |
| `At(t)` | Use `t` instead of the clock, for imports and back-dating |

A back-dated call increments the **old** period's counter, so numbers can be issued out of date order. That is
by design.

## Format templates

`sequence.ParseFormat` compiles a template once; a mistake is an error at start-up (`ErrInvalidFormat`, with the
byte offset). A template must contain `{seq}`, or numbers could not be unique.

| Token | Renders |
|---|---|
| `{seq}`, `{seq:N}` | The number; `N` is a minimum zero-padded width (1 to 32), never truncated |
| `{name}`, `{scope}`, `{period}` | The series name, the scope, the stored period key, verbatim |
| `{YYYY}` `{YY}` `{MM}` `{M}` `{DD}` `{D}` `{HH}` `{mm}` `{ss}` | The issue time (clock or `At`) in the sequencer's location |
| `{var:key}` | The value passed with `Var`; missing means `ErrMissingVar` (and no number is used up) |
| `{check:luhn}` | A Luhn check digit over every digit already rendered; it must be the last thing |
| `{{` and `}}` | A literal `{` and `}` |

Unknown tokens are errors, never passed through silently. Padding uses `{seq:5}`; there is no separate
`pad_length`. For anything else write a function: `sequence.FormatFunc(func(p sequence.Parts) (string, error) {...})`.

**About the check digit:** Luhn catches every single-digit typo and almost every swap of two neighbouring digits.
It cannot catch the swap of `09` and `90`. It guards against typing mistakes, not tampering.

## Periods

A `Period` is a `func(time.Time) string`. The key is stored with the counter; an empty key never restarts.

| Built-in | Key | Restarts |
|---|---|---|
| `Never` | `""` | never |
| `Yearly` | `2026` | every January 1 |
| `Quarterly` | `2026-Q4` | every quarter |
| `Monthly` | `2026-10` | every month |
| `Weekly` | `2026-W40` | every ISO 8601 week (the key uses the ISO year, so 2027-01-01 is `2026-W53`) |
| `Daily` | `2026-10-03` | every day |

Write your own for a fiscal year, as a function returning something like `"FY2026"`. The time is converted to
the sequencer's location first (`WithLocation`, default UTC), so a store in Jakarta can roll over at local midnight.
Keys should sort in time order.

## Gapless or gap-tolerant

- **Pool-bound store** (`sqlstore.New(db, ...)` with a `*sql.DB`): each number is committed on its own. Fast and
  never blocks callers on your transaction, but a rolled-back business transaction leaves a **gap**.
- **Transaction-bound store** (`store.WithTx(tx)`): the number is part of your transaction, so a rollback
  **gives it back**. No gaps. The next caller for the same counter waits until you commit.
  `sqlstore.RequireTx()` makes a pool-bound call return `ErrNoTransaction`, so a gapless series cannot silently
  become gap-tolerant.

The library never begins, commits or rolls back your transaction. The database's atomic upsert is the lock; there
are no locking drivers and no separate lock timeout: your `context.Context` is the timeout.

**Long transactions** serialise their counter. Keep them short, and use a scope (a tenant, a branch) so unrelated
customers never share a counter.

## Maximum and exhaustion warnings

`WithMax(999999, 90)` caps the series at 999,999 and warns at 90 percent. Past the maximum `Next` and `Reserve`
return an `*ExhaustedError` that matches `ErrExhausted`; nothing above the maximum is ever issued. Register
`sequence.WithExhaustionHandler(func(ctx, e sequence.Exhaustion) {...})` to be told when the threshold is crossed:

- It is called once per crossing, by the one caller that crossed it, on every store, with no extra database column.
- It is synchronous, after the number was formatted; start a goroutine for slow work. It cannot veto a number.
- It is at-least-once: a rolled-back transaction can report a crossing for a number that was never committed.
- `Reset` below the threshold lets it fire again. With no handler, the crossing is logged at Warn.

## Reserve, Current and Reset

- `Reserve(ctx, n)` returns `n` contiguous numbers with one database round trip (`n` from 1 to 1,048,576). All share
  one issue time and period. Use it for imports.
- `Current(ctx)` returns `(last, issued, err)`; `issued` is false when nothing was issued yet, so a real value of 0
  is distinguishable from "none".
- `Reset(ctx, next)` makes the next number exactly `next` (`next >= start`, and `<= max+1` with `WithMax`). To repair
  drift after an import, call `Reset(ctx, highestIssued+1)`. A reset to a lower value makes issued numbers issuable
  again, so stop writers first if that matters.
- `Current` and `Reset` need a store with the optional `Resetter` capability (`sqlstore` and `memstore` have it);
  otherwise they return an error matching `errors.ErrUnsupported`.

## Prefetch (fewer round trips, gaps allowed)

```go
fast, err := sequence.Prefetch(store, 50) // one Incr of 50, then 50 numbers served from memory
```

Gap-tolerant by design: a restart or a rollback loses the unused part of a block; numbers are unique but **not in
order across processes**. It refuses a transaction-bound store (`ErrPrefetchInTx`). `Reserve` bypasses the block.
`Current` and `Reset` are not available on a prefetched series; use `series.WithStore(store)` for those. Refills
happen in the calling goroutine under its `ctx`; there are no background goroutines and nothing to close.

## Errors

Match with `errors.Is`; the sentinels are part of the API.

| Error | When |
|---|---|
| `ErrExhausted` (and `*ExhaustedError`, via `errors.As`) | The maximum, or the int64 range, was exceeded |
| `ErrLockTimeout` | The **server** gave up waiting for a lock (Postgres `lock_timeout`, MySQL `innodb_lock_wait_timeout`, SQLite busy timeout) |
| `ErrNoTransaction` | `RequireTx` and the store is not bound to a transaction |
| `ErrScopeRequired` | `RequireScope` and the call had no scope |
| `ErrMissingVar` | A `{var:key}` had no value |
| `ErrPeriodTooLong` | A period key over 32 bytes |
| `ErrPrefetchInTx` | `Prefetch` of a transaction-bound store |
| `ErrInvalidConfig`, `ErrInvalidFormat` | A bad option, name, scope, `At` time or template |
| `errors.ErrUnsupported` | `Current` or `Reset` on a store without `Resetter` |

`context.Canceled` and `context.DeadlineExceeded` are returned as they are. A context deadline that expires while
waiting for a lock is a `DeadlineExceeded`, **not** an `ErrLockTimeout`: a deadline can also expire for other reasons.
To tell contention from slowness, set `lock_timeout` on the connection or role (never inside your transaction) and keep
the context deadline as the outer bound. Database errors are wrapped with `%w`, never swallowed.

## Databases

| | PostgreSQL | MySQL 8+ | SQLite 3.35+ |
|---|---|---|---|
| Constant | `sqlstore.Postgres` | `sqlstore.MySQL` | `sqlstore.SQLite` |
| Increment | one `INSERT ... ON CONFLICT ... RETURNING` | one `INSERT ... ON DUPLICATE KEY UPDATE` reading `LastInsertId` | one `INSERT ... ON CONFLICT ... RETURNING` |
| Notes | | binary `utf8mb4_0900_bin` key columns (the default collation would merge `Invoice` and `invoice`); create the table with `sqlstore.Migrations` / `Schema` | open write transactions with `BEGIN IMMEDIATE`: DSN `_txlock=immediate`, plus a `busy_timeout` and WAL |

The dialect is chosen by you, never sniffed from the connection. `sqlstore.WithTables("app.my_sequences")` changes the
table name (validated, bare or schema-qualified). `sqlstore.DBTX` is the two methods the store needs, so a `*sql.DB`,
`*sql.Tx`, `*sql.Conn`, a GORM `ConnPool`, a sqlx or bun handle all work.

**SQLite:** the default deferred transactions can fail with `SQLITE_BUSY` when a transaction reads, another connection
commits, and then the first tries to write. The library cannot change a transaction it does not own, so open write
transactions with `BEGIN IMMEDIATE` (`_txlock=immediate` for both `modernc.org/sqlite` and `mattn/go-sqlite3`). The tests use
the pure-Go `modernc` driver; `mattn` was not run. An open immediate transaction holds SQLite's single write lock.

**Best effort, not tested:** MariaDB (the MySQL form was not run against it), CockroachDB (serialisable isolation returns retryable
`40001`; retry the whole transaction, see [recipes](recipes.md#retries)), Aurora, Cloud SQL, Vitess and PlanetScale
(`LAST_INSERT_ID` is per connection and can misbehave behind some proxies). They may work; if one does not, the one-method
`Store` interface is the escape hatch.

## Testing your code

```go
f := seqtest.New(t)                       // in-memory store + fake clock
inv := f.Series("invoice", sequence.WithPeriod(sequence.Monthly))
f.Clock.Advance(32 * 24 * time.Hour)      // the month rolls over
```

The clock starts at a fixed instant (2026-06-17 12:00 UTC) and only moves when you move it (`At`, `Set`, `Advance`), so
tests never depend on the date they run on. `WithSequencerOptions(...)` forwards extra options. There are deliberately no
assertion helpers: compare with a plain `if`.

If you write your own `Store`, run the conformance suite: `storetest.Run(t, func(t *testing.T) sequence.Store {...})`,
and `storetest.RunTx` if it can bind to a transaction.

## Limits and honesty

- v0: the API may change in minor releases until v1.0.0.
- No throughput or latency numbers are published; none have been measured.
- Number recycling (re-using numbers of deleted records) and step increments are deliberately not included.
- Notifications are at-least-once; `Prefetch` and pool-bound numbers can have gaps; a reset can re-issue numbers.
