package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/sqlstore"
)

// Quick start: a Postgres-backed series in a few lines. Import any database/sql driver for
// its side effect, for example _ "github.com/jackc/pgx/v5/stdlib". Create the table once with
// sqlstore.Migrations (goose, golang-migrate, atlas) or sqlstore.Schema.
func Example() {
	db, _ := sql.Open("pgx", "postgres://user:pass@localhost/app")
	store, _ := sqlstore.New(db, sqlstore.Postgres)
	seq, _ := sequence.New(store)
	invoice, _ := seq.Series("invoice", sequence.WithPeriod(sequence.Yearly))

	n, err := invoice.Next(context.Background())
	fmt.Println(n, err)
}

// Gapless numbers: bind the series to your own transaction. A rollback gives the number back.
func ExampleStore_WithTx() {
	db, _ := sql.Open("pgx", "postgres://user:pass@localhost/app")
	store, _ := sqlstore.New(db, sqlstore.Postgres)
	seq, _ := sequence.New(store)
	invoice, _ := seq.Series("invoice")

	tx, _ := db.BeginTx(context.Background(), nil)
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	n, err := invoice.WithStore(store.WithTx(tx)).Next(context.Background())
	if err != nil {
		return
	}
	// ... insert the invoice row using n.Value in the same tx ...
	_ = tx.Commit()
	fmt.Println(n)
}
