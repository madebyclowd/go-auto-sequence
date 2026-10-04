---
bump: minor
type: Added
release-as: 0.1.0
---

First pre-release: formatted, ordered, unique document numbers such as `INV-2026-00042`. A `Series` issues numbers with `Next`
and `Reserve` (a contiguous range in one round trip) and is read or repaired with `Current` and `Reset`. Per-call options
`WithScope`, `Var`, `Vars` and `At` select a tenant, fill template values and back-date. Series options: `WithFormat`,
`WithPeriod`, `WithStart`, `WithMax` and `RequireScope`. Format templates support `{seq:N}`, date tokens, `{var:key}` and a
Luhn check digit (`{check:luhn}`, `ValidLuhn`). Built-in periods: `Never`, `Yearly`, `Quarterly`, `Monthly`, `Weekly` (ISO) and
`Daily`. `WithMax` stops a series at a maximum and `WithExhaustionHandler` reports the threshold crossing exactly once.
