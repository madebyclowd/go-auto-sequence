---
bump: major
type: Changed
release-as: 1.0.0
---

First stable release. The exported API of the root package, `memstore`, `sqlstore`, `storetest` and `seqtest` now follows semantic versioning: no breaking changes within v1. Before the release the defaults that v1 freezes were audited: the `Reserve` and `Prefetch` size limits, `errors.ErrUnsupported` for a store without `Resetter`, the `Current` clamp and `Reset` bound with `WithMax`, the rounding of the warning threshold, and the `seqtest` default clock. The release has not been run by a real application, only by the test suites on PostgreSQL, MySQL and SQLite.
