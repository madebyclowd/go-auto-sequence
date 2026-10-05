# Changelog

All notable changes to `go-auto-sequence` are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [1.0.0] - 2026-10-05

### Changed
- First stable release. The exported API of the root package, `memstore`, `sqlstore`, `storetest` and `seqtest` now follows semantic versioning: no breaking changes within v1. Before the release the defaults that v1 freezes were audited: the `Reserve` and `Prefetch` size limits, `errors.ErrUnsupported` for a store without `Resetter`, the `Current` clamp and `Reset` bound with `WithMax`, the rounding of the warning threshold, and the `seqtest` default clock. The release has not been run by a real application, only by the test suites on PostgreSQL, MySQL and SQLite.

## [0.1.0] - 2026-10-04

### Added
- First pre-release: formatted, ordered, unique document numbers such as `INV-2026-00042`. A `Series` issues numbers with `Next`
  and `Reserve` (a contiguous range in one round trip) and is read or repaired with `Current` and `Reset`. Per-call options
  `WithScope`, `Var`, `Vars` and `At` select a tenant, fill template values and back-date. Series options: `WithFormat`,
  `WithPeriod`, `WithStart`, `WithMax` and `RequireScope`. Format templates support `{seq:N}`, date tokens, `{var:key}` and a
  Luhn check digit (`{check:luhn}`, `ValidLuhn`). Built-in periods: `Never`, `Yearly`, `Quarterly`, `Monthly`, `Weekly` (ISO) and
  `Daily`. `WithMax` stops a series at a maximum and `WithExhaustionHandler` reports the threshold crossing exactly once.
- Stores: `sqlstore` for PostgreSQL, MySQL 8+ and SQLite 3.35+ on `database/sql` (no driver imported; works with GORM, sqlx and bun
  handles), and `memstore` for tests. Numbers are gapless when you bind a series to your own transaction
  (`store.WithTx(tx)`, optionally enforced with `RequireTx`) and gap-tolerant on a pool-bound store. `sequence.Prefetch` serves
  numbers from an in-memory block to cut round trips. Embedded migrations (`sqlstore.Migrations`, `sqlstore.Schema`) are left to
  your migration tool. Tested against real PostgreSQL, MySQL and SQLite servers in CI.
- Test support: `storetest`, a conformance suite that defines what a `Store` must do (run it against your own store), and
  `seqtest`, an in-memory fixture with a fake clock you move yourself for testing code that issues numbers.

[0.1.0]: https://github.com/madebyclowd/go-auto-sequence/releases/tag/v0.1.0

[1.0.0]: https://github.com/madebyclowd/go-auto-sequence/compare/v0.1.0...v1.0.0
