package sqlstore

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

//go:embed migrations
var migrationFiles embed.FS

// Migrations returns the numbered SQL files for d (for example 0001_create_sequences.sql) with
// the default table name "sequences". The files are plain SQL with no migration-tool
// directives, so they work with goose, golang-migrate, atlas or copy-paste. The library never
// applies them itself. It returns nil for an unknown dialect.
func Migrations(d Dialect) fs.FS {
	if !d.valid() {
		return nil
	}
	sub, err := fs.Sub(migrationFiles, "migrations/"+d.String())
	if err != nil {
		return nil
	}
	return sub
}

// Schema returns the DDL that creates the counter table named table (bare or
// schema-qualified), for use with WithTables.
func Schema(d Dialect, table string) (string, error) {
	if !d.valid() {
		return "", fmt.Errorf("%w: unknown dialect %v", sequence.ErrInvalidConfig, d)
	}
	if err := validateTable(table); err != nil {
		return "", err
	}
	b, err := fs.ReadFile(Migrations(d), "0001_create_sequences.sql")
	if err != nil {
		return "", err
	}
	return strings.Replace(string(b), "EXISTS "+defaultTable+" (", "EXISTS "+table+" (", 1), nil
}
