# Coming from the Laravel package

`go-auto-sequence` is the Go counterpart of [`madebyclowd/laravel-auto-sequence`](https://github.com/madebyclowd/laravel-auto-sequence).
It is **not a port** of that package's API: Go has no model events, no config files and no service container, so several
things were redesigned and a few were left out on purpose. Numbers are not compatible with the PHP tables.

## What maps one to one

| Laravel | Go |
|---|---|
| `generate()` | `series.Next(ctx)` |
| `getCurrent()` | `series.Current(ctx)` |
| `reset($to)` | `series.Reset(ctx, next)` (the *next* number, not the last) |
| `start_value`, `max_value` | `WithStart`, `WithMax` |
| `format_template` tokens `{seq}`, date tokens, `{period}`, `{scope}` | `ParseFormat("INV-{YYYY}-{seq:5}")` |
| `pad_length` | `{seq:5}` |
| `{attribute:field}` | `{var:key}` with `sequence.Var("key", value)` (a missing value is an error, not an empty string) |
| `{checksum:mod10}` | `{check:luhn}` (must be last) and `sequence.ValidLuhn` |
| `period` daily, weekly, monthly, quarterly, yearly, never | `Daily`, `Weekly`, `Monthly`, `Quarterly`, `Yearly`, `Never` or your own function |
| `scope` | `WithScope(x)` per call, and `RequireScope()` |
| `transaction_mode` gapless / gap tolerant | a transaction-bound store (`store.WithTx(tx)`) versus a pool-bound store; `RequireTx()` enforces gapless |
| `pre_allocation` | `sequence.Prefetch(store, n)` |
| `exhaustion_threshold` and the exhausted event | `WithMax(max, pct)` and `WithExhaustionHandler` |
| `reserveRange` (planned in PHP) | `Reserve(ctx, n)` |
| `sequence:install` migrations | `sqlstore.Migrations(dialect)` and `Schema`, run with your own migration tool |

## Different on purpose

- **Explicit calls, no model trait.** There is no hook that fills a column automatically. Call `Next` in your create path
  (inside the same transaction for gapless numbers). With GORM a `BeforeCreate` hook is the closest thing, see
  [recipes](recipes.md#gorm).
- **Locking drivers are gone.** The database's atomic upsert is the lock, and your `context.Context` is the timeout.
- **The template is not stored in the database.** In PHP the last writer's template overwrote the stored one; here the
  template lives in your code.
- **Per-call errors, not exceptions:** match `errors.Is(err, sequence.ErrExhausted)` and friends.
- **Events:** only the exhaustion warning exists. Emit "issued" and audit events yourself after `Commit`.

## Left out, with alternatives

| Laravel feature | Why | What to do instead |
|---|---|---|
| `continuous` mode and `recycle()` (re-using numbers of deleted records) | The riskiest feature, the least requested, and many tax regimes forbid re-using an issued number | Keep gapless numbers and do not delete issued records; void them instead. It can be added later without breaking anything. |
| `step` | Rarely used, and it complicates `Reset` and ranges | `WithStart`, or format the number in the template |
| `{rand:N}` | Random output breaks ordered, auditable numbers and a deterministic check digit | Generate your own random part and pass it with `Var` |
| `{date:FORMAT}` | PHP date letters would mislead Go users | The fixed date tokens `{YYYY}` `{MM}` `{DD}` and so on |
| Audit columns (`created_by`, `updated_by`) | User identity is application data | Record it on your own row |
| `connection` override, `allow_manual`, `type_code`, `type_relation` | Framework or model concerns | Use a different `Store`; keep prefixes in the format |
| `sequence:list`, `sequence:reset`, `sequence:verify` CLIs | Not built yet | `Current` and `Reset` in code; an inspector and CLI are planned for after v1.0 |
| SQL Server | Not tested | Write a `Store` ([recipe](recipes.md#write-your-own-store)) |

## Moving existing counters

The Go tables are separate. To continue an existing sequence, create a series with the same format and call
`Reset(ctx, highestIssued+1)` once (with the right `WithScope` and `At`), then verify with `Current`.
