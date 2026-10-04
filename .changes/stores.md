---
bump: minor
type: Added
---

Stores: `sqlstore` for PostgreSQL, MySQL 8+ and SQLite 3.35+ on `database/sql` (no driver imported; works with GORM, sqlx and bun
handles), and `memstore` for tests. Numbers are gapless when you bind a series to your own transaction
(`store.WithTx(tx)`, optionally enforced with `RequireTx`) and gap-tolerant on a pool-bound store. `sequence.Prefetch` serves
numbers from an in-memory block to cut round trips. Embedded migrations (`sqlstore.Migrations`, `sqlstore.Schema`) are left to
your migration tool. Tested against real PostgreSQL, MySQL and SQLite servers in CI.
