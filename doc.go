// Package sequence issues formatted, ordered, unique document numbers such as
// "INV-2026-00042" or "ORD-999".
//
// It is framework-agnostic: the core knows only a key, a format, and a counter,
// and never opens, commits, or rolls back a transaction. Storage lives behind a
// small Store interface (database/sql, in-memory, or your own).
//
// The repository is named go-auto-sequence, but the package identifier is
// sequence, so the import reads:
//
//	import "github.com/madebyclowd/go-auto-sequence"
//
// and is used as sequence.New(...). The import path and the package name differ,
// as with go-redis.
//
// Status: v1. The exported API is stable under semantic versioning.
package sequence
